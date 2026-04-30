package tasks

import (
	"strings"
	"testing"

	_ "github.com/gliderlabs/sigil/builtin"
)

func TestGetTasksEmptyRecipe(t *testing.T) {
	data := []byte("---\n")
	context := map[string]interface{}{}

	_, err := GetTasks(data, context)
	if err == nil {
		t.Fatal("GetTasks with empty recipe should return an error")
	}

	if !strings.Contains(err.Error(), "no recipe found") {
		t.Errorf("expected 'no recipe found' error, got: %v", err)
	}
}

func TestGetTasksEmptyList(t *testing.T) {
	data := []byte("---\n- tasks: []\n")
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks with empty task list should not error, got: %v", err)
	}

	if len(tasks.Keys()) != 0 {
		t.Errorf("expected 0 tasks, got %d", len(tasks.Keys()))
	}
}

func TestGetTasksValidAppTask(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: create test app
      dokku_app:
        app: test-app
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks should not error for valid app task, got: %v", err)
	}

	if len(tasks.Keys()) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks.Keys()))
	}

	task := tasks.Get("create test app")
	if task == nil {
		t.Fatal("task 'create test app' not found")
	}

	appTask, ok := task.(*AppTask)
	if !ok {
		t.Fatalf("task is not an AppTask (type is %T)", task)
	}
	if appTask.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", appTask.State)
	}
}

func TestGetTasksInvalidTaskType(t *testing.T) {
	data := []byte(`---
- tasks:
    - dokku_nonexistent:
        app: test-app
`)
	context := map[string]interface{}{}

	_, err := GetTasks(data, context)
	if err == nil {
		t.Fatal("GetTasks with invalid task type should return an error")
	}

	// An unknown task type lands in the unknown-envelope-key bucket and
	// is reported with a "did you mean" hint pointing at the closest
	// registered task name.
	if !strings.Contains(err.Error(), "unknown envelope key") {
		t.Errorf("expected 'unknown envelope key' error, got: %v", err)
	}
}

func TestGetTasksTooManyProperties(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: test
      dokku_app:
        app: test-app
      dokku_config:
        app: test-app
`)
	context := map[string]interface{}{}

	_, err := GetTasks(data, context)
	if err == nil {
		t.Fatal("GetTasks with two task-type keys should return an error")
	}

	// The cap is now expressed as "exactly one task-type key per entry"
	// instead of the legacy `len(t) > 2` heuristic so envelope keys
	// (name, tags, when, loop, register, ...) can coexist with the
	// task-type key.
	if !strings.Contains(err.Error(), "task-type keys") {
		t.Errorf("expected 'task-type keys' error, got: %v", err)
	}
}

func TestGetTasksAutoGeneratesName(t *testing.T) {
	data := []byte(`---
- tasks:
    - dokku_app:
        app: test-app
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks should not error for nameless task, got: %v", err)
	}

	keys := tasks.Keys()
	if len(keys) != 1 {
		t.Fatalf("expected 1 task, got %d", len(keys))
	}

	if !strings.HasPrefix(keys[0], "task #1 ") {
		t.Errorf("expected auto-generated name starting with 'task #1 ', got '%s'", keys[0])
	}
}

func TestGetTasksWithTemplateContext(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: create {{ .app_name }}
      dokku_app:
        app: {{ .app_name }}
`)
	context := map[string]interface{}{
		"app_name": "my-app",
	}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks should not error with template context, got: %v", err)
	}

	if len(tasks.Keys()) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks.Keys()))
	}

	task := tasks.Get("create my-app")
	if task == nil {
		t.Fatal("task 'create my-app' not found")
	}
}

func TestRegisteredTasksExist(t *testing.T) {
	expectedTasks := []string{
		"dokku_acl_app",
		"dokku_acl_service",
		"dokku_app",
		"dokku_app_clone",
		"dokku_app_json_property",
		"dokku_app_lock",
		"dokku_builder_dockerfile_property",
		"dokku_builder_herokuish_property",
		"dokku_builder_lambda_property",
		"dokku_builder_nixpacks_property",
		"dokku_builder_pack_property",
		"dokku_builder_property",
		"dokku_builder_railpack_property",
		"dokku_buildpacks",
		"dokku_caddy_property",
		"dokku_certs",
		"dokku_checks_property",
		"dokku_checks_toggle",
		"dokku_config",
		"dokku_cron_property",
		"dokku_docker_options",
		"dokku_domains",
		"dokku_domains_toggle",
		"dokku_git_auth",
		"dokku_git_from_archive",
		"dokku_git_from_image",
		"dokku_git_property",
		"dokku_git_sync",
		"dokku_haproxy_property",
		"dokku_http_auth",
		"dokku_letsencrypt",
		"dokku_letsencrypt_property",
		"dokku_logs_property",
		"dokku_network",
		"dokku_network_property",
		"dokku_nginx_property",
		"dokku_openresty_property",
		"dokku_ports",
		"dokku_proxy_toggle",
		"dokku_ps_property",
		"dokku_ps_scale",
		"dokku_registry_auth",
		"dokku_registry_property",
		"dokku_resource_limit",
		"dokku_resource_reserve",
		"dokku_scheduler_docker_local_property",
		"dokku_scheduler_k3s_property",
		"dokku_scheduler_property",
		"dokku_service_create",
		"dokku_service_link",
		"dokku_storage_ensure",
		"dokku_storage_mount",
		"dokku_traefik_property",
	}

	for _, name := range expectedTasks {
		if _, ok := RegisteredTasks[name]; !ok {
			t.Errorf("expected task %q to be registered", name)
		}
	}
}

func TestGetTasksMultipleTasks(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: create app
      dokku_app:
        app: test-app
    - name: set config
      dokku_config:
        app: test-app
        config:
          KEY: VALUE
    - name: mount storage
      dokku_storage_mount:
        app: test-app
        host_dir: /host
        container_dir: /container
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	keys := tasks.Keys()
	if len(keys) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(keys))
	}

	expectedNames := []string{"create app", "set config", "mount storage"}
	for i, name := range expectedNames {
		if keys[i] != name {
			t.Errorf("task[%d] = %q, want %q", i, keys[i], name)
		}
		if tasks.Get(name) == nil {
			t.Errorf("task %q not found", name)
		}
	}
}

func TestGetTasksTaskWithDefaultState(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: create app
      dokku_app:
        app: test-app
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("create app")
	if task == nil {
		t.Fatal("task not found")
	}

	appTask, ok := task.(*AppTask)
	if !ok {
		t.Fatalf("task is not an AppTask (type is %T)", task)
	}
	if appTask.State != StatePresent {
		t.Errorf("expected default state 'present', got %q", appTask.State)
	}
}

func TestGetTasksInvalidYaml(t *testing.T) {
	data := []byte("not valid yaml: [[[")
	context := map[string]interface{}{}

	_, err := GetTasks(data, context)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestGetTasksSigilTemplateError(t *testing.T) {
	data := []byte(`---
- tasks:
    - dokku_app:
        app: {{ .broken
`)
	context := map[string]interface{}{}

	_, err := GetTasks(data, context)
	if err == nil {
		t.Fatal("expected error for bad template syntax")
	}
	if !strings.Contains(err.Error(), "re-render error") {
		t.Errorf("expected 're-render error', got: %v", err)
	}
}

func TestGetTasksTwoPropertiesNoName(t *testing.T) {
	data := []byte(`---
- tasks:
    - dokku_app:
        app: test-app
      dokku_config:
        app: test-app
`)
	context := map[string]interface{}{}

	_, err := GetTasks(data, context)
	if err == nil {
		t.Fatal("expected error for two task-type keys without name")
	}
	// Two task-type keys (with or without a name) collapse onto the
	// same "exactly one task-type key" rule.
	if !strings.Contains(err.Error(), "task-type keys") {
		t.Errorf("expected 'task-type keys' error, got: %v", err)
	}
}

func TestGetTasksFromRealExample(t *testing.T) {
	data := []byte(`---
- inputs:
    - name: app
      description: "Name of app to be deployed"
      type: string
      required: true
    - name: image
      default: "lscr.io/linuxserver/adguardhome-sync:latest"
      description: "Image to be deployed"
  tasks:
    - name: create app
      dokku_app:
        app: {{ .app | default "" }}
    - name: set config
      dokku_config:
        app: {{ .app | default "" }}
        restart: false
        config:
          PUID: "1000"
          PGID: "1000"
          TZ: "Europe/UTC"
          CONFIGFILE: "/config/adguardhome-sync.yaml"
    - name: ensure storage
      dokku_storage_ensure:
        app: {{ .app | default "" }}
        chown: "heroku"
    - name: mount storage
      dokku_storage_mount:
        app: {{ .app | default "" }}
        host_dir: "/var/lib/dokku/data/storage/{{ .app | default "" }}"
        container_dir: "/config"
    - name: set ports
      dokku_ports:
        app: {{ .app | default "" }}
        port_mappings:
          - scheme: http
            host: 80
            container: 8080
    - name: deploy image
      dokku_git_from_image:
        app: {{ .app | default "" }}
        image: {{ .image | default "" }}
`)
	context := map[string]interface{}{
		"app":   "test-adguard",
		"image": "lscr.io/linuxserver/adguardhome-sync:latest",
	}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	keys := tasks.Keys()
	if len(keys) != 6 {
		t.Fatalf("expected 6 tasks, got %d", len(keys))
	}

	expectedNames := []string{
		"create app",
		"set config",
		"ensure storage",
		"mount storage",
		"set ports",
		"deploy image",
	}
	for i, name := range expectedNames {
		if keys[i] != name {
			t.Errorf("task[%d] = %q, want %q", i, keys[i], name)
		}
	}

	// verify template context was applied to app task
	appTaskRaw := tasks.Get("create app")
	appTask, ok := appTaskRaw.(*AppTask)
	if !ok {
		at, ok2 := appTaskRaw.(AppTask)
		if !ok2 {
			t.Fatalf("create app is not an AppTask (type is %T)", appTaskRaw)
		}
		appTask = &at
	}
	if appTask.App != "test-adguard" {
		t.Errorf("AppTask.App = %q, want %q", appTask.App, "test-adguard")
	}

	// verify config task has expected config keys
	configTaskRaw := tasks.Get("set config")
	configTask2, ok := configTaskRaw.(*ConfigTask)
	if !ok {
		ct, ok2 := configTaskRaw.(ConfigTask)
		if !ok2 {
			t.Fatalf("set config is not a ConfigTask (type is %T)", configTaskRaw)
		}
		configTask2 = &ct
	}
	if len(configTask2.Config) != 4 {
		t.Errorf("expected 4 config keys, got %d", len(configTask2.Config))
	}

	// verify port mapping was parsed
	portsTaskRaw := tasks.Get("set ports")
	portsTask2, ok := portsTaskRaw.(*PortsTask)
	if !ok {
		pt, ok2 := portsTaskRaw.(PortsTask)
		if !ok2 {
			t.Fatalf("set ports is not a PortsTask (type is %T)", portsTaskRaw)
		}
		portsTask2 = &pt
	}
	if len(portsTask2.PortMappings) != 1 {
		t.Errorf("expected 1 port mapping, got %d", len(portsTask2.PortMappings))
	}
	if portsTask2.PortMappings[0].String() != "http:80:8080" {
		t.Errorf("port mapping = %q, want %q", portsTask2.PortMappings[0].String(), "http:80:8080")
	}

	// verify git from image was parsed with template context
	gitTaskRaw := tasks.Get("deploy image")
	gitTask, ok := gitTaskRaw.(*GitFromImageTask)
	if !ok {
		gt, ok2 := gitTaskRaw.(GitFromImageTask)
		if !ok2 {
			t.Fatalf("deploy image is not a GitFromImageTask (type is %T)", gitTaskRaw)
		}
		gitTask = &gt
	}
	if gitTask.Image != "lscr.io/linuxserver/adguardhome-sync:latest" {
		t.Errorf("Image = %q, want %q", gitTask.Image, "lscr.io/linuxserver/adguardhome-sync:latest")
	}
}

func TestAllTasksExamplesReturnNoError(t *testing.T) {
	for name, task := range RegisteredTasks {
		t.Run(name, func(t *testing.T) {
			_, err := task.Examples()
			if err != nil {
				t.Errorf("Examples() returned error: %v", err)
			}
		})
	}
}

func TestRegisteredTaskCount(t *testing.T) {
	expected := 55
	if got := len(RegisteredTasks); got != expected {
		t.Errorf("expected %d registered tasks, got %d", expected, got)
	}
}

func TestGetTasksEnvelopeWithTagsAndWhen(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: deploy api
      tags: [api, deploy]
      when: 'env == "prod"'
      dokku_app:
        app: api
`)
	out, err := GetTasks(data, map[string]interface{}{"env": "prod"})
	if err != nil {
		t.Fatalf("GetTasks: %v", err)
	}
	env := out.GetEnvelope("deploy api")
	if env == nil {
		t.Fatal("envelope not found")
	}
	if got := env.Tags; len(got) != 2 || got[0] != "api" || got[1] != "deploy" {
		t.Errorf("tags = %v, want [api deploy]", got)
	}
	if env.When != `env == "prod"` {
		t.Errorf("when = %q", env.When)
	}
	if env.WhenProgram() == nil {
		t.Error("expected when to be pre-compiled")
	}
}

func TestGetTasksRejectsUnknownEnvelopeKey(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: x
      onerror: ignore
      dokku_app:
        app: x
`)
	_, err := GetTasks(data, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for unknown envelope key")
	}
	if !strings.Contains(err.Error(), "unknown envelope key") {
		t.Errorf("got: %v", err)
	}
}

func TestGetTasksUnknownEnvelopeKeyDidYouMean(t *testing.T) {
	// "tag" is one Levenshtein step from "tags" so the suggestion fires.
	data := []byte(`---
- tasks:
    - name: x
      tag: foo
      dokku_app:
        app: x
`)
	_, err := GetTasks(data, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for unknown envelope key")
	}
	if !strings.Contains(err.Error(), `did you mean "tags"`) {
		t.Errorf("expected 'did you mean tags' hint, got: %v", err)
	}
}

func TestGetTasksAcceptsMultipleEnvelopeKeys(t *testing.T) {
	// Pre-#205 the parser capped each entry at 2 keys (`name` plus exactly
	// one `dokku_*` key). Verify the cap is gone: name + tags + when +
	// dokku_app should now parse cleanly.
	data := []byte(`---
- tasks:
    - name: deploy api
      tags: [api]
      when: 'true'
      dokku_app:
        app: api
`)
	out, err := GetTasks(data, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetTasks: %v", err)
	}
	if len(out.Keys()) != 1 {
		t.Errorf("expected 1 task, got %d", len(out.Keys()))
	}
}

func TestGetTasksReservedEnvelopeKeysDecode(t *testing.T) {
	// register / changed_when / failed_when / ignore_errors are reserved
	// by #210; the loader still decodes them so #210 does not have to
	// revisit the envelope-key allowlist.
	data := []byte(`---
- tasks:
    - name: deploy api
      register: app_result
      changed_when: 'result.changed'
      failed_when: 'result.failed'
      ignore_errors: true
      dokku_app:
        app: api
`)
	out, err := GetTasks(data, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetTasks: %v", err)
	}
	env := out.GetEnvelope("deploy api")
	if env.Register != "app_result" {
		t.Errorf("register = %q", env.Register)
	}
	if env.ChangedWhen != "result.changed" {
		t.Errorf("changed_when = %q", env.ChangedWhen)
	}
	if env.FailedWhen != "result.failed" {
		t.Errorf("failed_when = %q", env.FailedWhen)
	}
	if !env.IgnoreErrors {
		t.Errorf("ignore_errors = %v", env.IgnoreErrors)
	}
}

func TestGetTasksTagsScalarFormDecodes(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: deploy api
      tags: api
      dokku_app:
        app: api
`)
	out, err := GetTasks(data, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetTasks: %v", err)
	}
	env := out.GetEnvelope("deploy api")
	if got := env.Tags; len(got) != 1 || got[0] != "api" {
		t.Errorf("tags = %v, want [api]", got)
	}
}

func TestTaskDocStrings(t *testing.T) {
	tests := []struct {
		task Task
		want string
	}{
		{&AclAppTask{}, "Manages the dokku-acl access list for a dokku application"},
		{&AclServiceTask{}, "Manages the dokku-acl access list for a dokku service"},
		{&AppTask{}, "Creates or destroys an app"},
		{&AppCloneTask{}, "Clones an existing dokku app to a new app"},
		{&AppJsonPropertyTask{}, "Manages the app.json configuration for a given dokku application"},
		{&AppLockTask{}, "Locks or unlocks a dokku application from deployment"},
		{&BuilderDockerfilePropertyTask{}, "Manages the builder-dockerfile configuration for a given dokku application"},
		{&BuilderHerokuishPropertyTask{}, "Manages the builder-herokuish configuration for a given dokku application"},
		{&BuilderLambdaPropertyTask{}, "Manages the builder-lambda configuration for a given dokku application"},
		{&BuilderNixpacksPropertyTask{}, "Manages the builder-nixpacks configuration for a given dokku application"},
		{&BuilderPackPropertyTask{}, "Manages the builder-pack configuration for a given dokku application"},
		{&BuilderPropertyTask{}, "Manages the builder configuration for a given dokku application"},
		{&BuilderRailpackPropertyTask{}, "Manages the builder-railpack configuration for a given dokku application"},
		{&BuildpacksPropertyTask{}, "Manages the buildpacks configuration for a given dokku application"},
		{&BuildpacksTask{}, "Manages the buildpacks for a given dokku application"},
		{&CaddyPropertyTask{}, "Manages the caddy configuration for a given dokku application"},
		{&CertsTask{}, "Manages SSL certificates for a dokku app or globally. The `cert` and `key` fields are paths on the dokku server, so when running with `DOKKU_HOST` set the referenced files must already exist on the remote host - docket does not upload them."},
		{&ChecksPropertyTask{}, "Manages the checks configuration for a given dokku application"},
		{&ChecksToggleTask{}, "Enables or disables the checks plugin for a given dokku application"},
		{&ConfigTask{}, "Manages the configuration for a given dokku application"},
		{&CronPropertyTask{}, "Manages the cron configuration for a given dokku application"},
		{&DockerOptionsTask{}, "Manages docker-options for a given dokku application"},
		{&DomainsTask{}, "Manages the domains for a given dokku application or globally"},
		{&DomainsToggleTask{}, "Enables or disables the domains plugin for a given dokku application"},
		{&GitAuthTask{}, "Manages netrc credentials for a git host"},
		{&GitFromArchiveTask{}, "Deploys a git repository from an archive URL"},
		{&GitFromImageTask{}, "Deploys a git repository from a docker image"},
		{&GitPropertyTask{}, "Manages the git configuration for a given dokku application"},
		{&GitSyncTask{}, "Syncs a git repository to a dokku application"},
		{&HaproxyPropertyTask{}, "Manages the haproxy configuration for a given dokku application"},
		{&HttpAuthTask{}, "Manages HTTP authentication for a given dokku application"},
		{&LetsencryptTask{}, "Enables or disables letsencrypt SSL certificates for a dokku application"},
		{&LetsencryptPropertyTask{}, "Manages the letsencrypt configuration for a given dokku application"},
		{&LogsPropertyTask{}, "Manages the logs configuration for a given dokku application"},
		{&NetworkTask{}, "Creates or destroys a Docker network"},
		{&NetworkPropertyTask{}, "Manages the network property for a given dokku application"},
		{&NginxPropertyTask{}, "Manages the nginx configuration for a given dokku application"},
		{&OpenrestyPropertyTask{}, "Manages the openresty configuration for a given dokku application"},
		{&PortsTask{}, "Manages the ports for a given dokku application"},
		{&PsScaleTask{}, "Manages the process scale for a given dokku application"},
		{&RegistryAuthTask{}, "Manages docker registry authentication for a dokku application or globally"},
		{&RegistryPropertyTask{}, "Manages the registry configuration for a given dokku application"},
		{&ResourceLimitTask{}, "Manages the resource limits for a given dokku application"},
		{&ResourceReserveTask{}, "Manages the resource reservations for a given dokku application"},
		{&SchedulerDockerLocalPropertyTask{}, "Manages the scheduler-docker-local configuration for a given dokku application"},
		{&SchedulerK3sPropertyTask{}, "Manages the scheduler-k3s configuration for a given dokku application"},
		{&SchedulerPropertyTask{}, "Manages the scheduler configuration for a given dokku application"},
		{&ServiceCreateTask{}, "Creates or destroys a dokku service"},
		{&ServiceLinkTask{}, "Links or unlinks a dokku service to an app"},
		{&ProxyToggleTask{}, "Enables or disables the proxy plugin for a given dokku application"},
		{&StorageEnsureTask{}, "Ensures the storage for a given dokku application"},
		{&StorageMountTask{}, "Mounts or unmounts the storage for a given dokku application"},
		{&TraefikPropertyTask{}, "Manages the traefik configuration for a given dokku application"},
	}

	for _, tt := range tests {
		doc := tt.task.Doc()
		if doc != tt.want {
			t.Errorf("Doc() = %q, want %q", doc, tt.want)
		}
	}
}

func TestGetTasksGroupBlockOnly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: deploy
      block:
        - name: ensure app
          dokku_app:
            app: my-app
        - name: also ensure
          dokku_app:
            app: my-other-app
`)
	got, err := GetTasks(data, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetTasks should not error for block-only group, got: %v", err)
	}
	keys := got.Keys()
	if len(keys) != 1 {
		t.Fatalf("expected 1 envelope (the group), got %d: %v", len(keys), keys)
	}
	env := got.GetEnvelope("deploy")
	if env == nil {
		t.Fatal("expected envelope named 'deploy'")
	}
	if !env.IsGroup() {
		t.Fatal("expected envelope to be a group")
	}
	if len(env.Block) != 2 {
		t.Errorf("expected 2 block children, got %d", len(env.Block))
	}
	if len(env.Rescue) != 0 {
		t.Errorf("expected 0 rescue children, got %d", len(env.Rescue))
	}
	if len(env.Always) != 0 {
		t.Errorf("expected 0 always children, got %d", len(env.Always))
	}
	if env.Task != nil {
		t.Errorf("group envelope must have nil Task, got %T", env.Task)
	}
	if env.TypeName != "" {
		t.Errorf("group envelope TypeName must be empty, got %q", env.TypeName)
	}
}

func TestGetTasksGroupAllClauses(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: deploy with rollback
      block:
        - dokku_app:
            app: candidate
      rescue:
        - dokku_app:
            app: candidate
            state: absent
      always:
        - dokku_config:
            app: existing
            config:
              LAST_ATTEMPT: now
`)
	got, err := GetTasks(data, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetTasks: %v", err)
	}
	env := got.GetEnvelope("deploy with rollback")
	if env == nil {
		t.Fatal("expected envelope named 'deploy with rollback'")
	}
	if len(env.Block) != 1 || len(env.Rescue) != 1 || len(env.Always) != 1 {
		t.Errorf("expected 1/1/1 block/rescue/always, got %d/%d/%d", len(env.Block), len(env.Rescue), len(env.Always))
	}
}

func TestGetTasksRejectsEmptyBlock(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: empty
      block: []
`)
	_, err := GetTasks(data, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for empty block, got nil")
	}
	if !strings.Contains(err.Error(), "block: must contain at least one child task") {
		t.Errorf("expected 'block: must contain at least one child task' error, got: %v", err)
	}
}

func TestGetTasksRejectsRescueWithoutBlock(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: orphan
      rescue:
        - dokku_app:
            app: x
`)
	_, err := GetTasks(data, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for rescue without block, got nil")
	}
	if !strings.Contains(err.Error(), "rescue: without block:") {
		t.Errorf("expected 'rescue: without block:' error, got: %v", err)
	}
}

func TestGetTasksRejectsBlockAlongsideTaskType(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: hybrid
      block:
        - dokku_app:
            app: x
      dokku_app:
        app: y
`)
	_, err := GetTasks(data, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for block alongside task-type, got nil")
	}
	if !strings.Contains(err.Error(), "is a block: group and cannot also carry task-type key") {
		t.Errorf("expected block+task-type error, got: %v", err)
	}
}

func TestGetTasksNestedGroup(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: outer
      block:
        - name: inner
          block:
            - dokku_app:
                app: deepest
          rescue:
            - dokku_app:
                app: rescued
`)
	got, err := GetTasks(data, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetTasks for nested group: %v", err)
	}
	outer := got.GetEnvelope("outer")
	if outer == nil || !outer.IsGroup() {
		t.Fatal("expected outer group envelope")
	}
	if len(outer.Block) != 1 {
		t.Fatalf("outer.Block: want 1 child, got %d", len(outer.Block))
	}
	inner := outer.Block[0]
	if !inner.IsGroup() {
		t.Fatal("expected inner envelope to be a group")
	}
	if len(inner.Block) != 1 {
		t.Errorf("inner.Block: want 1, got %d", len(inner.Block))
	}
	if len(inner.Rescue) != 1 {
		t.Errorf("inner.Rescue: want 1, got %d", len(inner.Rescue))
	}
}
