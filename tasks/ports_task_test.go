package tasks

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
	_ "github.com/gliderlabs/sigil/builtin"
)

func TestPortsTaskInvalidState(t *testing.T) {
	task := PortsTask{
		App:          "test-app",
		PortMappings: []PortMapping{{Scheme: "http", Host: 80, Container: 5000}},
		State:        "invalid",
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestPortsTaskEmptyPortMappings(t *testing.T) {
	task := PortsTask{App: "test-app", PortMappings: []PortMapping{}, State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with empty port mappings should return an error")
	}
}

func TestPortsTaskValidatePerItem(t *testing.T) {
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
const portsReportKey = "--quiet ports:report web --ports-map"

func TestPortsSetPlansFullReplacement(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		portsReportKey: "http:80:5000 https:443:5000",
	}))()

	plan := PortsTask{
		App:          "web",
		PortMappings: []PortMapping{{Scheme: "http", Host: 8080, Container: 5000}},
		State:        StateSet,
	}.Plan()
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
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		portsReportKey: "",
	}))()

	plan := PortsTask{
		App:          "web",
		PortMappings: []PortMapping{{Scheme: "http", Host: 80, Container: 5000}},
		State:        StateSet,
	}.Plan()
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.Status != PlanStatusCreate {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusCreate)
	}
}

func TestPortsSetConvergesWhenReportMatches(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		portsReportKey: "https:443:5000 http:80:5000",
	}))()

	plan := PortsTask{
		App: "web",
		PortMappings: []PortMapping{
			{Scheme: "http", Host: 80, Container: 5000},
			{Scheme: "https", Host: 443, Container: 5000},
		},
		State: StateSet,
	}.Plan()
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
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		portsReportKey: "http:80:5000 https:443:5000",
	}))()

	plan := PortsTask{App: "web", State: StateClear}.Plan()
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
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		portsReportKey: "",
	}))()

	plan := PortsTask{App: "web", State: StateClear}.Plan()
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
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		portsReportKey: "http:80:5000",
	}))()

	plan := PortsTask{
		App: "web",
		PortMappings: []PortMapping{
			{Scheme: "http", Host: 80, Container: 5000},
			{Scheme: "https", Host: 443, Container: 5000},
		},
		State: StatePresent,
	}.Plan()
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

func TestPortsAbsentPlansOnlyThePresentMappings(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		portsReportKey: "http:80:5000",
	}))()

	plan := PortsTask{
		App: "web",
		PortMappings: []PortMapping{
			{Scheme: "http", Host: 80, Container: 5000},
			{Scheme: "https", Host: 443, Container: 5000},
		},
		State: StateAbsent,
	}.Plan()
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
	pm := PortMapping{Scheme: "http", Host: 80, Container: 5000}
	expected := "http:80:5000"
	if pm.String() != expected {
		t.Errorf("PortMapping.String() = %q, want %q", pm.String(), expected)
	}
}

func TestPortMappingStringVariousValues(t *testing.T) {
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
