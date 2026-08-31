package tasks

import (
	"sort"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

// getReportedPorts queries dokku ports:report to get the current mappings for
// an app, so assertions do not lean on the task's own probe.
func getReportedPorts(appName string) []string {
	result, err := subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "ports:report", appName, "--ports-map"},
	})
	if err != nil {
		return nil
	}

	mappings := strings.Fields(result.StdoutContents())
	sort.Strings(mappings)
	return mappings
}

func TestIntegrationPortsAddAndRemove(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-ports"

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	portMappings := []PortMapping{
		{Scheme: "http", Host: 8080, Container: 5000},
	}

	// add port
	addTask := PortsTask{App: appName, PortMappings: portMappings, State: StatePresent}
	result := addTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to add port: %v", result.Error)
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for new port")
	}

	// add same port again (idempotent)
	result = addTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent add failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for existing port")
	}

	// remove port
	removeTask := PortsTask{App: appName, PortMappings: portMappings, State: StateAbsent}
	result = removeTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to remove port: %v", result.Error)
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for port removal")
	}

	// remove again (idempotent)
	result = removeTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent remove failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for nonexistent port")
	}
}

// TestIntegrationPortsPresentRejectsSchemeHostCollision pins the half of
// dokku's reuse check that needs the probe: docket refuses the add itself
// rather than letting ports:add fail, and leaves the bound mapping alone.
func TestIntegrationPortsPresentRejectsSchemeHostCollision(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-ports-collision"

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	seedTask := PortsTask{
		App:          appName,
		PortMappings: []PortMapping{{Scheme: "http", Host: 8080, Container: 5000}},
		State:        StatePresent,
	}
	if result := seedTask.Execute(testCtx()); result.Error != nil {
		t.Fatalf("failed to seed ports: %v", result.Error)
	}

	collidingTask := PortsTask{
		App:          appName,
		PortMappings: []PortMapping{{Scheme: "http", Host: 8080, Container: 6000}},
		State:        StatePresent,
	}
	result := collidingTask.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected an error when adding a mapping on an already-bound scheme:host pair")
	}
	if !strings.Contains(result.Error.Error(), "http:8080:6000 conflicts with the existing mapping http:8080:5000") {
		t.Errorf("unexpected error: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false when the add is refused")
	}

	mappings := getReportedPorts(appName)
	if len(mappings) != 1 || mappings[0] != "http:8080:5000" {
		t.Errorf("expected the seeded mapping to survive, got %v", mappings)
	}
}

func TestIntegrationPortsSetAndClear(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-ports-set-clear"

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	// seed two mappings so set has something to replace
	seedTask := PortsTask{
		App: appName,
		PortMappings: []PortMapping{
			{Scheme: "http", Host: 8080, Container: 5000},
			{Scheme: "http", Host: 8081, Container: 5000},
		},
		State: StatePresent,
	}
	if result := seedTask.Execute(testCtx()); result.Error != nil {
		t.Fatalf("failed to seed ports: %v", result.Error)
	}

	// set replaces the whole list rather than adding to it
	setTask := PortsTask{
		App:          appName,
		PortMappings: []PortMapping{{Scheme: "http", Host: 8082, Container: 5000}},
		State:        StateSet,
	}
	result := setTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to set ports: %v", result.Error)
	}
	if result.State != StateSet {
		t.Errorf("expected state 'set', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for set ports")
	}

	mappings := getReportedPorts(appName)
	if len(mappings) != 1 || mappings[0] != "http:8082:5000" {
		t.Fatalf("expected exactly [http:8082:5000] after set, got %v", mappings)
	}

	// set again (idempotent)
	result = setTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent set failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false when the mappings already match")
	}

	// clear drops every mapping in one call
	clearTask := PortsTask{App: appName, State: StateClear}
	result = clearTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to clear ports: %v", result.Error)
	}
	if result.State != StateClear {
		t.Errorf("expected state 'clear', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for clear ports")
	}

	if mappings := getReportedPorts(appName); len(mappings) != 0 {
		t.Errorf("expected 0 mappings after clear, got %d: %v", len(mappings), mappings)
	}

	// clear again (idempotent)
	result = clearTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent clear failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false when there is nothing to clear")
	}
}
