package tasks

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

// nodeSysctlsReportKey is the fakeDokku key for the probe every node-sysctls
// planner runs. The report carries every scope, so the fixtures below include a
// profile scope the task must ignore.
const nodeSysctlsReportKey = "--quiet scheduler-k3s:node-sysctls:report --format json"

func TestSchedulerK3sNodeSysctlsSetPlansFullReplacement(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		nodeSysctlsReportKey: `{"--global":{"vm.swappiness":"60","vm.stale":"1"},"edge-workers":{"vm.swappiness":"60"}}`,
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
		nodeSysctlsReportKey: `{"--global":{"vm.swappiness":"10"}}`,
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
		nodeSysctlsReportKey: `{"--global":{"vm.swappiness":"10"},"edge-workers":{"vm.swappiness":"10"}}`,
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
	// The profile scope is populated and the global one is not. Reading the
	// profile's entry here would report drift forever, since :clear --global
	// could never empty it.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		nodeSysctlsReportKey: `{"--global":{},"edge-workers":{"vm.swappiness":"10"}}`,
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
		nodeSysctlsReportKey: `{"--global":{"vm.swappiness":"60"}}`,
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
		nodeSysctlsReportKey: "not json",
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
			name:    "global must be set",
			task:    SchedulerK3sNodeSysctlsTask{Sysctls: map[string]string{"vm.swappiness": "10"}},
			wantErr: "'global' must be set to true",
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
