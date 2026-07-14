package tasks

import (
	"strings"
	"testing"

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
