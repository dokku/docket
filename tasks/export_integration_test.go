package tasks

import (
	"strings"
	"testing"
)

// TestIntegrationExportConfigRoundTrip verifies the config exporter reconstructs
// an app's config from the live server such that re-planning it reports no
// drift - the export/apply idempotency contract at the task level.
func TestIntegrationExportConfigRoundTrip(t *testing.T) {
	skipIfNoDokkuT(t)

	app := "docket-test-export-config"
	destroyApp(app)
	createApp(app)
	defer destroyApp(app)

	set := ConfigTask{App: app, Restart: false, Config: map[string]string{"EXPORT_KEY": "export_value"}, State: StatePresent}
	if r := set.Execute(); r.Error != nil {
		t.Fatalf("set config: %v", r.Error)
	}

	bodies, err := ConfigTask{}.ExportApp(app)
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 config task, got %d", len(bodies))
	}
	cfg := bodies[0].(ConfigTask)
	if cfg.Config["EXPORT_KEY"] != "export_value" {
		t.Errorf("exported config missing key: %+v", cfg.Config)
	}
	// The exporter omits state (the recipe loader defaults it to present); set
	// it here since we plan the body directly without going through the loader.
	cfg.State = StatePresent
	if plan := cfg.Plan(); !plan.InSync {
		t.Errorf("exported config should report no drift, got status %v reason %q", plan.Status, plan.Reason)
	}
}

// TestIntegrationExportReconstructsApp exercises the full ExportRecipe pipeline
// against a real server: the config value is lifted into the vars map and the
// domains land in the recipe.
func TestIntegrationExportReconstructsApp(t *testing.T) {
	skipIfNoDokkuT(t)

	app := "docket-test-export-app"
	destroyApp(app)
	createApp(app)
	defer destroyApp(app)

	if r := (ConfigTask{App: app, Config: map[string]string{"SECRET": "s3cr3t"}, State: StatePresent}).Execute(); r.Error != nil {
		t.Fatalf("set config: %v", r.Error)
	}
	if r := (DomainsTask{App: app, Domains: []string{"exp.example.com"}, State: StateSet}).Execute(); r.Error != nil {
		t.Fatalf("set domains: %v", r.Error)
	}

	res, err := ExportRecipe(ExportOptions{Apps: []string{app}})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}

	varName := sanitizeIdent(app) + "_SECRET"
	if res.Vars[varName] != "s3cr3t" {
		t.Errorf("expected vars[%s]=s3cr3t, got %q", varName, res.Vars[varName])
	}

	recipe, err := res.MarshalRecipe("yaml")
	if err != nil {
		t.Fatalf("MarshalRecipe: %v", err)
	}
	out := string(recipe)
	if strings.Contains(out, "s3cr3t") {
		t.Errorf("recipe leaked the secret value:\n%s", out)
	}
	for _, want := range []string{"exp.example.com", "{{ ." + varName + " }}", "dokku_config"} {
		if !strings.Contains(out, want) {
			t.Errorf("recipe missing %q:\n%s", want, out)
		}
	}
}

// TestIntegrationExportSchedulerK3sProfile verifies the global profile exporter
// against a real dokku with the scheduler-k3s plugin. Gated on the plugin so it
// skips where scheduler-k3s is not installed.
func TestIntegrationExportSchedulerK3sProfile(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "scheduler-k3s")

	name := "docket-test-export-profile"
	cleanup := SchedulerK3sProfileTask{Name: name, Role: "worker", State: StateAbsent}
	cleanup.Execute()
	defer cleanup.Execute()

	set := SchedulerK3sProfileTask{Name: name, Role: "worker", KubeletArgs: []string{"foo=bar"}, TaintScheduling: true, State: StatePresent}
	if r := set.Execute(); r.Error != nil {
		t.Fatalf("add profile: %v", r.Error)
	}

	bodies, err := SchedulerK3sProfileTask{}.ExportGlobal()
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	var found *SchedulerK3sProfileTask
	for i := range bodies {
		if p, ok := bodies[i].(SchedulerK3sProfileTask); ok && p.Name == name {
			found = &p
			break
		}
	}
	if found == nil {
		t.Fatalf("exported profiles do not include %q", name)
	}
	if found.Role != "worker" || found.TaintScheduling != true {
		t.Errorf("exported profile mismatch: %+v", *found)
	}
	// The exporter omits state; set it as the recipe loader would before
	// planning the body directly.
	found.State = StatePresent
	if plan := found.Plan(); !plan.InSync {
		t.Errorf("exported profile should report no drift, got status %v reason %q", plan.Status, plan.Reason)
	}
}
