package tasks

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

// exampleReq declares what applying a task's documented examples end to end
// needs beyond their published requirements. Everything a recipe could
// express - the plugins, the apps and services an example names, a deployed
// app, a resource it attaches - is the example's `requires` list in the
// catalog, and the driver applies that list before the examples run. What is
// left here is what a task cannot express: the test environment itself (a
// k3s cluster, an env-gated harness, legacy docker links), an opt-out, how
// the examples relate to each other, and a documented value that is a
// placeholder the driver must replace. The zero value (a task absent from
// exampleIntegrationPolicy) applies every example verbatim after its
// requirements, which is the common case.
type exampleReq struct {
	// skip, when non-empty, opts the whole task out of the apply sweep. Its
	// examples are still covered offline by TestAllTaskExamplesValidate.
	skip string
	// envGate names an environment variable that must equal "1" for the task to
	// run, for heavy infra a dedicated CI job provisions.
	envGate string
	// k3s gates the task behind a live scheduler-k3s cluster.
	k3s bool
	// requireDockerLink gates the task behind legacy docker --link support, a
	// property of the docker daemon rather than of any task.
	requireDockerLink bool
	// freshRequiresPerExample tears down and re-applies an example's
	// requirements before it runs, so a task whose examples are independent
	// full deploys of the same app (git_from_archive) does not fail the second
	// on unchanged content.
	freshRequiresPerExample bool
	// setup provisions what no task can and returns a transform that rewrites
	// each decoded example before it is applied. It is used only where a
	// documented value is a placeholder that cannot apply verbatim (an inline
	// PEM / cert path, a registry secret, a file on the runner) or where the
	// example needs test infra (an ACME DNS record); a nil transform means
	// apply verbatim. It runs after every example's requirements are applied.
	setup func(t *testing.T) (transform func(Task) Task, cleanup func())
}

// exampleIntegrationPolicy maps a registered task name to its apply requirements.
// Tasks not listed apply every example verbatim after its requirements.
var exampleIntegrationPolicy = map[string]exampleReq{
	// Env-gated heavy infra (dedicated CI jobs provision it).
	"dokku_letsencrypt": {
		envGate: "DOKKU_TEST_LETSENCRYPT",
		setup:   setupLetsencryptExample,
	},
	"dokku_scheduler_k3s_annotations":  {k3s: true},
	"dokku_scheduler_k3s_chart":        {k3s: true},
	"dokku_scheduler_k3s_labels":       {k3s: true},
	"dokku_scheduler_k3s_node_sysctls": {k3s: true},
	"dokku_scheduler_k3s_profile":      {k3s: true},
	"dokku_scheduler_k3s_property":     {k3s: true},

	// The tarball example names a file on the runner, which no task writes.
	"dokku_maintenance_custom_page": {setup: setupMaintenanceCustomPageExample},

	// service_link needs legacy docker links on the daemon.
	"dokku_service_link": {requireDockerLink: true},

	// git_from_archive's two examples are independent full deploys of the same
	// app, so the second must start from a fresh app to avoid an unchanged-tree
	// error on redeploy.
	"dokku_git_from_archive": {freshRequiresPerExample: true},

	// Tasks whose documented value is a placeholder the driver must provision
	// (a real inline PEM / cert path, or a registry secret) because it cannot
	// be published in the docs.
	"dokku_certs":         {setup: setupCertsExample},
	"dokku_registry_auth": {setup: setupRegistryExample},

	// Tasks that need infra not present in the integration environment.
	"dokku_ps_scale":                       {skip: "requires a multi-process (web+worker) deploy that a stock image does not provide; covered offline"},
	"dokku_scheduler_k3s_autoscaling_auth": {skip: "requires KEDA installed on the cluster to apply the trigger-authentication chart; covered offline"},
	"dokku_service_backup":                 {skip: "needs an object-storage backend and credentials; covered offline"},
	"dokku_plugin":                         {skip: "installs/uninstalls a real dokku plugin; covered offline and by TestIntegrationPlugin"},
	"dokku_ssh_key":                        {skip: "manages host ssh keys; covered offline"},
}

// TestIntegrationTaskExamples applies every documented example against a live
// Dokku, so the snippets in docs/tasks/*.md are provably runnable, not just
// offline-valid (TestAllTaskExamplesValidate). For each registered task it
// reads the examples from the catalog, applies each example's published
// `requires` list, then wraps the example as a one-task recipe, loads it
// through GetTasks (exercising the same parse path apply uses), and runs Plan
// then apply, asserting the plan does not error and the applied state reaches
// the desired state. Examples run in source order so a "set"/"create" example
// precedes its paired "clear"/"destroy". What the requires list cannot express
// lives in exampleIntegrationPolicy.
func TestIntegrationTaskExamples(t *testing.T) {
	for name, task := range allRegisteredTasks() {
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
			if policy.requireDockerLink {
				skipIfDockerLinkUnsupportedT(t)
			}

			schema, err := TaskSchemaOf(name, task)
			if err != nil {
				t.Fatalf("TaskSchemaOf returned error: %v", err)
			}

			// Every example's requirements go on before any example runs, and
			// come off, with every object an example created, once they all
			// have.
			setup := &exampleSetup{}
			t.Cleanup(setup.teardown)
			unmet := map[string]string{}
			for _, example := range schema.Examples {
				if reason := setup.apply(t, example.Requires); reason != "" {
					unmet[example.Name] = reason
				}
			}

			var transform func(Task) Task
			if policy.setup != nil {
				tr, cleanup := policy.setup(t)
				transform = tr
				if cleanup != nil {
					t.Cleanup(cleanup)
				}
			}

			for _, example := range schema.Examples {
				t.Run(example.Name, func(t *testing.T) {
					if reason := unmet[example.Name]; reason != "" {
						t.Skipf("example apply skipped: %s", reason)
					}
					if policy.freshRequiresPerExample {
						setup.reapply(t, example.Requires)
					}
					task := loadExampleTask(t, example)
					if transform != nil {
						task = transform(task)
					}
					setup.trackCreated(task)
					applyExample(t, example.Name, task)
					registerGlobalPropertyRevert(t, task)
				})
			}
		})
	}
}

// exampleSetup applies the requirements of one task's examples and undoes
// them afterwards.
type exampleSetup struct {
	// applied are the required tasks applied, in order, by identity address.
	applied []Task
	seen    map[string]bool
	// created are the objects the examples themselves create, as the tasks
	// that destroy them.
	created []Task
}

// apply applies each requirement not yet applied, in order. A dokku_plugin
// requirement is never installed: CI's setup-dokku action provisions the
// plugins, and a missing one returns the reason the example skips, before the
// requirements that need it are applied.
func (s *exampleSetup) apply(t *testing.T, requires []ExampleRequirementSchema) string {
	t.Helper()
	for _, required := range requires {
		task, err := decodeExampleCodeblock(required.YAML)
		if err != nil {
			t.Fatalf("required %s does not decode: %v", required.Type, err)
		}
		if plugin, ok := taskAsPointer(task).(*PluginTask); ok {
			if !dokkuPluginInstalled(plugin.Name) {
				return fmt.Sprintf("dokku plugin %q not installed", plugin.Name)
			}
			continue
		}
		address := IdentityAddress(required.Type, task)
		if s.seen[address] {
			continue
		}
		if s.seen == nil {
			s.seen = map[string]bool{}
		}
		s.seen[address] = true
		s.applied = append(s.applied, task)
		applyRequirement(t, address, task)
	}
	return ""
}

// reapply tears down the given requirements and applies them again, so the
// example starts from what they alone produce.
func (s *exampleSetup) reapply(t *testing.T, requires []ExampleRequirementSchema) {
	t.Helper()
	for i := len(requires) - 1; i >= 0; i-- {
		task, err := decodeExampleCodeblock(requires[i].YAML)
		if err != nil {
			t.Fatalf("required %s does not decode: %v", requires[i].Type, err)
		}
		if _, ok := taskAsPointer(task).(*PluginTask); ok {
			continue
		}
		tearDownTask(task)
	}
	for _, required := range requires {
		task, err := decodeExampleCodeblock(required.YAML)
		if err != nil {
			t.Fatalf("required %s does not decode: %v", required.Type, err)
		}
		if _, ok := taskAsPointer(task).(*PluginTask); ok {
			continue
		}
		applyRequirement(t, IdentityAddress(required.Type, task), task)
	}
}

// trackCreated records each object task creates, so it is destroyed after
// the examples whatever the examples left standing.
func (s *exampleSetup) trackCreated(task Task) {
	for _, object := range TaskObjects(task) {
		if object.Creates && object.Set {
			if creator := objectCreator(object); creator != nil {
				s.created = append(s.created, creator)
			}
		}
	}
}

// teardown destroys what the examples created, then tears down the applied
// requirements in reverse order. It is best effort: a failure leaves residue
// on a throwaway CI server but cannot fail a passing example.
func (s *exampleSetup) teardown() {
	for i := len(s.created) - 1; i >= 0; i-- {
		tearDownTask(s.created[i])
	}
	for i := len(s.applied) - 1; i >= 0; i-- {
		tearDownTask(s.applied[i])
	}
}

// applyRequirement plans and applies one required task, failing the test
// when it does not converge.
func applyRequirement(t *testing.T, address string, task Task) {
	t.Helper()
	if plan := task.Plan(testCtx()); plan.Status == PlanStatusError {
		t.Fatalf("required %s: Plan returned error status: %v", address, plan.Error)
	}
	state := task.Execute(testCtx())
	if state.Error != nil {
		t.Fatalf("required %s: apply returned error: %v\n  commands: %v\n  stdout: %s\n  stderr: %s",
			address, state.Error, state.Commands, state.Stdout, state.Stderr)
	}
}

// tearDownTask moves task into its teardown state and applies it. A task
// with no teardown, such as a deploy, is left alone; destroying its app
// removes it.
func tearDownTask(task Task) {
	teardown := TaskLifecycle(task).Teardown
	if teardown == "" {
		return
	}
	destroy, err := TaskInState(task, teardown)
	if err != nil {
		return
	}
	destroy.Execute(testCtx())
}

// loadExampleTask wraps an example snippet into a one-task recipe and loads it
// through the real loader, returning the single decoded task.
func loadExampleTask(t *testing.T, example ExampleSchema) Task {
	t.Helper()
	out, err := GetTasks(wrapExampleRecipe(example.YAML), map[string]interface{}{})
	if err != nil {
		t.Fatalf("example %q: GetTasks: %v", example.Name, err)
	}
	keys := out.Keys()
	if len(keys) != 1 {
		t.Fatalf("example %q: expected exactly one task, got %d", example.Name, len(keys))
	}
	return out.Get(keys[0])
}

// wrapExampleRecipe turns a single-task snippet into a one-task recipe the
// loader accepts, the same way exampleRecipe lays out a complete one.
func wrapExampleRecipe(codeblock string) []byte {
	return exampleRecipe(ExampleSchema{YAML: codeblock})
}

// applyExample runs Plan then apply for one decoded example, asserting the plan
// did not error and the applied state converged on the desired state.
func applyExample(t *testing.T, name string, task Task) {
	t.Helper()
	if plan := task.Plan(testCtx()); plan.Status == PlanStatusError {
		t.Fatalf("example %q: Plan returned error status: %v", name, plan.Error)
	}
	state := task.Execute(testCtx())
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
	return nil, func() { _ = os.Remove(tarballPath) }
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
	result := DomainsTask{App: letsencryptExampleApp, Domains: []string{letsencryptExampleDomain}, State: StateSet}.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to set domain %q for letsencrypt example: %v", letsencryptExampleDomain, result.Error)
	}
	cleanup := func() {
		DomainsTask{App: letsencryptExampleApp, State: StateClear}.Execute(testCtx())
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
	result, err := subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
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
	_, _ = subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "curl",
		Args:    []string{"-sf", "-X", "POST", "-H", "Content-Type: application/json", "-d", body, challtestsrvURL + "/clear-a"},
	})
}

// registerGlobalPropertyRevert clears a global property an example just set, so
// the setting does not persist and break later app builds or deploys. Property
// tasks' "Setting a global X" examples have no global-clear counterpart (their
// clear example targets the per-app scope), and a leftover global registry
// server, network attachment, or similar makes every subsequent deploy fail.
// It is a no-op for any task that is not a present/set of a global property.
func registerGlobalPropertyRevert(t *testing.T, task Task) {
	t.Helper()
	v := reflect.Indirect(reflect.ValueOf(taskAsPointer(task)))
	if v.Kind() != reflect.Struct {
		return
	}
	global := v.FieldByName("Global")
	property := v.FieldByName("Property")
	state := v.FieldByName("State")
	if !global.IsValid() || global.Kind() != reflect.Bool || !global.Bool() {
		return
	}
	if !property.IsValid() || property.Kind() != reflect.String || property.String() == "" {
		return
	}
	if !state.IsValid() || state.Kind() != reflect.String {
		return
	}
	if s := State(state.String()); s != StatePresent && s != StateSet {
		return
	}
	clear := reflect.New(v.Type())
	clear.Elem().Set(v)
	clear.Elem().FieldByName("State").Set(reflect.ValueOf(StateAbsent))
	if value := clear.Elem().FieldByName("Value"); value.IsValid() && value.Kind() == reflect.String {
		value.SetString("")
	}
	clearTask, ok := clear.Interface().(Task)
	if !ok {
		return
	}
	t.Cleanup(func() { clearTask.Execute(testCtx()) })
}

// taskAsPointer returns task as a pointer to its struct so a type switch can
// mutate it, copying a value task into a fresh addressable pointer when needed.
func taskAsPointer(task Task) Task {
	v := reflect.ValueOf(task)
	if v.Kind() == reflect.Pointer {
		return task
	}
	p := reflect.New(v.Type())
	p.Elem().Set(v)
	return p.Interface().(Task)
}
