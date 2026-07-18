package tasks

import (
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

// exampleDeployImage is the image used to deploy a placeholder app when a
// task's examples need a running deploy (ps:scale, service:link, letsencrypt,
// app:clone source). It matches the image the per-task integration tests use.
const exampleDeployImage = "dokku/smoke-test-app:dockerfile"

// exampleReq declares the extra setup a task's documented Examples() need to be
// applied end to end. The zero value (a task absent from exampleIntegrationPolicy)
// applies every example verbatim against the shared placeholder apps it
// references, which is the common case for property, config, toggle, storage,
// domains, and ports tasks. Entries gate a task behind a plugin, service,
// cluster, or env flag, or opt it out with a reason, so a new task is exercised
// by default and only genuinely special ones need an entry.
type exampleReq struct {
	// skip, when non-empty, opts the whole task out of the apply sweep. Its
	// examples are still covered offline by TestAllTaskExamplesValidate.
	skip string
	// envGate names an environment variable that must equal "1" for the task to
	// run, for heavy infra a dedicated CI job provisions.
	envGate string
	// k3s gates the task behind a live scheduler-k3s cluster.
	k3s bool
	// plugins are dokku plugins every example needs; the task skips when any is
	// missing.
	plugins []string
	// ensureApps are apps that must exist (created, not deployed) before the
	// task's setup and examples run - used when the setup needs the app present
	// (e.g. to enable http-auth on it).
	ensureApps []string
	// deployApps are placeholder apps that must be deployed (not just created)
	// before the examples run.
	deployApps []string
	// cleanupApps are extra apps the task creates (e.g. a clone target) that
	// must be destroyed afterward.
	cleanupApps []string
	// freshAppPerExample destroys and recreates the referenced app before each
	// example, so a task whose examples are independent full deploys of the
	// same app (git_from_archive) does not fail the second on unchanged content.
	freshAppPerExample bool
	// requireDockerLink gates the task behind legacy docker --link support.
	requireDockerLink bool
	// setup provisions a backing resource and returns a transform that rewrites
	// each decoded example before it is applied. It is used only where a
	// documented value is a placeholder that cannot apply verbatim (an inline
	// PEM / cert path, or a registry secret); a nil transform means apply
	// verbatim.
	setup func(t *testing.T) (transform func(Task) Task, cleanup func())
}

// exampleIntegrationPolicy maps a registered task name to its apply requirements.
// Tasks not listed apply every example verbatim against the placeholder apps.
var exampleIntegrationPolicy = map[string]exampleReq{
	// Env-gated heavy infra (dedicated CI jobs provision it).
	"dokku_letsencrypt": {
		envGate:    "DOKKU_TEST_LETSENCRYPT",
		plugins:    []string{"letsencrypt"},
		deployApps: []string{"node-js-app"},
		setup:      setupLetsencryptExample,
	},
	"dokku_scheduler_k3s_annotations": {k3s: true},
	"dokku_scheduler_k3s_chart":       {k3s: true},
	"dokku_scheduler_k3s_labels":      {k3s: true},
	"dokku_scheduler_k3s_profile":     {k3s: true},
	"dokku_scheduler_k3s_property":    {k3s: true},

	// Plugin-gated app tasks (the plugins are installed in the integration CI job).
	"dokku_acl_app":              {plugins: []string{"acl"}},
	"dokku_acl_service":          {plugins: []string{"acl"}},
	"dokku_http_auth":            {plugins: []string{"http-auth"}},
	"dokku_http_auth_allowed_ip": {plugins: []string{"http-auth"}, ensureApps: []string{"hello-world"}, setup: setupHttpAuthEnabledExample},
	"dokku_http_auth_domain":     {plugins: []string{"http-auth"}, ensureApps: []string{"hello-world"}, setup: setupHttpAuthDomainExample},
	"dokku_http_auth_user":       {plugins: []string{"http-auth"}, ensureApps: []string{"hello-world"}, setup: setupHttpAuthEnabledExample},
	"dokku_maintenance":          {plugins: []string{"maintenance"}},
	"dokku_maintenance_custom_page": {
		plugins:    []string{"maintenance"},
		ensureApps: []string{"node-js-app"},
		setup:      setupMaintenanceCustomPageExample,
	},
	"dokku_letsencrypt_property": {plugins: []string{"letsencrypt"}},

	// Deploy-dependent tasks.
	"dokku_app_clone":    {deployApps: []string{"node-js-app"}, cleanupApps: []string{"node-js-app-staging"}},
	"dokku_service_link": {plugins: []string{"redis"}, requireDockerLink: true, deployApps: []string{"my-app"}},

	// git_from_archive's two examples are independent full deploys of the same
	// app, so the second must start from a fresh app to avoid an unchanged-tree
	// error on redeploy.
	"dokku_git_from_archive": {freshAppPerExample: true},

	// storage_mount's named-entry examples reference a storage entry that must
	// be created first.
	"dokku_storage_mount": {ensureApps: []string{"node-js-app"}, setup: setupStorageMountExample},

	// Tasks whose documented value is a placeholder the driver must provision
	// (a real inline PEM / cert path, a registry secret, or a maintenance
	// tarball) because it cannot be published in the docs.
	"dokku_certs":         {setup: setupCertsExample},
	"dokku_registry_auth": {setup: setupRegistryExample},

	// Tasks that need infra not present in the integration environment.
	"dokku_ps_scale":                       {skip: "requires a multi-process (web+worker) deploy that a stock image does not provide; covered offline"},
	"dokku_scheduler_k3s_autoscaling_auth":  {skip: "requires KEDA installed on the cluster to apply the trigger-authentication chart; covered offline"},
	"dokku_service_backup":                 {skip: "needs an object-storage backend and credentials; covered offline"},
	"dokku_plugin":                         {skip: "installs/uninstalls a real dokku plugin; covered offline and by TestIntegrationPlugin"},
	"dokku_ssh_key":                        {skip: "manages host ssh keys; covered offline"},
}

// TestIntegrationTaskExamples applies every documented example against a live
// Dokku, so the snippets in docs/tasks/*.md are provably runnable, not just
// offline-valid (TestAllTaskExamplesValidate). For each registered task it
// wraps the example Codeblock as a one-task recipe, loads it through GetTasks
// (exercising the same parse path apply uses), and runs Plan then apply,
// asserting the plan does not error and the applied state reaches the desired
// state. Examples run in source order so a "set"/"create" example precedes its
// paired "clear"/"destroy". Per-task prerequisites and opt-outs live in
// exampleIntegrationPolicy.
func TestIntegrationTaskExamples(t *testing.T) {
	for name, task := range RegisteredTasks {
		policy := exampleIntegrationPolicy[name]
		t.Run(name, func(t *testing.T) {
			skipIfNoDokkuT(t)
			if policy.skip != "" {
				t.Skipf("example apply skipped: %s", policy.skip)
			}
			if policy.k3s {
				skipUnlessSchedulerK3sT(t)
			}
			if policy.envGate != "" && os.Getenv(policy.envGate) != "1" {
				t.Skipf("example apply skipped: %s is not set", policy.envGate)
			}
			for _, plugin := range policy.plugins {
				skipIfPluginMissingT(t, plugin)
			}
			if policy.requireDockerLink {
				skipIfDockerLinkUnsupportedT(t)
			}

			examples, err := task.Examples()
			if err != nil {
				t.Fatalf("Examples() returned error: %v", err)
			}

			// created tracks every app the driver stands up for this task so it
			// can tear them down afterward, whatever app names the examples use.
			created := map[string]bool{}
			t.Cleanup(func() {
				for app := range created {
					destroyApp(app)
				}
				for _, app := range policy.cleanupApps {
					destroyApp(app)
				}
			})
			for _, app := range policy.ensureApps {
				createApp(app)
				created[app] = true
			}
			for _, app := range policy.deployApps {
				deployExampleApp(t, app)
				created[app] = true
			}

			var transform func(Task) Task
			if policy.setup != nil {
				tr, cleanup := policy.setup(t)
				transform = tr
				if cleanup != nil {
					t.Cleanup(cleanup)
				}
			}

			for _, example := range examples {
				t.Run(example.Name, func(t *testing.T) {
					task := loadExampleTask(t, example)
					gateExample(t, name, task)
					ensureExampleApp(t, name, task, created, policy.freshAppPerExample)
					ensureExampleService(t, name, task)
					if transform != nil {
						task = transform(task)
					}
					applyExample(t, example.Name, task)
				})
			}
		})
	}
}

// ensureExampleApp creates the app an example references so an app-scoped apply
// has a real app to act on, tracking it for teardown. It skips the empty app of
// a global example, and does not pre-create for the two tasks that manage the
// app themselves: dokku_app_clone (App is the clone target the example creates,
// torn down via cleanupApps) and dokku_app (its examples create and destroy the
// app, which is only tracked here so a failed run still tears it down).
func ensureExampleApp(t *testing.T, taskName string, task Task, created map[string]bool, fresh bool) {
	t.Helper()
	if taskName == "dokku_app_clone" {
		return
	}
	app := taskStringField(task, "App")
	if app == "" {
		return
	}
	if taskName == "dokku_app" {
		created[app] = true
		return
	}
	if fresh {
		destroyApp(app)
	}
	createApp(app)
	created[app] = true
}

// deployExampleApp deploys the smoke-test image onto app so examples that act on
// a running app (ps:scale, service:link, app:clone source, letsencrypt) have one.
func deployExampleApp(t *testing.T, app string) {
	t.Helper()
	createApp(app)
	result := GitFromImageTask{App: app, Image: exampleDeployImage, State: StateDeployed}.Execute()
	if result.Error != nil {
		t.Fatalf("failed to deploy placeholder app %q: %v", app, result.Error)
	}
}

// loadExampleTask wraps an example Codeblock into a one-task recipe and loads it
// through the real loader, returning the single decoded task.
func loadExampleTask(t *testing.T, example Doc) Task {
	t.Helper()
	out, err := GetTasks(wrapExampleRecipe(example.Codeblock), map[string]interface{}{})
	if err != nil {
		t.Fatalf("example %q: GetTasks: %v", example.Name, err)
	}
	keys := out.Keys()
	if len(keys) != 1 {
		t.Fatalf("example %q: expected exactly one task, got %d", example.Name, len(keys))
	}
	return out.Get(keys[0])
}

// wrapExampleRecipe turns a single-task example Codeblock into a one-task
// recipe the loader accepts, re-indenting the block under a `tasks:` list item.
// The first line is prefixed with the list marker and the rest are indented to
// align beneath it, preserving the block's internal structure.
func wrapExampleRecipe(codeblock string) []byte {
	var b strings.Builder
	b.WriteString("---\n- tasks:\n")
	for i, line := range strings.Split(strings.TrimRight(codeblock, "\n"), "\n") {
		if i == 0 {
			b.WriteString("    - ")
		} else {
			b.WriteString("      ")
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return []byte(b.String())
}

// gateExample skips a single example when the specific backing plugin it needs
// is absent: the service datastore plugin (redis/postgres) for a Service field,
// or global-cert for a global certs example. This is per example, not per task,
// so a task's redis example still runs when only its postgres example must skip.
func gateExample(t *testing.T, taskName string, task Task) {
	t.Helper()
	if service := taskStringField(task, "Service"); service != "" {
		skipIfPluginMissingT(t, service)
	}
	if certs, ok := taskAsPointer(task).(*CertsTask); ok && certs.Global {
		skipIfPluginMissingT(t, "global-cert")
	}
}

// ensureExampleService creates the datastore service an example links, exposes,
// or configures, so the apply has a real service to act on. It is a no-op for
// dokku_service_create, whose own examples create and destroy the service.
func ensureExampleService(t *testing.T, taskName string, task Task) {
	t.Helper()
	if taskName == "dokku_service_create" {
		return
	}
	service := taskStringField(task, "Service")
	name := taskStringField(task, "Name")
	if service == "" || name == "" {
		return
	}
	result := ServiceCreateTask{Service: service, Name: name, State: StatePresent}.Execute()
	if result.Error != nil {
		t.Fatalf("failed to create backing service %s/%s: %v", service, name, result.Error)
	}
	t.Cleanup(func() { destroyService(service, name) })
}

// applyExample runs Plan then apply for one decoded example, asserting the plan
// did not error and the applied state converged on the desired state.
func applyExample(t *testing.T, name string, task Task) {
	t.Helper()
	if plan := task.Plan(); plan.Status == PlanStatusError {
		t.Fatalf("example %q: Plan returned error status: %v", name, plan.Error)
	}
	state := task.Execute()
	if state.Error != nil {
		t.Fatalf("example %q: apply returned error: %v\n  commands: %v\n  exit: %d\n  stdout: %s\n  stderr: %s",
			name, state.Error, state.Commands, state.ExitCode, state.Stdout, state.Stderr)
	}
	if state.State != state.DesiredState {
		t.Errorf("example %q: applied state = %q, want desired state %q", name, state.State, state.DesiredState)
	}
}

// setupCertsExample generates one self-signed cert/key pair and returns a
// transform that points every certs example at real material: the file-path
// examples at the generated paths, the inline-PEM examples at the generated PEM
// contents (the documented truncated PEM cannot apply).
func setupCertsExample(t *testing.T) (func(Task) Task, func()) {
	t.Helper()
	certPath, keyPath := generateSelfSignedCert(t, "node-js-app")
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("failed to read generated cert: %v", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("failed to read generated key: %v", err)
	}
	transform := func(task Task) Task {
		certs, ok := taskAsPointer(task).(*CertsTask)
		if !ok {
			return task
		}
		if certs.Cert != "" {
			certs.Cert = certPath
		}
		if certs.Key != "" {
			certs.Key = keyPath
		}
		if certs.CertContent != "" {
			certs.CertContent = string(certPEM)
		}
		if certs.KeyContent != "" {
			certs.KeyContent = string(keyPEM)
		}
		return certs
	}
	return transform, nil
}

// setupRegistryExample boots a throwaway local registry and returns a transform
// that points every registry_auth example at it with known test credentials,
// since a real registry secret must not live in the docs.
func setupRegistryExample(t *testing.T) (func(Task) Task, func()) {
	t.Helper()
	server := startTestRegistry(t)
	transform := func(task Task) Task {
		auth, ok := taskAsPointer(task).(*RegistryAuthTask)
		if !ok {
			return task
		}
		auth.Server = server
		if auth.State == StatePresent {
			auth.Username = "testuser"
			auth.Password = "testpassword"
		}
		return auth
	}
	return transform, nil
}

// setupHttpAuthEnabledExample enables HTTP auth on the example app so the
// http_auth_user / http_auth_allowed_ip examples, which configure an already
// enabled auth, have an initialized auth config to act on.
func setupHttpAuthEnabledExample(t *testing.T) (func(Task) Task, func()) {
	t.Helper()
	enableHttpAuthExampleApp(t)
	return nil, nil
}

// setupHttpAuthDomainExample enables HTTP auth and adds the domains the
// http_auth_domain examples restrict auth to, since dokku only accepts domains
// that already belong to the app.
func setupHttpAuthDomainExample(t *testing.T) (func(Task) Task, func()) {
	t.Helper()
	enableHttpAuthExampleApp(t)
	result := DomainsTask{App: "hello-world", Domains: []string{"app.example.com", "www.example.com"}, State: StateSet}.Execute()
	if result.Error != nil {
		t.Fatalf("failed to add http-auth-domain example domains: %v", result.Error)
	}
	return nil, nil
}

// enableHttpAuthExampleApp turns on HTTP auth for the shared http-auth example
// app (created via the task's ensureApps).
func enableHttpAuthExampleApp(t *testing.T) {
	t.Helper()
	result := HttpAuthTask{App: "hello-world", Username: "admin", Password: "secret", State: StatePresent}.Execute()
	if result.Error != nil {
		t.Fatalf("failed to enable http-auth on example app: %v", result.Error)
	}
}

// setupStorageMountExample creates the named storage entry the storage_mount
// named-entry examples attach, and removes it afterward.
func setupStorageMountExample(t *testing.T) (func(Task) Task, func()) {
	t.Helper()
	result := StorageEntryTask{Name: "node-js-app-data", Chown: "herokuish", State: StatePresent}.Execute()
	if result.Error != nil {
		t.Fatalf("failed to create storage entry for storage_mount example: %v", result.Error)
	}
	return nil, func() {
		StorageEntryTask{Name: "node-js-app-data", State: StateAbsent}.Execute()
	}
}

// setupMaintenanceCustomPageExample writes a valid maintenance-page tarball to
// the path the tarball example references, since the documented path holds no
// real file. It removes the file afterward.
func setupMaintenanceCustomPageExample(t *testing.T) (func(Task) Task, func()) {
	t.Helper()
	const tarballPath = "/etc/dokku/maintenance/node-js-app.tar"
	tarball, err := buildMaintenancePageTarball("<html><body><h1>Down for maintenance</h1></body></html>\n")
	if err != nil {
		t.Fatalf("failed to build maintenance tarball: %v", err)
	}
	if err := os.MkdirAll("/etc/dokku/maintenance", 0o755); err != nil {
		t.Fatalf("failed to create maintenance dir: %v", err)
	}
	if err := os.WriteFile(tarballPath, tarball, 0o644); err != nil {
		t.Fatalf("failed to write maintenance tarball: %v", err)
	}
	return nil, func() { os.Remove(tarballPath) }
}

// Pebble reaches the app over the docker bridge gateway, and challtestsrv's
// management API (used to publish the A record the ACME HTTP-01 challenge
// resolves) listens there too. These match the addresses the dokku-letsencrypt
// pebble harness uses, which the DOKKU_TEST_LETSENCRYPT CI job stands up.
const (
	letsencryptExampleApp    = "node-js-app"
	letsencryptExampleDomain = "node-js-app.dokku.test"
	challtestsrvURL          = "http://172.17.0.1:8055"
)

// setupLetsencryptExample makes the deployed placeholder app resolvable and
// routable for the ACME HTTP-01 challenge: it publishes an A record for the
// app's domain with challtestsrv and sets that domain on the app, so
// letsencrypt:enable issues a real cert against the pebble ACME server the CI
// job configured. It returns no transform - the enable/disable examples apply
// verbatim - and a cleanup that removes the domain and the A record.
func setupLetsencryptExample(t *testing.T) (func(Task) Task, func()) {
	t.Helper()
	registerChalltestsrvA(t, letsencryptExampleDomain, "172.17.0.1")
	result := DomainsTask{App: letsencryptExampleApp, Domains: []string{letsencryptExampleDomain}, State: StateSet}.Execute()
	if result.Error != nil {
		t.Fatalf("failed to set domain %q for letsencrypt example: %v", letsencryptExampleDomain, result.Error)
	}
	cleanup := func() {
		DomainsTask{App: letsencryptExampleApp, State: StateClear}.Execute()
		clearChalltestsrvA(letsencryptExampleDomain)
	}
	return nil, cleanup
}

// registerChalltestsrvA publishes an A record so the ACME server resolves the
// challenge domain to the host. The host is fully qualified with a trailing dot
// to match the resolver, as the dokku-letsencrypt harness does.
func registerChalltestsrvA(t *testing.T, host, target string) {
	t.Helper()
	body := fmt.Sprintf(`{"host":"%s.","addresses":["%s"]}`, host, target)
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "curl",
		Args:    []string{"-sf", "-X", "POST", "-H", "Content-Type: application/json", "-d", body, challtestsrvURL + "/add-a"},
	})
	if err != nil || result.ExitCode != 0 {
		t.Fatalf("failed to publish A record for %q via challtestsrv: %v", host, err)
	}
}

// clearChalltestsrvA removes a published A record; best effort during cleanup.
func clearChalltestsrvA(host string) {
	body := fmt.Sprintf(`{"host":"%s."}`, host)
	subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "curl",
		Args:    []string{"-sf", "-X", "POST", "-H", "Content-Type: application/json", "-d", body, challtestsrvURL + "/clear-a"},
	})
}

// taskStringField returns the value of a string field on a task struct, or ""
// when the field is absent or not a string. It reads through a pointer so it
// works whether the loader returned a value or pointer task.
func taskStringField(task Task, field string) string {
	v := reflect.Indirect(reflect.ValueOf(task))
	if v.Kind() != reflect.Struct {
		return ""
	}
	f := v.FieldByName(field)
	if !f.IsValid() || f.Kind() != reflect.String {
		return ""
	}
	return f.String()
}

// taskAsPointer returns task as a pointer to its struct so a type switch can
// mutate it, copying a value task into a fresh addressable pointer when needed.
func taskAsPointer(task Task) Task {
	v := reflect.ValueOf(task)
	if v.Kind() == reflect.Ptr {
		return task
	}
	p := reflect.New(v.Type())
	p.Elem().Set(v)
	return p.Interface().(Task)
}
