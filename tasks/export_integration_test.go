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

	set := ConfigTask{App: app, Restart: boolPtr(false), Config: map[string]string{"EXPORT_KEY": "export_value"}, State: StatePresent}
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

// TestIntegrationExportGlobalCerts verifies the global certificate exporter:
// global-cert:show streams the cert/key back (dokku-global-cert 0.4.x+), so the
// exported task round-trips with no drift. Requires the dokku-global-cert plugin.
func TestIntegrationExportGlobalCerts(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "global-cert")

	certPath, keyPath := generateSelfSignedCert(t, "global-export.example.com")

	// best-effort cleanup before and after
	cleanup := func() {
		(CertsTask{Global: true, State: StateAbsent}).Execute()
	}
	cleanup()
	defer cleanup()

	if r := (CertsTask{Global: true, Cert: certPath, Key: keyPath, State: StatePresent}).Execute(); r.Error != nil {
		t.Fatalf("add global cert: %v", r.Error)
	}

	bodies, err := CertsTask{}.ExportGlobal()
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 certs task, got %d", len(bodies))
	}
	c := bodies[0].(CertsTask)
	if !c.Global {
		t.Errorf("exported global cert task should set global: true")
	}
	if c.App != "" {
		t.Errorf("exported global cert task should not set app, got %q", c.App)
	}
	if !strings.Contains(c.CertContent, "BEGIN CERTIFICATE") {
		t.Errorf("exported cert_content missing PEM: %q", c.CertContent)
	}
	if !strings.Contains(c.KeyContent, "BEGIN") {
		t.Errorf("exported key_content missing PEM: %q", c.KeyContent)
	}
	c.State = StatePresent
	if plan := c.Plan(); !plan.InSync {
		t.Errorf("exported global certs should report no drift, got status %v reason %q", plan.Status, plan.Reason)
	}
}

// TestIntegrationExportGitPropertyGlobal verifies the global property exporter
// (#327) against a real dokku: a globally-set property is read back through
// git:report --global and reconstructed as a global: true task that round-trips
// with no drift. archive-max-files is a global-only key, so it exercises the
// scope the per-app exporter cannot reach. git is a core plugin, so only dokku
// itself is required.
func TestIntegrationExportGitPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	cleanup := GitPropertyTask{Global: true, Property: "archive-max-files", State: StateAbsent}
	cleanup.Execute()
	defer cleanup.Execute()

	set := GitPropertyTask{Global: true, Property: "archive-max-files", Value: "4242", State: StatePresent}
	if r := set.Execute(); r.Error != nil {
		t.Fatalf("set global git property: %v", r.Error)
	}

	bodies, err := GitPropertyTask{}.ExportGlobal()
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	var found *GitPropertyTask
	for i := range bodies {
		if p, ok := bodies[i].(GitPropertyTask); ok && p.Property == "archive-max-files" {
			found = &p
			break
		}
	}
	if found == nil {
		t.Fatalf("exported global git properties do not include archive-max-files: %+v", bodies)
	}
	if !found.Global {
		t.Errorf("exported property should set global: true, got %+v", *found)
	}
	if found.App != "" {
		t.Errorf("exported global property should not set app, got %q", found.App)
	}
	if found.Value != "4242" {
		t.Errorf("exported value = %q, want 4242", found.Value)
	}

	// The exporter omits state; set it as the recipe loader would before planning.
	found.State = StatePresent
	if plan := found.Plan(); !plan.InSync {
		t.Errorf("exported global property should report no drift, got status %v reason %q", plan.Status, plan.Reason)
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

// TestIntegrationSchedulerK3sAnnotationsExport verifies the annotations
// exporter reconstructs an app's annotations such that re-planning the exported
// body reports no drift. Named with the TestIntegrationSchedulerK3s prefix so
// the scheduler-k3s-test CI job's -run filter picks it up, and gated like the
// other scheduler-k3s task tests.
func TestIntegrationSchedulerK3sAnnotationsExport(t *testing.T) {
	skipUnlessSchedulerK3sT(t)

	app := "docket-test-export-k3s-annotations"
	destroyApp(app)
	createApp(app)
	defer destroyApp(app)

	cleanup := SchedulerK3sAnnotationsTask{
		App:          app,
		ResourceType: "deployment",
		Annotations:  map[string]string{"prometheus.io/scrape": "", "prometheus.io/port": ""},
		State:        StateAbsent,
	}
	defer cleanup.Execute()

	set := SchedulerK3sAnnotationsTask{
		App:          app,
		ResourceType: "deployment",
		Annotations:  map[string]string{"prometheus.io/scrape": "true", "prometheus.io/port": "9090"},
		State:        StatePresent,
	}
	if r := set.Execute(); r.Error != nil {
		t.Fatalf("set annotations: %v", r.Error)
	}

	bodies, err := SchedulerK3sAnnotationsTask{}.ExportApp(app)
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	var found *SchedulerK3sAnnotationsTask
	for i := range bodies {
		if a, ok := bodies[i].(SchedulerK3sAnnotationsTask); ok && a.ProcessType == "" && a.ResourceType == "deployment" {
			found = &a
			break
		}
	}
	if found == nil {
		t.Fatalf("exported annotations do not include the deployment scope: %+v", bodies)
	}
	if found.Annotations["prometheus.io/scrape"] != "true" || found.Annotations["prometheus.io/port"] != "9090" {
		t.Errorf("exported annotations mismatch: %+v", found.Annotations)
	}
	found.State = StatePresent
	if plan := found.Plan(); !plan.InSync {
		t.Errorf("exported annotations should report no drift, got status %v reason %q", plan.Status, plan.Reason)
	}
}

// TestIntegrationSchedulerK3sLabelsExport verifies the labels exporter
// reconstructs an app's labels such that re-planning the exported body reports
// no drift. Named with the TestIntegrationSchedulerK3s prefix so the
// scheduler-k3s-test CI job's -run filter picks it up, and gated like the other
// scheduler-k3s task tests.
func TestIntegrationSchedulerK3sLabelsExport(t *testing.T) {
	skipUnlessSchedulerK3sT(t)

	app := "docket-test-export-k3s-labels"
	destroyApp(app)
	createApp(app)
	defer destroyApp(app)

	cleanup := SchedulerK3sLabelsTask{
		App:          app,
		ProcessType:  "web",
		ResourceType: "deployment",
		Labels:       map[string]string{"tier": "", "app.kubernetes.io/component": ""},
		State:        StateAbsent,
	}
	defer cleanup.Execute()

	set := SchedulerK3sLabelsTask{
		App:          app,
		ProcessType:  "web",
		ResourceType: "deployment",
		Labels:       map[string]string{"tier": "edge", "app.kubernetes.io/component": "api"},
		State:        StatePresent,
	}
	if r := set.Execute(); r.Error != nil {
		t.Fatalf("set labels: %v", r.Error)
	}

	bodies, err := SchedulerK3sLabelsTask{}.ExportApp(app)
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	var found *SchedulerK3sLabelsTask
	for i := range bodies {
		if l, ok := bodies[i].(SchedulerK3sLabelsTask); ok && l.ProcessType == "web" && l.ResourceType == "deployment" {
			found = &l
			break
		}
	}
	if found == nil {
		t.Fatalf("exported labels do not include the web/deployment scope: %+v", bodies)
	}
	if found.Labels["tier"] != "edge" || found.Labels["app.kubernetes.io/component"] != "api" {
		t.Errorf("exported labels mismatch: %+v", found.Labels)
	}
	found.State = StatePresent
	if plan := found.Plan(); !plan.InSync {
		t.Errorf("exported labels should report no drift, got status %v reason %q", plan.Status, plan.Reason)
	}
}

// TestIntegrationSchedulerK3sAutoscalingAuthExport verifies the trigger-auth
// exporter reads the real metadata values back (the dokku#8806 JSON contract)
// so re-planning the exported body reports no drift. Named with the
// TestIntegrationSchedulerK3s prefix so the scheduler-k3s-test CI job's -run
// filter picks it up, and gated like the other scheduler-k3s task tests.
func TestIntegrationSchedulerK3sAutoscalingAuthExport(t *testing.T) {
	skipUnlessSchedulerK3sT(t)

	app := "docket-test-export-k3s-autoscaling-auth"
	destroyApp(app)
	createApp(app)
	defer destroyApp(app)

	cleanup := SchedulerK3sAutoscalingAuthTask{
		App:      app,
		Trigger:  "aws-secret-manager",
		Metadata: map[string]string{"awsRegion": "", "secretName": ""},
		State:    StateAbsent,
	}
	defer cleanup.Execute()

	set := SchedulerK3sAutoscalingAuthTask{
		App:      app,
		Trigger:  "aws-secret-manager",
		Metadata: map[string]string{"awsRegion": "us-east-1", "secretName": "my-secret"},
		State:    StatePresent,
	}
	if r := set.Execute(); r.Error != nil {
		t.Fatalf("set autoscaling-auth: %v", r.Error)
	}

	bodies, err := SchedulerK3sAutoscalingAuthTask{}.ExportApp(app)
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	var found *SchedulerK3sAutoscalingAuthTask
	for i := range bodies {
		if a, ok := bodies[i].(SchedulerK3sAutoscalingAuthTask); ok && a.Trigger == "aws-secret-manager" {
			found = &a
			break
		}
	}
	if found == nil {
		t.Fatalf("exported trigger auth does not include aws-secret-manager: %+v", bodies)
	}
	if found.Metadata["awsRegion"] != "us-east-1" || found.Metadata["secretName"] != "my-secret" {
		t.Errorf("exported metadata mismatch: %+v", found.Metadata)
	}
	found.State = StatePresent
	if plan := found.Plan(); !plan.InSync {
		t.Errorf("exported trigger auth should report no drift, got status %v reason %q", plan.Status, plan.Reason)
	}
}

// TestIntegrationExportServiceCreate verifies the datastore service exporter
// discovers a live service (via the service-list plugin trigger) and
// reconstructs a dokku_service_create that re-plans with no drift. Gated on the
// redis datastore plugin.
func TestIntegrationExportServiceCreate(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "redis")

	serviceType := "redis"
	serviceName := "docket-test-export-svc-create"
	destroyService(serviceType, serviceName)
	if r := (ServiceCreateTask{Service: serviceType, Name: serviceName, State: StatePresent}).Execute(); r.Error != nil {
		t.Fatalf("create service: %v", r.Error)
	}
	defer destroyService(serviceType, serviceName)

	bodies, err := ServiceCreateTask{}.ExportGlobal()
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	var found *ServiceCreateTask
	for i := range bodies {
		if c, ok := bodies[i].(ServiceCreateTask); ok && c.Service == serviceType && c.Name == serviceName {
			found = &c
			break
		}
	}
	if found == nil {
		t.Fatalf("exported services do not include %s:%s: %+v", serviceType, serviceName, bodies)
	}
	found.State = StatePresent
	if plan := found.Plan(); !plan.InSync {
		t.Errorf("exported service create should report no drift, got status %v reason %q", plan.Status, plan.Reason)
	}
}

// TestIntegrationExportServiceExpose verifies the exposed-ports exporter reads
// the host ports back (parsing dokku's `container->host` format) so the exported
// task re-plans with no drift. Gated on the redis datastore plugin.
func TestIntegrationExportServiceExpose(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "redis")

	serviceType := "redis"
	serviceName := "docket-test-export-svc-expose"
	hostPort := "16379"
	destroyService(serviceType, serviceName)
	if r := (ServiceCreateTask{Service: serviceType, Name: serviceName, State: StatePresent}).Execute(); r.Error != nil {
		t.Fatalf("create service: %v", r.Error)
	}
	defer destroyService(serviceType, serviceName)
	if r := (ServiceExposeTask{Service: serviceType, Name: serviceName, Ports: []string{hostPort}, State: StatePresent}).Execute(); r.Error != nil {
		t.Fatalf("expose service: %v", r.Error)
	}

	bodies, err := ServiceExposeTask{}.ExportGlobal()
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	var found *ServiceExposeTask
	for i := range bodies {
		if e, ok := bodies[i].(ServiceExposeTask); ok && e.Service == serviceType && e.Name == serviceName {
			found = &e
			break
		}
	}
	if found == nil {
		t.Fatalf("exported exposes do not include %s:%s: %+v", serviceType, serviceName, bodies)
	}
	if len(found.Ports) != 1 || found.Ports[0] != hostPort {
		t.Errorf("exported expose ports = %v, want [%s]", found.Ports, hostPort)
	}
	found.State = StatePresent
	if plan := found.Plan(); !plan.InSync {
		t.Errorf("exported service expose should report no drift, got status %v reason %q", plan.Status, plan.Reason)
	}
}

// TestIntegrationExportServiceLink verifies the app-scoped link exporter emits a
// dokku_service_link for a linked service, and that the config exporter drops
// the `<ALIAS>_URL` the link injects (so the link stays the single source of
// truth). Gated on the redis datastore plugin and docker links.
func TestIntegrationExportServiceLink(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "redis")
	skipIfDockerLinkUnsupportedT(t)

	appName := "docket-test-export-svc-link-app"
	serviceType := "redis"
	serviceName := "docket-test-export-svc-link"
	destroyApp(appName)
	destroyService(serviceType, serviceName)
	createApp(appName)
	defer destroyApp(appName)
	if r := (ServiceCreateTask{Service: serviceType, Name: serviceName, State: StatePresent}).Execute(); r.Error != nil {
		t.Fatalf("create service: %v", r.Error)
	}
	defer func() {
		(ServiceLinkTask{App: appName, Service: serviceType, Name: serviceName, State: StateAbsent}).Execute()
		destroyService(serviceType, serviceName)
	}()
	if r := (ServiceLinkTask{App: appName, Service: serviceType, Name: serviceName, State: StatePresent}).Execute(); r.Error != nil {
		t.Fatalf("link service: %v", r.Error)
	}

	bodies, err := ServiceLinkTask{}.ExportApp(appName)
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	var found *ServiceLinkTask
	for i := range bodies {
		if l, ok := bodies[i].(ServiceLinkTask); ok && l.Service == serviceType && l.Name == serviceName {
			found = &l
			break
		}
	}
	if found == nil {
		t.Fatalf("exported links do not include %s:%s for %s: %+v", serviceType, serviceName, appName, bodies)
	}
	found.State = StatePresent
	if plan := found.Plan(); !plan.InSync {
		t.Errorf("exported service link should report no drift, got status %v reason %q", plan.Status, plan.Reason)
	}

	// The link injects REDIS_URL into the app's config; config export must drop
	// it, since the link recreates it on apply with the new server's creds.
	cfgBodies, err := ConfigTask{}.ExportApp(appName)
	if err != nil {
		t.Fatalf("config ExportApp: %v", err)
	}
	for _, b := range cfgBodies {
		c := b.(ConfigTask)
		if _, ok := c.Config["REDIS_URL"]; ok {
			t.Errorf("REDIS_URL (a linked-service DSN) should be excluded from config export: %+v", c.Config)
		}
	}
}

// TestIntegrationExportServiceBackup verifies the backup exporter reconstructs
// the schedule/bucket/use_iam from the cron file such that the exported task
// re-plans with no drift; the write-only credentials are not read back. Gated on
// the redis datastore plugin, and skipped if it does not implement backups.
func TestIntegrationExportServiceBackup(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "redis")

	serviceType := "redis"
	serviceName := "docket-test-export-svc-backup"
	destroyService(serviceType, serviceName)
	if r := (ServiceCreateTask{Service: serviceType, Name: serviceName, State: StatePresent}).Execute(); r.Error != nil {
		t.Fatalf("create service: %v", r.Error)
	}
	defer destroyService(serviceType, serviceName)

	schedule := "0 3 * * *"
	bucket := "docket-test-bucket"
	if r := (ServiceBackupTask{Service: serviceType, Name: serviceName, Schedule: schedule, Bucket: bucket, UseIam: true, State: StatePresent}).Execute(); r.Error != nil {
		// not every datastore plugin implements backup-schedule
		t.Skipf("skipping: could not schedule backup (%v)", r.Error)
	}

	bodies, err := ServiceBackupTask{}.ExportGlobal()
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	var found *ServiceBackupTask
	for i := range bodies {
		if b, ok := bodies[i].(ServiceBackupTask); ok && b.Service == serviceType && b.Name == serviceName {
			found = &b
			break
		}
	}
	if found == nil {
		t.Fatalf("exported backups do not include %s:%s: %+v", serviceType, serviceName, bodies)
	}
	if found.Schedule != schedule || found.Bucket != bucket || !found.UseIam {
		t.Errorf("exported backup mismatch: %+v", *found)
	}
	if found.AwsSecretAccessKey != "" || found.EncryptionPassphrase != "" {
		t.Errorf("backup export should not include write-only credentials: %+v", *found)
	}
	found.State = StatePresent
	if plan := found.Plan(); !plan.InSync {
		t.Errorf("exported service backup should report no drift, got status %v reason %q", plan.Status, plan.Reason)
	}
}

// TestIntegrationExportAclService verifies the service-ACL exporter reads the
// members back so the exported task re-plans with no drift. Gated on both the
// redis datastore plugin and dokku-acl (the latter is not installed in the main
// CI job, so this skips there).
func TestIntegrationExportAclService(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "redis")
	skipIfPluginMissingT(t, "acl")

	serviceType := "redis"
	serviceName := "docket-test-export-acl-svc"
	user := "docket-test-acl-user"
	destroyService(serviceType, serviceName)
	if r := (ServiceCreateTask{Service: serviceType, Name: serviceName, State: StatePresent}).Execute(); r.Error != nil {
		t.Fatalf("create service: %v", r.Error)
	}
	defer destroyService(serviceType, serviceName)

	if r := (AclServiceTask{Service: serviceName, Type: serviceType, Users: []string{user}, State: StatePresent}).Execute(); r.Error != nil {
		t.Skipf("skipping: could not add acl user (%v)", r.Error)
	}

	bodies, err := AclServiceTask{}.ExportGlobal()
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}
	var found *AclServiceTask
	for i := range bodies {
		if a, ok := bodies[i].(AclServiceTask); ok && a.Type == serviceType && a.Service == serviceName {
			found = &a
			break
		}
	}
	if found == nil {
		t.Fatalf("exported acls do not include %s:%s: %+v", serviceType, serviceName, bodies)
	}
	foundUser := false
	for _, u := range found.Users {
		if u == user {
			foundUser = true
		}
	}
	if !foundUser {
		t.Errorf("exported acl users %v missing %q", found.Users, user)
	}
	found.State = StatePresent
	if plan := found.Plan(); !plan.InSync {
		t.Errorf("exported acl service should report no drift, got status %v reason %q", plan.Status, plan.Reason)
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
