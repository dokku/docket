package tasks

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

// The fakeDokku keys for the probes the node-sysctls planner and exporter run.
// Plan narrows the stored report to the task's own scope; export reads every
// scope at once.
const (
	nodeSysctlsGlobalReportKey  = "--quiet scheduler-k3s:node-sysctls:report --stored --global --format json"
	nodeSysctlsProfileReportKey = "--quiet scheduler-k3s:node-sysctls:report --stored --profile edge-workers --format json"
	nodeSysctlsStoredReportKey  = "--quiet scheduler-k3s:node-sysctls:report --stored --format json"
)

func TestSchedulerK3sNodeSysctlsSetPlansFullReplacement(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		nodeSysctlsGlobalReportKey: `{"--global":{"vm.swappiness":"60","vm.stale":"1"}}`,
	}))

	plan := SchedulerK3sNodeSysctlsTask{
		Global:  true,
		Sysctls: map[string]string{"vm.swappiness": "10", "vm.max_map_count": "262144"},
		State:   StateSet,
	}.Plan(ctx)

	if plan.Error != nil {
		t.Fatalf("Plan() error: %v", plan.Error)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one command, got %v", plan.Commands)
	}
	if !strings.HasSuffix(plan.Commands[0], "scheduler-k3s:node-sysctls:set --replace --global vm.max_map_count=262144 vm.swappiness=10") {
		t.Errorf("unexpected command: %q", plan.Commands[0])
	}
	want := []string{`set vm.max_map_count=262144 (new)`, `set vm.swappiness=10 (was "60")`, `unset vm.stale (was "1")`}
	if !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
}

func TestSchedulerK3sNodeSysctlsSetConvergesWhenReportMatches(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		nodeSysctlsGlobalReportKey: `{"--global":{"vm.swappiness":"10"}}`,
	}))

	plan := SchedulerK3sNodeSysctlsTask{
		Global:  true,
		Sysctls: map[string]string{"vm.swappiness": "10"},
		State:   StateSet,
	}.Plan(ctx)

	if !plan.InSync {
		t.Errorf("expected in sync, got %v", plan.Mutations)
	}
}

func TestSchedulerK3sNodeSysctlsClearEmptiesTheScope(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		nodeSysctlsGlobalReportKey: `{"--global":{"vm.swappiness":"10"}}`,
	}))

	plan := SchedulerK3sNodeSysctlsTask{Global: true, State: StateClear}.Plan(ctx)

	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one command, got %v", plan.Commands)
	}
	if !strings.HasSuffix(plan.Commands[0], "scheduler-k3s:node-sysctls:clear --global") {
		t.Errorf("unexpected command: %q", plan.Commands[0])
	}
	if plan.Status != PlanStatusDestroy {
		t.Errorf("Status = %v, want %v", plan.Status, PlanStatusDestroy)
	}
	if want := []string{`unset vm.swappiness (was "10")`}; !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
}

func TestSchedulerK3sNodeSysctlsClearIsInSyncWhenScopeIsEmpty(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		nodeSysctlsGlobalReportKey: `{"--global":{}}`,
	}))

	plan := SchedulerK3sNodeSysctlsTask{Global: true, State: StateClear}.Plan(ctx)

	if !plan.InSync {
		t.Errorf("expected in sync, got %v", plan.Mutations)
	}
	if len(plan.Commands) != 0 {
		t.Errorf("expected no commands, got %v", plan.Commands)
	}
}

func TestSchedulerK3sNodeSysctlsPresentAndAbsentUseThePerKeyForm(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		nodeSysctlsGlobalReportKey: `{"--global":{"vm.swappiness":"60"}}`,
	}))

	present := SchedulerK3sNodeSysctlsTask{
		Global:  true,
		Sysctls: map[string]string{"vm.swappiness": "10"},
		State:   StatePresent,
	}.Plan(ctx)
	if len(present.Commands) != 1 {
		t.Fatalf("expected exactly one command, got %v", present.Commands)
	}
	if !strings.HasSuffix(present.Commands[0], "scheduler-k3s:node-sysctls:set --global vm.swappiness 10") {
		t.Errorf("unexpected present command: %q", present.Commands[0])
	}

	absent := SchedulerK3sNodeSysctlsTask{
		Global:  true,
		Sysctls: map[string]string{"vm.swappiness": ""},
		State:   StateAbsent,
	}.Plan(ctx)
	if len(absent.Commands) != 1 {
		t.Fatalf("expected exactly one command, got %v", absent.Commands)
	}
	// An empty value is how the per-key form deletes; it is a real empty
	// argument, which the rendered command shows as a trailing space.
	if !strings.HasSuffix(absent.Commands[0], "scheduler-k3s:node-sysctls:set --global vm.swappiness ") {
		t.Errorf("unexpected absent command: %q", absent.Commands[0])
	}
}

func TestSchedulerK3sNodeSysctlsProbeSurfacesSSHError(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, _ subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		return subprocess.ExecCommandResponse{ExitCode: 255}, &subprocess.SSHError{
			Host:   "dokku@unreachable",
			Stderr: "ssh: connect to host unreachable port 22: Connection refused",
		}
	})

	plan := SchedulerK3sNodeSysctlsTask{Global: true, State: StateClear}.Plan(ctx)
	if plan.Error == nil {
		t.Fatal("expected a plan error when the transport fails")
	}
	var sshErr *subprocess.SSHError
	if !errors.As(plan.Error, &sshErr) {
		t.Errorf("expected the SSHError to survive, got %v", plan.Error)
	}
}

// TestSchedulerK3sNodeSysctlsProbeTreatsDokkuFailureAsNoSysctls pins the other
// half of that split: a dokku-level non-zero exit means scheduler-k3s has
// nothing stored yet, so plan reports the declared map as a create.
func TestSchedulerK3sNodeSysctlsProbeTreatsDokkuFailureAsNoSysctls(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, _ subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		return subprocess.ExecCommandResponse{ExitCode: 1}, &subprocess.ExecError{
			Response: subprocess.ExecCommandResponse{ExitCode: 1, Stderr: "scheduler-k3s is not initialized"},
			Err:      errors.New("exit status 1"),
			Ran:      true,
		}
	})

	plan := SchedulerK3sNodeSysctlsTask{
		Global:  true,
		Sysctls: map[string]string{"vm.swappiness": "10"},
		State:   StateSet,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.Status != PlanStatusCreate {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusCreate)
	}
}

func TestSchedulerK3sNodeSysctlsProbeSurfacesMalformedReport(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		nodeSysctlsGlobalReportKey: "not json",
	}))

	plan := SchedulerK3sNodeSysctlsTask{Global: true, State: StateClear}.Plan(ctx)
	if plan.Error == nil {
		t.Fatal("expected a plan error when the report does not decode")
	}
	if !strings.Contains(plan.Error.Error(), "parse scheduler-k3s:node-sysctls:report json") {
		t.Errorf("unexpected error: %v", plan.Error)
	}
}

func TestSchedulerK3sNodeSysctlsTaskInvalidState(t *testing.T) {
	t.Parallel()
	result := SchedulerK3sNodeSysctlsTask{
		Global:  true,
		Sysctls: map[string]string{"vm.swappiness": "10"},
		State:   "invalid",
	}.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestSchedulerK3sNodeSysctlsValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		task    SchedulerK3sNodeSysctlsTask
		wantErr string
	}{
		{
			name:    "a scope is required",
			task:    SchedulerK3sNodeSysctlsTask{Sysctls: map[string]string{"vm.swappiness": "10"}},
			wantErr: "'profile' is required when 'global' is not set to true",
		},
		{
			name:    "global and profile are mutually exclusive",
			task:    SchedulerK3sNodeSysctlsTask{Global: true, Profile: "edge-workers", Sysctls: map[string]string{"vm.swappiness": "10"}},
			wantErr: "'profile' must not be set when 'global' is set to true",
		},
		{
			name: "profile accepts a valid name",
			task: SchedulerK3sNodeSysctlsTask{Profile: "edge-workers", Sysctls: map[string]string{"vm.swappiness": "10"}},
		},
		{
			name: "profile accepts a valid name for state clear",
			task: SchedulerK3sNodeSysctlsTask{Profile: "edge-workers", State: StateClear},
		},
		{
			name:    "profile rejects a name dokku refuses",
			task:    SchedulerK3sNodeSysctlsTask{Profile: "-edge", State: StateClear},
			wantErr: "'profile' must contain only alphanumeric characters and dashes",
		},
		{
			// dokku's node-sysctls writes refuse a profile helm cannot name a
			// release after, :clear included, so no state gets a pass.
			name:    "profile rejects an uppercase name for state clear",
			task:    SchedulerK3sNodeSysctlsTask{Profile: "EdgeWorkers", State: StateClear},
			wantErr: "'profile' must be lowercase, got \"EdgeWorkers\"",
		},
		{
			name:    "profile rejects a name too long for its helm release",
			task:    SchedulerK3sNodeSysctlsTask{Profile: strings.Repeat("a", 27), Sysctls: map[string]string{"vm.swappiness": ""}, State: StateAbsent},
			wantErr: "'profile' must be at most 26 characters, got 27",
		},
		{
			name:    "set rejects an empty map",
			task:    SchedulerK3sNodeSysctlsTask{Global: true, State: StateSet},
			wantErr: "'sysctls' must not be empty for state 'set'",
		},
		{
			name:    "clear rejects a map",
			task:    SchedulerK3sNodeSysctlsTask{Global: true, Sysctls: map[string]string{"vm.swappiness": "10"}, State: StateClear},
			wantErr: "'sysctls' must not be set for state 'clear'",
		},
		{
			name: "clear accepts no map",
			task: SchedulerK3sNodeSysctlsTask{Global: true, State: StateClear},
		},
		{
			name:    "set rejects a name carrying an equals sign",
			task:    SchedulerK3sNodeSysctlsTask{Global: true, Sysctls: map[string]string{"vm.bad=name": "1"}, State: StateSet},
			wantErr: "sysctl names must not contain '=' for state 'set'",
		},
		{
			name: "set accepts an empty value",
			task: SchedulerK3sNodeSysctlsTask{Global: true, Sysctls: map[string]string{"vm.swappiness": ""}, State: StateSet},
		},
		{
			name:    "present rejects an empty value",
			task:    SchedulerK3sNodeSysctlsTask{Global: true, Sysctls: map[string]string{"vm.swappiness": ""}},
			wantErr: "sysctl values must not be empty for state 'present'",
		},
		{
			// The per-key form passes the value as its own argument, where a
			// leading dash reads as the start of a flag.
			name:    "present rejects a negative value",
			task:    SchedulerK3sNodeSysctlsTask{Global: true, Sysctls: map[string]string{"kernel.perf_event_paranoid": "-1"}},
			wantErr: "sysctl values must not begin with '-' for state 'present'",
		},
		{
			name: "set accepts a negative value",
			task: SchedulerK3sNodeSysctlsTask{Global: true, Sysctls: map[string]string{"kernel.perf_event_paranoid": "-1"}, State: StateSet},
		},
		{
			// absent never sends a value, so there is nothing for the flag
			// parser to misread.
			name: "absent accepts a negative value",
			task: SchedulerK3sNodeSysctlsTask{Global: true, Sysctls: map[string]string{"kernel.perf_event_paranoid": "-1"}, State: StateAbsent},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.task.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}

func TestSchedulerK3sNodeSysctlsProfileCommandsAddressTheProfile(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		nodeSysctlsProfileReportKey: `{"edge-workers":{"vm.swappiness":"60"}}`,
	}))

	cases := []struct {
		name   string
		task   SchedulerK3sNodeSysctlsTask
		suffix string
	}{
		{
			name:   "set",
			task:   SchedulerK3sNodeSysctlsTask{Profile: "edge-workers", Sysctls: map[string]string{"vm.swappiness": "10", "vm.max_map_count": "262144"}, State: StateSet},
			suffix: "scheduler-k3s:node-sysctls:set --replace --profile edge-workers vm.max_map_count=262144 vm.swappiness=10",
		},
		{
			name:   "present",
			task:   SchedulerK3sNodeSysctlsTask{Profile: "edge-workers", Sysctls: map[string]string{"vm.swappiness": "10"}, State: StatePresent},
			suffix: "scheduler-k3s:node-sysctls:set --profile edge-workers vm.swappiness 10",
		},
		{
			name:   "absent",
			task:   SchedulerK3sNodeSysctlsTask{Profile: "edge-workers", Sysctls: map[string]string{"vm.swappiness": ""}, State: StateAbsent},
			suffix: "scheduler-k3s:node-sysctls:set --profile edge-workers vm.swappiness ",
		},
		{
			name:   "clear",
			task:   SchedulerK3sNodeSysctlsTask{Profile: "edge-workers", State: StateClear},
			suffix: "scheduler-k3s:node-sysctls:clear --profile edge-workers",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan := tc.task.Plan(ctx)
			if plan.Error != nil {
				t.Fatalf("Plan() error: %v", plan.Error)
			}
			if len(plan.Commands) != 1 {
				t.Fatalf("expected exactly one command, got %v", plan.Commands)
			}
			if !strings.HasSuffix(plan.Commands[0], tc.suffix) {
				t.Errorf("command = %q, want suffix %q", plan.Commands[0], tc.suffix)
			}
		})
	}
}

// TestSchedulerK3sNodeSysctlsProfileClearIsInSyncWhenStoredMapIsEmpty pins
// #555: a profile's resolved set carries every global sysctl, so a probe that
// read it would see drift after every :clear --profile. The stored report
// returns only the profile's own map, which :clear does empty.
func TestSchedulerK3sNodeSysctlsProfileClearIsInSyncWhenStoredMapIsEmpty(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		nodeSysctlsProfileReportKey: `{"edge-workers":{}}`,
		// The unfiltered resolved report, which the probe must not read.
		"--quiet scheduler-k3s:node-sysctls:report --format json": `{"--global":{"vm.swappiness":"10"},"edge-workers":{"vm.swappiness":"10"}}`,
	}))

	plan := SchedulerK3sNodeSysctlsTask{Profile: "edge-workers", State: StateClear}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("Plan() error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Errorf("expected in sync, got %v", plan.Mutations)
	}
}

func TestSchedulerK3sNodeSysctlsProfileSetDiffsAgainstTheStoredMap(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		nodeSysctlsProfileReportKey: `{"edge-workers":{"vm.swappiness":"60"}}`,
	}))

	plan := SchedulerK3sNodeSysctlsTask{
		Profile: "edge-workers",
		Sysctls: map[string]string{"vm.swappiness": "60"},
		State:   StateSet,
	}.Plan(ctx)
	if !plan.InSync {
		t.Errorf("expected in sync, got %v", plan.Mutations)
	}
}

// TestSchedulerK3sNodeSysctlsProbeRejectsDokkuWithoutStored checks a dokku
// older than 0.38.30 is reported rather than read as "no sysctls", which would
// let state 'clear' report in sync on a scope that still stores sysctls.
func TestSchedulerK3sNodeSysctlsProbeRejectsDokkuWithoutStored(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, _ subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		response := subprocess.ExecCommandResponse{ExitCode: 2, Stderr: "flag provided but not defined: -stored\nUsage of scheduler-k3s:node-sysctls:report:\n"}
		return response, &subprocess.ExecError{Response: response, Err: errors.New("exit status 2"), Ran: true}
	})

	plan := SchedulerK3sNodeSysctlsTask{Global: true, State: StateClear}.Plan(ctx)
	if plan.Error == nil {
		t.Fatal("expected a plan error when dokku has no --stored flag")
	}
	if !strings.Contains(plan.Error.Error(), "requires dokku 0.38.30") {
		t.Errorf("unexpected error: %v", plan.Error)
	}
}
