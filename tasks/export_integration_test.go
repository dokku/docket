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

// TestIntegrationExportCerts verifies the app certificate exporter: certs:show
// streams the cert/key back, so the exported task round-trips with no drift.
// certs is a core plugin, so only dokku itself is required.
func TestIntegrationExportCerts(t *testing.T) {
	skipIfNoDokkuT(t)

	app := "docket-test-export-certs"
	destroyApp(app)
	createApp(app)
	defer destroyApp(app)

	certPath, keyPath := generateSelfSignedCert(t, app+".example.com")
	if r := (CertsTask{App: app, Cert: certPath, Key: keyPath, State: StatePresent}).Execute(); r.Error != nil {
		t.Fatalf("add cert: %v", r.Error)
	}

	bodies, err := CertsTask{}.ExportApp(app)
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 certs task, got %d", len(bodies))
	}
	c := bodies[0].(CertsTask)
	if !strings.Contains(c.CertContent, "BEGIN CERTIFICATE") {
		t.Errorf("exported cert_content missing PEM: %q", c.CertContent)
	}
	if !strings.Contains(c.KeyContent, "BEGIN") {
		t.Errorf("exported key_content missing PEM: %q", c.KeyContent)
	}
	c.State = StatePresent
	if plan := c.Plan(); !plan.InSync {
		t.Errorf("exported certs should report no drift, got status %v reason %q", plan.Status, plan.Reason)
	}
}

// TestIntegrationExportSchedulerK3sProfile verifies the global profile exporter
// against a real dokku. scheduler-k3s is a core plugin and profiles are stored
// on disk (no cluster or deploy needed), so only dokku itself is required.
func TestIntegrationExportSchedulerK3sProfile(t *testing.T) {
	skipIfNoDokkuT(t)

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

// TestIntegrationExportSchedulerK3sChart verifies the global chart-values
// exporter. scheduler-k3s is a core plugin and chart values are stored on disk
// (no cluster or deploy needed), so only dokku itself is required.
func TestIntegrationExportSchedulerK3sChart(t *testing.T) {
	skipIfNoDokkuT(t)

	chart := "ingress-nginx"
	cleanup := SchedulerK3sChartTask{Chart: chart, Values: map[string]any{"docket-test-key": ""}, State: StateAbsent}
	cleanup.Execute()
	defer cleanup.Execute()

	set := SchedulerK3sChartTask{Chart: chart, Values: map[string]any{"docket-test-key": "docket-test-value"}, State: StatePresent}
	if r := set.Execute(); r.Error != nil {
		t.Fatalf("set chart value: %v", r.Error)
	}

	bodies, err := SchedulerK3sChartTask{}.ExportGlobal()
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	var found *SchedulerK3sChartTask
	for i := range bodies {
		if c, ok := bodies[i].(SchedulerK3sChartTask); ok && c.Chart == chart {
			found = &c
			break
		}
	}
	if found == nil {
		t.Fatalf("exported charts do not include %q", chart)
	}
	if found.Values["docket-test-key"] != "docket-test-value" {
		t.Errorf("exported chart value mismatch: %+v", found.Values)
	}
	found.State = StatePresent
	if plan := found.Plan(); !plan.InSync {
		t.Errorf("exported chart should report no drift, got status %v reason %q", plan.Status, plan.Reason)
	}
}

// TestIntegrationExportLetsencrypt verifies the letsencrypt exporter reads the
// active state. Gated on the letsencrypt plugin (not a dokku core plugin).
// Enabling letsencrypt triggers a real ACME issuance, so this only asserts the
// inactive path: a fresh app is not active, so no task is emitted.
func TestIntegrationExportLetsencrypt(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "letsencrypt")

	app := "docket-test-export-letsencrypt"
	destroyApp(app)
	createApp(app)
	defer destroyApp(app)

	bodies, err := LetsencryptTask{}.ExportApp(app)
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	if len(bodies) != 0 {
		t.Errorf("expected no letsencrypt task for an inactive app, got %d", len(bodies))
	}
}
