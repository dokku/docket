package tasks

import (
	"github.com/dokku/docket/subprocess"
	"strings"
	"testing"
)

func dockerNetworkExists(name string) bool {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "docker",
		Args:    []string{"network", "inspect", name, "--format", "{{.Name}}"},
	})
	if err != nil {
		return false
	}
	return strings.TrimSpace(result.StdoutContents()) == name
}

func TestIntegrationNetworkCreateAndDestroy(t *testing.T) {
	skipIfNoDokkuT(t)

	networkName := "docket-test-network"

	// ensure clean state
	destroyNetwork(networkName)

	// verify network does not exist via docker cli
	if dockerNetworkExists(networkName) {
		t.Fatal("expected network to not exist before creation")
	}

	// create the network
	task := NetworkTask{Name: networkName, State: StatePresent}
	result := task.Execute()
	if result.Error != nil {
		t.Fatalf("failed to create network: %v", result.Error)
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for new network creation")
	}

	// verify network exists via docker cli
	if !dockerNetworkExists(networkName) {
		t.Fatal("expected network to exist after creation")
	}

	// verify network driver via docker cli
	inspectResult, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "docker",
		Args:    []string{"network", "inspect", networkName, "--format", "{{.Driver}}"},
	})
	if err != nil {
		t.Fatalf("failed to inspect network driver: %v", err)
	}
	driver := strings.TrimSpace(inspectResult.StdoutContents())
	if driver != "bridge" {
		t.Errorf("expected network driver 'bridge', got '%s'", driver)
	}

	// creating again should be idempotent
	result = task.Execute()
	if result.Error != nil {
		t.Fatalf("idempotent create failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for existing network")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}

	// destroy the network
	destroyTask := NetworkTask{Name: networkName, State: StateAbsent}
	result = destroyTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to destroy network: %v", result.Error)
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for network destruction")
	}

	// verify network does not exist via docker cli after destroy
	if dockerNetworkExists(networkName) {
		t.Fatal("expected network to not exist after destruction")
	}

	// destroying again should be idempotent
	result = destroyTask.Execute()
	if result.Error != nil {
		t.Fatalf("idempotent destroy failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for nonexistent network")
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
}

func TestIntegrationExportNetwork(t *testing.T) {
	skipIfNoDokkuT(t)

	// The DokkuManaged field the exporter keys on landed in dokku 0.39; a dokku
	// predating it omits the field for every network, so the exporter has
	// nothing to distinguish dokku-created networks. network:list --format json
	// serializes DokkuManaged for every network (including the built-ins that
	// always exist), so its presence in the raw output gates the test.
	probe, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "network:list", "--format", "json"},
	})
	if err != nil {
		t.Skipf("network:list --format json not available: %v", err)
	}
	if !strings.Contains(string(probe.StdoutBytes()), "DokkuManaged") {
		t.Skip("network:list --format json does not expose DokkuManaged on this dokku")
	}

	networkName := "docket-test-export-network"
	destroyNetwork(networkName)
	defer destroyNetwork(networkName)

	if r := (NetworkTask{Name: networkName, State: StatePresent}).Execute(); r.Error != nil {
		t.Fatalf("failed to create network: %v", r.Error)
	}

	bodies, err := NetworkTask{}.ExportGlobal()
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}

	var found *NetworkTask
	for i := range bodies {
		nt, ok := bodies[i].(NetworkTask)
		if !ok {
			t.Fatalf("ExportGlobal returned %T, want NetworkTask", bodies[i])
		}
		// The always-present bridge built-in is not dokku-created and must be
		// filtered out.
		if nt.Name == "bridge" {
			t.Errorf("ExportGlobal emitted Docker built-in %q", nt.Name)
		}
		if nt.Name == networkName {
			nt := nt
			found = &nt
		}
	}
	if found == nil {
		t.Fatalf("ExportGlobal did not include %q; got %+v", networkName, bodies)
	}

	// The exported task round-trips: re-planning it against the same server is a
	// no-op because the network already exists. The exporter omits state, which
	// the recipe loader defaults to present; set it here since we plan the body
	// directly without going through the loader.
	found.State = StatePresent
	if plan := found.Plan(); plan.Error != nil {
		t.Fatalf("re-plan of exported task failed: %v", plan.Error)
	} else if !plan.InSync {
		t.Errorf("exported task should be in sync after export, got %+v", plan)
	}
}
