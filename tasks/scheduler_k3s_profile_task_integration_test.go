package tasks

import (
	"strings"
	"testing"
)

// schedulerK3sProfileBoundaryName is a profile name of exactly
// schedulerK3sProfileNameHelmMaxLength characters, built from the constant so
// the fixture tracks it rather than needing a hand recount. Its derived
// node-sysctls release name fills helm's 53-character ceiling exactly.
var schedulerK3sProfileBoundaryName = "docket-test-" +
	strings.Repeat("x", schedulerK3sProfileNameHelmMaxLength-len("docket-test-"))

// Profile names here are kept short on purpose. dokku gives each profile a
// node-sysctls helm release named `dokku-node-sysctls-profile-<name>` and helm
// caps a release name at 53 characters, so the task refuses a name longer than
// 26 for state 'present' (dokku/docket#482). dokku's own `profiles:add` does
// not - it accepts up to 32 - so the ceiling is only felt on the unset, which
// resolves the release name through helm.
func TestIntegrationSchedulerK3sProfileAll(t *testing.T) {
	skipUnlessSchedulerK3sT(t)

	cases := []struct {
		name string
		task SchedulerK3sProfileTask
	}{
		{
			name: "minimal-worker",
			task: SchedulerK3sProfileTask{
				Name: "docket-test-minimal",
				Role: "worker",
			},
		},
		{
			name: "full-server",
			task: SchedulerK3sProfileTask{
				Name:              "docket-test-full",
				Role:              "server",
				KubeletArgs:       []string{"max-pods=64", "eviction-hard=memory.available<5%"},
				TaintScheduling:   true,
				AllowUnknownHosts: true,
			},
		},
		{
			name: "multiple-kubelet-args",
			task: SchedulerK3sProfileTask{
				Name:        "docket-test-args",
				Role:        "worker",
				KubeletArgs: []string{"max-pods=100", "node-labels=tier=spot", "system-reserved=cpu=200m"},
			},
		},
		{
			// The only check of the derived cap against live helm rather
			// than against docket's own arithmetic. This CI job runs
			// `scheduler-k3s:initialize`, so isKubernetesAvailable() is
			// true and the unset really does resolve
			// dokku-node-sysctls-profile-<name>. If the cap were a
			// character too generous, this unset would fail with
			// "release name is invalid" instead of cleaning up.
			name: "name-at-the-helm-cap",
			task: SchedulerK3sProfileTask{
				Name: schedulerK3sProfileBoundaryName,
				Role: "worker",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setTask := tc.task
			setTask.State = StatePresent
			unsetTask := tc.task
			unsetTask.State = StateAbsent
			defer unsetTask.Execute()
			runPropertyIdempotencyTest(t, propertyIdempotencyCase{
				label:     "scheduler-k3s profile " + tc.name,
				setTask:   setTask,
				unsetTask: unsetTask,
			})
		})
	}
}

// TestIntegrationSchedulerK3sProfileFieldDrift locks in the per-field
// idempotency story given dokku's full-replace semantics: changing one field
// while re-declaring the others must converge in one apply and report
// Changed=false on the immediate re-apply.
func TestIntegrationSchedulerK3sProfileFieldDrift(t *testing.T) {
	skipUnlessSchedulerK3sT(t)

	profileName := "docket-test-drift"

	seed := SchedulerK3sProfileTask{
		Name:              profileName,
		Role:              "worker",
		KubeletArgs:       []string{"max-pods=64", "eviction-hard=memory.available<5%"},
		TaintScheduling:   true,
		AllowUnknownHosts: true,
		State:             StatePresent,
	}
	defer SchedulerK3sProfileTask{Name: profileName, Role: "worker", State: StateAbsent}.Execute()

	if result := seed.Execute(); result.Error != nil {
		t.Fatalf("seed failed: %v", result.Error)
	}

	steps := []struct {
		name   string
		mutate func(SchedulerK3sProfileTask) SchedulerK3sProfileTask
		check  func(t *testing.T, entry schedulerK3sProfileEntry)
	}{
		{
			name: "flip role",
			mutate: func(s SchedulerK3sProfileTask) SchedulerK3sProfileTask {
				s.Role = "server"
				return s
			},
			check: func(t *testing.T, e schedulerK3sProfileEntry) {
				if e.Role != "server" {
					t.Errorf("role expected server, got %q", e.Role)
				}
				if len(e.KubeletArgs) != 2 {
					t.Errorf("kubelet_args expected to survive role flip, got %v", e.KubeletArgs)
				}
				if !e.TaintScheduling || !e.AllowUnknownHosts {
					t.Errorf("bool flags expected to survive role flip, got taint=%v allow=%v",
						e.TaintScheduling, e.AllowUnknownHosts)
				}
			},
		},
		{
			name: "drop one kubelet arg",
			mutate: func(s SchedulerK3sProfileTask) SchedulerK3sProfileTask {
				s.KubeletArgs = []string{"max-pods=64"}
				return s
			},
			check: func(t *testing.T, e schedulerK3sProfileEntry) {
				if len(e.KubeletArgs) != 1 || e.KubeletArgs[0] != "max-pods=64" {
					t.Errorf("expected kubelet_args=[max-pods=64], got %v", e.KubeletArgs)
				}
			},
		},
		{
			name: "flip taint_scheduling off",
			mutate: func(s SchedulerK3sProfileTask) SchedulerK3sProfileTask {
				s.TaintScheduling = false
				return s
			},
			check: func(t *testing.T, e schedulerK3sProfileEntry) {
				if e.TaintScheduling {
					t.Errorf("expected taint_scheduling=false, got true")
				}
				if !e.AllowUnknownHosts {
					t.Errorf("expected allow_unknown_hosts to survive, got false")
				}
			},
		},
	}

	current := seed
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			next := step.mutate(current)
			result := next.Execute()
			if result.Error != nil {
				t.Fatalf("apply failed: %v", result.Error)
			}
			if !result.Changed {
				t.Errorf("expected Changed=true on drift apply, got false")
			}

			reapply := next.Execute()
			if reapply.Error != nil {
				t.Fatalf("re-apply failed: %v", reapply.Error)
			}
			if reapply.Changed {
				t.Errorf("expected Changed=false on immediate re-apply, got true")
			}

			entry, found, err := getSchedulerK3sProfile(profileName)
			if err != nil {
				t.Fatalf("probe failed: %v", err)
			}
			if !found {
				t.Fatal("profile missing after apply")
			}
			step.check(t, entry)

			current = next
		})
	}
}
