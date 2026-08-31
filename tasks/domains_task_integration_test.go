package tasks

import (
	"github.com/dokku/docket/subprocess"
	"strings"
	"testing"
)

// getReportedDomains queries dokku domains:report to get the current domain list for an app
func getReportedDomains(appName string) []string {
	result, err := subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"domains:report", appName, "--domains-app-vhosts"},
	})
	if err != nil {
		return nil
	}

	return strings.Fields(result.StdoutContents())
}

func TestIntegrationDomainsAddAndRemove(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-domains-task"

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	// add domains
	addTask := DomainsTask{
		App:     appName,
		Domains: []string{"example.com", "www.example.com"},
		State:   StatePresent,
	}
	result := addTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to add domains: %v", result.Error)
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for new domains")
	}

	// verify domains via domains:report
	domains := getReportedDomains(appName)
	domainSet := map[string]bool{}
	for _, d := range domains {
		domainSet[d] = true
	}
	if !domainSet["example.com"] {
		t.Error("expected 'example.com' in domains:report output after add")
	}
	if !domainSet["www.example.com"] {
		t.Error("expected 'www.example.com' in domains:report output after add")
	}

	// add same domains again (idempotent)
	result = addTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent add failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for existing domains")
	}

	// remove one domain
	removeTask := DomainsTask{
		App:     appName,
		Domains: []string{"www.example.com"},
		State:   StateAbsent,
	}
	result = removeTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to remove domain: %v", result.Error)
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for domain removal")
	}

	// verify domains via domains:report after removal
	domains = getReportedDomains(appName)
	domainSet = map[string]bool{}
	for _, d := range domains {
		domainSet[d] = true
	}
	if !domainSet["example.com"] {
		t.Error("expected 'example.com' to still be present after removing www.example.com")
	}
	if domainSet["www.example.com"] {
		t.Error("expected 'www.example.com' to be absent after removal")
	}

	// remove same domain again (idempotent)
	result = removeTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent remove failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for already-removed domain")
	}

	// set domains (replaces all)
	setTask := DomainsTask{
		App:     appName,
		Domains: []string{"new.example.com"},
		State:   StateSet,
	}
	result = setTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to set domains: %v", result.Error)
	}
	if result.State != StateSet {
		t.Errorf("expected state 'set', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for set domains")
	}

	// verify domains via domains:report after set
	domains = getReportedDomains(appName)
	if len(domains) != 1 {
		t.Fatalf("expected exactly 1 domain after set, got %d: %v", len(domains), domains)
	}
	if domains[0] != "new.example.com" {
		t.Errorf("expected domain 'new.example.com' after set, got '%s'", domains[0])
	}

	// clear all domains
	clearTask := DomainsTask{
		App:   appName,
		State: StateClear,
	}
	result = clearTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to clear domains: %v", result.Error)
	}
	if result.State != StateClear {
		t.Errorf("expected state 'clear', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for clear domains")
	}

	// verify no domains via domains:report after clear
	domains = getReportedDomains(appName)
	if len(domains) != 0 {
		t.Errorf("expected 0 domains after clear, got %d: %v", len(domains), domains)
	}
}
