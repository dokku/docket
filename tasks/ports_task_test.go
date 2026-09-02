package tasks

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestPortsTaskInvalidState(t *testing.T) {
	t.Parallel()
	task := PortsTask{
		App:          "test-app",
		PortMappings: []PortMapping{{Scheme: "http", Host: 80, Container: 5000}},
		State:        "invalid",
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestPortsTaskEmptyPortMappings(t *testing.T) {
	t.Parallel()
	task := PortsTask{App: "test-app", PortMappings: []PortMapping{}, State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with empty port mappings should return an error")
	}
}

func TestPortsTaskValidatePerItem(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		task    PortsTask
		wantErr string
	}{
		{
			name: "valid mapping",
			task: PortsTask{App: "web", PortMappings: []PortMapping{{Scheme: "http", Host: 80, Container: 5000}}},
		},
		{
			name:    "missing scheme is rejected",
			task:    PortsTask{App: "web", PortMappings: []PortMapping{{Host: 80, Container: 5000}}},
			wantErr: "'scheme' is required for port_mappings[0]",
		},
		{
			name:    "missing host is rejected",
			task:    PortsTask{App: "web", PortMappings: []PortMapping{{Scheme: "http", Container: 5000}}},
			wantErr: "'host' must be a port",
		},
		{
			name:    "missing container is rejected",
			task:    PortsTask{App: "web", PortMappings: []PortMapping{{Scheme: "http", Host: 80}}},
			wantErr: "'container' must be a port",
		},
		{
			name:    "missing app is rejected",
			task:    PortsTask{PortMappings: []PortMapping{{Scheme: "http", Host: 80, Container: 5000}}},
			wantErr: "'app' is required",
		},
		{
			name:    "empty list is rejected for present",
			task:    PortsTask{App: "web", PortMappings: []PortMapping{}, State: StatePresent},
			wantErr: "'port_mappings' must not be empty for state 'present'",
		},
		{
			name:    "empty list is rejected for absent",
			task:    PortsTask{App: "web", PortMappings: []PortMapping{}, State: StateAbsent},
			wantErr: "'port_mappings' must not be empty for state 'absent'",
		},
		{
			name:    "empty list is rejected for set",
			task:    PortsTask{App: "web", PortMappings: []PortMapping{}, State: StateSet},
			wantErr: "'port_mappings' must not be empty for state 'set'",
		},
		{
			name: "clear does not require a list",
			task: PortsTask{App: "web", State: StateClear},
		},
		{
			name: "clear accepts an explicitly empty list",
			task: PortsTask{App: "web", PortMappings: []PortMapping{}, State: StateClear},
		},
		{
			name:    "clear rejects a list",
			task:    PortsTask{App: "web", PortMappings: []PortMapping{{Scheme: "http", Host: 80, Container: 5000}}, State: StateClear},
			wantErr: "'port_mappings' must not be set for state 'clear'",
		},
		{
			name: "reused scheme and host port is rejected",
			task: PortsTask{App: "web", PortMappings: []PortMapping{
				{Scheme: "http", Host: 80, Container: 5000},
				{Scheme: "http", Host: 80, Container: 6000},
			}},
			wantErr: "port_mappings[1] reuses the scheme and host port of port_mappings[0] (http:80)",
		},
		{
			name: "reused scheme and host port is rejected for set",
			task: PortsTask{App: "web", PortMappings: []PortMapping{
				{Scheme: "https", Host: 443, Container: 5000},
				{Scheme: "http", Host: 80, Container: 5000},
				{Scheme: "https", Host: 443, Container: 6000},
			}, State: StateSet},
			wantErr: "port_mappings[2] reuses the scheme and host port of port_mappings[0] (https:443)",
		},
		{
			name: "an exact duplicate mapping is rejected",
			task: PortsTask{App: "web", PortMappings: []PortMapping{
				{Scheme: "http", Host: 80, Container: 5000},
				{Scheme: "http", Host: 80, Container: 5000},
			}},
			wantErr: "port_mappings[1] reuses the scheme and host port of port_mappings[0] (http:80)",
		},
		{
			// ports:remove runs no reuse check: at most one of the two can be
			// bound, so the other is a no-op the plan already drops.
			name: "absent accepts a reused scheme and host port",
			task: PortsTask{App: "web", PortMappings: []PortMapping{
				{Scheme: "http", Host: 80, Container: 5000},
				{Scheme: "http", Host: 80, Container: 6000},
			}, State: StateAbsent},
		},
		{
			name: "the same host port under different schemes is accepted",
			task: PortsTask{App: "web", PortMappings: []PortMapping{
				{Scheme: "http", Host: 80, Container: 5000},
				{Scheme: "https", Host: 80, Container: 5000},
			}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.task.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

// portsReportKey is the fakeDokku key for the probe getPorts runs.
const portsReportKey = "--quiet ports:report web --ports-map-json"

func TestPortsSetPlansFullReplacement(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		portsReportKey: `[{"container_port":5000,"host_port":80,"scheme":"http"},{"container_port":5000,"host_port":443,"scheme":"https"}]`,
	}))

	plan := PortsTask{
		App:          "web",
		PortMappings: []PortMapping{{Scheme: "http", Host: 8080, Container: 5000}},
		State:        StateSet,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift when the desired mappings differ from the report")
	}
	if plan.Status != PlanStatusModify {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusModify)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one planned command, got %v", plan.Commands)
	}
	// The command carries the complete desired list, not the per-mapping delta.
	if !strings.HasSuffix(plan.Commands[0], "ports:set web http:8080:5000") {
		t.Errorf("expected ports:set with the full desired list, got %q", plan.Commands[0])
	}
	want := []string{"add http:8080:5000", "remove http:80:5000", "remove https:443:5000"}
	if !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
}

func TestPortsSetOnAppWithNoMappingsIsACreate(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		portsReportKey: "null",
	}))

	plan := PortsTask{
		App:          "web",
		PortMappings: []PortMapping{{Scheme: "http", Host: 80, Container: 5000}},
		State:        StateSet,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.Status != PlanStatusCreate {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusCreate)
	}
}

func TestPortsSetConvergesWhenReportMatches(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		portsReportKey: `[{"container_port":5000,"host_port":443,"scheme":"https"},{"container_port":5000,"host_port":80,"scheme":"http"}]`,
	}))

	plan := PortsTask{
		App: "web",
		PortMappings: []PortMapping{
			{Scheme: "http", Host: 80, Container: 5000},
			{Scheme: "https", Host: 443, Container: 5000},
		},
		State: StateSet,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Fatalf("expected in-sync once the report matches the desired set, got %#v", plan)
	}
	if len(plan.Commands) != 0 {
		t.Errorf("expected no planned commands when in sync, got %v", plan.Commands)
	}
}

func TestPortsClearPlansEveryMapping(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		portsReportKey: `[{"container_port":5000,"host_port":80,"scheme":"http"},{"container_port":5000,"host_port":443,"scheme":"https"}]`,
	}))

	plan := PortsTask{App: "web", State: StateClear}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift when the app has mappings to clear")
	}
	if plan.Status != PlanStatusDestroy {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusDestroy)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one planned command, got %v", plan.Commands)
	}
	// ports:clear takes the app and nothing else.
	if !strings.HasSuffix(plan.Commands[0], "ports:clear web") {
		t.Errorf("expected a bare ports:clear command, got %q", plan.Commands[0])
	}
	want := []string{"remove http:80:5000", "remove https:443:5000"}
	if !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
}

func TestPortsClearIsInSyncWithoutMappings(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		portsReportKey: "null",
	}))

	plan := PortsTask{App: "web", State: StateClear}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Fatalf("expected in-sync when the app has no mappings, got %#v", plan)
	}
	if len(plan.Commands) != 0 {
		t.Errorf("expected no planned commands when in sync, got %v", plan.Commands)
	}
}

func TestPortsPresentPlansOnlyTheMissingMappings(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		portsReportKey: `[{"container_port":5000,"host_port":80,"scheme":"http"}]`,
	}))

	plan := PortsTask{
		App: "web",
		PortMappings: []PortMapping{
			{Scheme: "http", Host: 80, Container: 5000},
			{Scheme: "https", Host: 443, Container: 5000},
		},
		State: StatePresent,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.Status != PlanStatusModify {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusModify)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one planned command, got %v", plan.Commands)
	}
	if !strings.HasSuffix(plan.Commands[0], "ports:add web https:443:5000") {
		t.Errorf("expected ports:add with only the missing mapping, got %q", plan.Commands[0])
	}
	want := []string{"add https:443:5000"}
	if !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
}

func TestPortsPresentRejectsCollisionWithAnExistingMapping(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		portsReportKey: `[{"container_port":5000,"host_port":80,"scheme":"http"}]`,
	}))

	plan := PortsTask{
		App:          "web",
		PortMappings: []PortMapping{{Scheme: "http", Host: 80, Container: 6000}},
		State:        StatePresent,
	}.Plan(ctx)
	if plan.Error == nil {
		t.Fatal("expected an error when the scheme:host pair is already bound to another container port")
	}
	if !strings.Contains(plan.Error.Error(), "http:80:6000 conflicts with the existing mapping http:80:5000") {
		t.Errorf("unexpected error: %v", plan.Error)
	}
	if len(plan.Commands) != 0 {
		t.Errorf("expected no planned commands, got %v", plan.Commands)
	}
}

func TestPortsPresentAllowsTheSameHostPortUnderAnotherScheme(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		portsReportKey: `[{"container_port":5000,"host_port":80,"scheme":"http"}]`,
	}))

	plan := PortsTask{
		App:          "web",
		PortMappings: []PortMapping{{Scheme: "https", Host: 80, Container: 5000}},
		State:        StatePresent,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one planned command, got %v", plan.Commands)
	}
	if !strings.HasSuffix(plan.Commands[0], "ports:add web https:80:5000") {
		t.Errorf("expected ports:add for the differing scheme, got %q", plan.Commands[0])
	}
}

func TestPortsAbsentPlansOnlyThePresentMappings(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		portsReportKey: `[{"container_port":5000,"host_port":80,"scheme":"http"}]`,
	}))

	plan := PortsTask{
		App: "web",
		PortMappings: []PortMapping{
			{Scheme: "http", Host: 80, Container: 5000},
			{Scheme: "https", Host: 443, Container: 5000},
		},
		State: StateAbsent,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.Status != PlanStatusDestroy {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusDestroy)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one planned command, got %v", plan.Commands)
	}
	if !strings.HasSuffix(plan.Commands[0], "ports:remove web http:80:5000") {
		t.Errorf("expected ports:remove with only the configured mapping, got %q", plan.Commands[0])
	}
}

func TestPortMappingString(t *testing.T) {
	t.Parallel()
	pm := PortMapping{Scheme: "http", Host: 80, Container: 5000}
	expected := "http:80:5000"
	if pm.String() != expected {
		t.Errorf("PortMapping.String() = %q, want %q", pm.String(), expected)
	}
}

func TestPortMappingStringVariousValues(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		pm   PortMapping
		want string
	}{
		{"http standard", PortMapping{Scheme: "http", Host: 80, Container: 5000}, "http:80:5000"},
		{"https", PortMapping{Scheme: "https", Host: 443, Container: 5000}, "https:443:5000"},
		{"high ports", PortMapping{Scheme: "http", Host: 8080, Container: 80}, "http:8080:80"},
		{"zero ports", PortMapping{Scheme: "http", Host: 0, Container: 0}, "http:0:0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.pm.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetTasksPortsTaskWithMappings(t *testing.T) {
	t.Parallel()
	data := []byte(`---
- tasks:
    - name: set ports
      dokku_ports:
        app: test-app
        port_mappings:
          - scheme: http
            host: 80
            container: 5000
          - scheme: https
            host: 443
            container: 5000
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("set ports")
	if task == nil {
		t.Fatal("task 'set ports' not found")
	}

	portsTask, ok := task.(*PortsTask)
	if !ok {
		pt, ok2 := task.(PortsTask)
		if !ok2 {
			t.Fatalf("task is not a PortsTask (type is %T)", task)
		}
		portsTask = &pt
	}

	if len(portsTask.PortMappings) != 2 {
		t.Fatalf("expected 2 port mappings, got %d", len(portsTask.PortMappings))
	}

	if portsTask.PortMappings[0].String() != "http:80:5000" {
		t.Errorf("mapping[0] = %q, want %q", portsTask.PortMappings[0].String(), "http:80:5000")
	}
	if portsTask.PortMappings[1].String() != "https:443:5000" {
		t.Errorf("mapping[1] = %q, want %q", portsTask.PortMappings[1].String(), "https:443:5000")
	}
}

func TestGetTasksPortsTaskClearWithoutMappings(t *testing.T) {
	t.Parallel()
	data := []byte(`---
- tasks:
    - name: clear ports
      dokku_ports:
        app: test-app
        state: clear
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("clear ports")
	if task == nil {
		t.Fatal("task 'clear ports' not found")
	}

	portsTask, ok := task.(*PortsTask)
	if !ok {
		pt, ok2 := task.(PortsTask)
		if !ok2 {
			t.Fatalf("task is not a PortsTask (type is %T)", task)
		}
		portsTask = &pt
	}

	if portsTask.State != StateClear {
		t.Errorf("expected state 'clear', got %q", portsTask.State)
	}
	if len(portsTask.PortMappings) != 0 {
		t.Errorf("expected no port mappings, got %d", len(portsTask.PortMappings))
	}
	if err := portsTask.Validate(); err != nil {
		t.Errorf("clear without port_mappings should validate, got: %v", err)
	}
}

// TestPortsReportArgs pins the probe's argv. fakeDokku keys on the joined
// args, so a flag change that slipped through here would turn every ports
// fixture lookup into a miss and leave the "no mappings" tests passing for the
// wrong reason.
func TestPortsReportArgs(t *testing.T) {
	t.Parallel()
	want := []string{"--quiet", "ports:report", "web", "--ports-map-json"}
	if got := portsReportArgs("web"); !reflect.DeepEqual(got, want) {
		t.Errorf("portsReportArgs() = %v, want %v", got, want)
	}
}

func TestParsePortsMapReport(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		raw     string
		want    map[string]PortMapping
		wantErr string
	}{
		{
			name: "single mapping",
			raw:  `[{"container_port":5000,"host_port":80,"scheme":"http"}]`,
			want: map[string]PortMapping{
				"http:80:5000": {Scheme: "http", Host: 80, Container: 5000},
			},
		},
		{
			name: "several mappings",
			raw:  `[{"container_port":5000,"host_port":80,"scheme":"http"},{"container_port":5000,"host_port":443,"scheme":"https"}]`,
			want: map[string]PortMapping{
				"http:80:5000":   {Scheme: "http", Host: 80, Container: 5000},
				"https:443:5000": {Scheme: "https", Host: 443, Container: 5000},
			},
		},
		{
			// dokku marshals a nil slice for an app with no mappings.
			name: "null is no mappings",
			raw:  "null",
			want: map[string]PortMapping{},
		},
		{
			// dokku reports [] when it could not read the map property.
			name: "empty array is no mappings",
			raw:  "[]",
			want: map[string]PortMapping{},
		},
		{
			name: "empty payload is no mappings",
			raw:  "",
			want: map[string]PortMapping{},
		},
		{
			// dokku records a bare host port under the __internal__ scheme with
			// no container port. It round-trips unchanged, as it did under the
			// scheme:host:container text form this replaced.
			name: "internal scheme round-trips",
			raw:  `[{"container_port":0,"host_port":5000,"scheme":"__internal__"}]`,
			want: map[string]PortMapping{
				"__internal__:5000:0": {Scheme: "__internal__", Host: 5000, Container: 0},
			},
		},
		{
			name: "duplicate entries collapse on the mapping key",
			raw:  `[{"container_port":5000,"host_port":80,"scheme":"http"},{"container_port":5000,"host_port":80,"scheme":"http"}]`,
			want: map[string]PortMapping{
				"http:80:5000": {Scheme: "http", Host: 80, Container: 5000},
			},
		},
		{
			// The text parse dropped every token it could not read; the decode
			// says so instead.
			name:    "malformed payload errors",
			raw:     "not json",
			wantErr: "parse ports:report --ports-map-json",
		},
		{
			// A report polluted by a banner is rejected whole rather than
			// having the banner silently skipped and the mappings kept.
			name:    "banner-prefixed payload errors",
			raw:     "Deprecated: use something else\n[{\"container_port\":5000,\"host_port\":80,\"scheme\":\"http\"}]",
			wantErr: "parse ports:report --ports-map-json",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePortsMapReport([]byte(tt.raw))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error %q, got: %v", tt.wantErr, err)
				}
				if got != nil {
					t.Errorf("expected no mappings alongside the error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parsePortsMapReport() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestPortsProbeSurfacesSSHError pins that an unreachable host is a plan error
// rather than an app that happens to have no mappings, which would have
// state 'clear' and 'absent' report in sync against a server never contacted.
func TestPortsProbeSurfacesSSHError(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, _ subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		return subprocess.ExecCommandResponse{ExitCode: 255}, &subprocess.SSHError{
			Host:   "dokku@unreachable",
			Stderr: "ssh: connect to host unreachable port 22: Connection refused",
		}
	})

	plan := PortsTask{App: "web", State: StateClear}.Plan(ctx)
	if plan.Error == nil {
		t.Fatal("expected a plan error when the transport fails")
	}
	var sshErr *subprocess.SSHError
	if !errors.As(plan.Error, &sshErr) {
		t.Errorf("expected the SSHError to survive, got %v", plan.Error)
	}
	if plan.InSync {
		t.Error("expected drift rather than in-sync when the probe could not run")
	}
}

// TestPortsProbeTreatsDokkuFailureAsNoMappings pins the deliberate other half
// of the split: a dokku-level non-zero exit means the app does not exist yet,
// so plan reports the full list as an add rather than failing outright.
func TestPortsProbeTreatsDokkuFailureAsNoMappings(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, _ subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		return subprocess.ExecCommandResponse{ExitCode: 1}, &subprocess.ExecError{
			Response: subprocess.ExecCommandResponse{ExitCode: 1, Stderr: "App web does not exist"},
			Err:      errors.New("exit status 1"),
			Ran:      true,
		}
	})

	plan := PortsTask{
		App:          "web",
		PortMappings: []PortMapping{{Scheme: "http", Host: 80, Container: 5000}},
		State:        StatePresent,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.Status != PlanStatusCreate {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusCreate)
	}
	want := []string{"add http:80:5000"}
	if !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
}

// TestPortsProbeSurfacesMalformedReport is the behaviour the JSON report buys:
// a report dokku could not have produced is an error, where the text parse
// dropped the unreadable mappings and reported the rest as the whole truth.
func TestPortsProbeSurfacesMalformedReport(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		portsReportKey: "not json",
	}))

	plan := PortsTask{App: "web", State: StateClear}.Plan(ctx)
	if plan.Error == nil {
		t.Fatal("expected a plan error when the report does not decode")
	}
	if !strings.Contains(plan.Error.Error(), "parse ports:report --ports-map-json") {
		t.Errorf("unexpected error: %v", plan.Error)
	}
	if plan.InSync {
		t.Error("expected drift rather than in-sync when the report could not be read")
	}
}
