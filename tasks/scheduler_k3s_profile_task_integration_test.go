package tasks

import (
	"testing"
)

func TestIntegrationSchedulerK3sProfileAll(t *testing.T) {
	skipUnlessSchedulerK3sT(t)

	cases := []struct {
		name string
		task SchedulerK3sProfileTask
	}{
		{
			name: "minimal-worker",
			task: SchedulerK3sProfileTask{
				Name: "docket-test-profile-minimal",
				Role: "worker",
			},
		},
		{
			name: "full-server",
			task: SchedulerK3sProfileTask{
				Name:              "docket-test-profile-full",
				Role:              "server",
				KubeletArgs:       []string{"max-pods=64", "eviction-hard=memory.available<5%"},
				TaintScheduling:   true,
				AllowUnknownHosts: true,
			},
		},
		{
			name: "multiple-kubelet-args",
			task: SchedulerK3sProfileTask{
				Name:        "docket-test-profile-args",
				Role:        "worker",
				KubeletArgs: []string{"max-pods=100", "node-labels=tier=spot", "system-reserved=cpu=200m"},
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

	profileName := "docket-test-profile-drift"

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
