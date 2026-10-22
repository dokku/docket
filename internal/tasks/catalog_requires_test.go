package tasks

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// catalogExamples returns every registered task's published examples, keyed
// by task type.
func catalogExamples(t *testing.T) map[string][]ExampleSchema {
	t.Helper()
	catalog, err := Catalog()
	if err != nil {
		t.Fatalf("Catalog() error: %v", err)
	}
	out := make(map[string][]ExampleSchema, len(catalog.Tasks))
	for _, schema := range catalog.Tasks {
		out[schema.Type] = schema.Examples
	}
	return out
}

// decodeRequirement decodes one published requirement, failing the test when
// its snippet does not hold exactly the task its type names.
func decodeRequirement(t *testing.T, required ExampleRequirementSchema) Task {
	t.Helper()
	task, err := decodeExampleCodeblock(required.YAML)
	if err != nil {
		t.Fatalf("required %s does not decode: %v\n%s", required.Type, err, required.YAML)
	}
	if got := typeKeyOf(task); got != required.Type {
		t.Fatalf("required task is a %s, but its type says %s", got, required.Type)
	}
	return task
}

// exampleRecipe is the complete recipe an example and its requirements make:
// the required tasks in order, then the example, as one task list.
func exampleRecipe(example ExampleSchema) []byte {
	var b strings.Builder
	b.WriteString("---\n- tasks:\n")
	snippets := make([]string, 0, len(example.Requires)+1)
	for _, required := range example.Requires {
		snippets = append(snippets, required.YAML)
	}
	snippets = append(snippets, example.YAML)
	for _, snippet := range snippets {
		for i, line := range strings.Split(strings.TrimRight(snippet, "\n"), "\n") {
			if i == 0 {
				b.WriteString("    - ")
			} else {
				b.WriteString("      ")
			}
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return []byte(b.String())
}

// unmetRequirements returns what task needs that none of before provides: an
// object it names but does not create with no earlier task creating it, and
// a plugin it requires with no earlier dokku_plugin installing it.
func unmetRequirements(task Task, before []Task) []string {
	var missing []string
	for _, object := range TaskObjects(task) {
		if !object.Set || object.Creates {
			continue
		}
		created := false
		for _, earlier := range before {
			for _, other := range TaskObjects(earlier) {
				if other.Creates && other.Kind == object.Kind && reflect.DeepEqual(other.Values, object.Values) {
					created = true
				}
			}
		}
		if !created {
			missing = append(missing, string(object.Kind)+" "+strings.Join(object.Values, "/"))
		}
	}
	if support, ok := TaskMinimumVersions(task); ok {
		for _, plugin := range support.Plugins {
			if !pluginApplies(task, plugin) {
				continue
			}
			name, ok := pluginListName(task, plugin)
			if !ok {
				continue
			}
			installed := false
			for _, earlier := range before {
				if p, ok := taskAsPointer(earlier).(*PluginTask); ok && p.Name == name {
					installed = true
				}
			}
			if !installed {
				missing = append(missing, "plugin "+name)
			}
		}
	}
	return missing
}

// TestExampleRequiresCoverObjects holds every published example to its
// requirement list (#652): each app and service the example names through
// objects[].keys, and each plugin it requires, is provided by a required task,
// and each required task's own needs are provided by one listed before it.
func TestExampleRequiresCoverObjects(t *testing.T) {
	for typeKey, examples := range catalogExamples(t) {
		for _, example := range examples {
			task, err := decodeExampleCodeblock(example.YAML)
			if err != nil {
				t.Fatalf("%s example %q does not decode: %v", typeKey, example.Name, err)
			}
			var required []Task
			for _, entry := range example.Requires {
				next := decodeRequirement(t, entry)
				for _, missing := range unmetRequirements(next, required) {
					t.Errorf("%s example %q: required %s needs %s, which no earlier requirement provides", typeKey, example.Name, entry.Type, missing)
				}
				required = append(required, next)
			}
			for _, missing := range unmetRequirements(task, required) {
				t.Errorf("%s example %q needs %s, which its requires list does not provide", typeKey, example.Name, missing)
			}
		}
	}
}

// TestExampleRequiresAreValidTasks loads each example with its requirements
// as one recipe through the loader, so a required task missing a required
// field fails here, and checks every required task's constraints and
// Validate() the way `docket validate` does.
func TestExampleRequiresAreValidTasks(t *testing.T) {
	for typeKey, examples := range catalogExamples(t) {
		for _, example := range examples {
			if len(example.Requires) == 0 {
				continue
			}
			out, err := GetTasks(exampleRecipe(example), map[string]interface{}{})
			if err != nil {
				t.Errorf("%s example %q with its requirements does not load: %v", typeKey, example.Name, err)
				continue
			}
			if got, want := len(out.Keys()), len(example.Requires)+1; got != want {
				t.Errorf("%s example %q loads as %d tasks, want %d", typeKey, example.Name, got, want)
			}
			for _, entry := range example.Requires {
				task := decodeRequirement(t, entry)
				if violations := ConstraintViolations(task); len(violations) > 0 {
					t.Errorf("%s example %q: required %s breaks a constraint: %v", typeKey, example.Name, entry.Type, violations[0])
				}
				if validator, ok := task.(InputValidator); ok {
					if err := validator.Validate(); err != nil {
						t.Errorf("%s example %q: required %s fails Validate(): %v", typeKey, example.Name, entry.Type, err)
					}
				}
			}
		}
	}
}

// TestExampleRequiresListEachTaskOnce checks the requirement list names no
// task twice, so a requirement two others share is applied once.
func TestExampleRequiresListEachTaskOnce(t *testing.T) {
	for typeKey, examples := range catalogExamples(t) {
		for _, example := range examples {
			seen := map[string]bool{}
			for _, entry := range example.Requires {
				address := IdentityAddress(entry.Type, decodeRequirement(t, entry))
				if seen[address] {
					t.Errorf("%s example %q requires %s twice", typeKey, example.Name, address)
				}
				seen[address] = true
			}
		}
	}
}

// TestTaskRequiresDerivation pins the derivation on the shapes it handles: a
// plugin from a fixed name, from a name field and behind a when field, an
// object's creator with its keys mapped by role, a task that creates its own
// object, and a requirement's own requirements coming first.
func TestTaskRequiresDerivation(t *testing.T) {
	cases := []struct {
		name string
		task Task
		want []string
	}{
		{"an app task needs its app", ConfigTask{App: "api"}, []string{
			"dokku_app:\n    app: api\n",
		}},
		{"a global task needs no app", DomainsTask{Global: true}, nil},
		{"a creating task needs nothing", AppTask{App: "api"}, nil},
		{"a clone creates its own app", AppCloneTask{App: "api-staging", SourceApp: "api"}, nil},
		{"a when field off needs no plugin", CertsTask{App: "api"}, []string{
			"dokku_app:\n    app: api\n",
		}},
		{"a when field on needs its plugin", CertsTask{Global: true}, []string{
			"dokku_plugin:\n    name: global-cert\n    url: https://github.com/dokku-community/dokku-global-cert.git\n",
		}},
		{"a service task needs its plugin before its service", ServiceLinkTask{App: "api", Service: "redis", Name: "cache"}, []string{
			"dokku_plugin:\n    name: redis\n    url: https://github.com/dokku/dokku-redis.git\n",
			"dokku_app:\n    app: api\n",
			"dokku_service_create:\n    service: redis\n    name: cache\n",
		}},
		{"a service's keys map by role", AclServiceTask{Service: "cache", Type: "redis", Users: []string{"alice"}}, []string{
			"dokku_plugin:\n    name: acl\n    url: https://github.com/dokku-community/dokku-acl.git\n",
			"dokku_plugin:\n    name: redis\n    url: https://github.com/dokku/dokku-redis.git\n",
			"dokku_service_create:\n    service: redis\n    name: cache\n",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, required := range TaskRequires(tc.task) {
				schema, err := buildRequirementSchema(required)
				if err != nil {
					t.Fatalf("render: %v", err)
				}
				got = append(got, schema.YAML)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("TaskRequires(%T) =\n%q\nwant\n%q", tc.task, got, tc.want)
			}
		})
	}
}

// TestExampleRequiresDeclaredAfterDerived checks a declared requirement comes
// after what the example's own task derives and after its own requirements,
// and that a requirement both declare is listed once.
func TestExampleRequiresDeclaredAfterDerived(t *testing.T) {
	declared := []Task{deployedExampleApp("api")}
	got := exampleRequires(ServiceLinkTask{App: "api", Service: "redis", Name: "cache"}, declared)
	var types []string
	for _, task := range got {
		types = append(types, typeKeyOf(task))
	}
	want := []string{"dokku_plugin", "dokku_app", "dokku_service_create", "dokku_git_from_image"}
	if !reflect.DeepEqual(types, want) {
		t.Errorf("requires = %v, want %v", types, want)
	}
}

// TestObjectCreatorsCreateTheirKind holds objectCreators to each task's own
// ObjectEffects, and to creating the object from its keys alone.
func TestObjectCreatorsCreateTheirKind(t *testing.T) {
	for _, kind := range objectKinds {
		typeKey, ok := objectCreators[kind]
		if !ok {
			t.Errorf("no creator for object kind %s", kind)
			continue
		}
		task, ok := Lookup(typeKey)
		if !ok {
			t.Errorf("creator %s of %s is not registered", typeKey, kind)
			continue
		}
		creates := false
		for _, object := range TaskObjects(task) {
			if object.Kind == kind && object.Creates {
				creates = true
			}
		}
		if !creates {
			t.Errorf("%s does not create a %s", typeKey, kind)
		}
	}
}

// TestPluginRepositoriesCoverRequirements fails when a task requires a
// fixed-name plugin a dokku_plugin requirement has no repository for.
func TestPluginRepositoriesCoverRequirements(t *testing.T) {
	for typeKey, task := range allRegisteredTasks() {
		support, ok := TaskMinimumVersions(task)
		if !ok {
			continue
		}
		for _, plugin := range support.Plugins {
			if plugin.NameField != "" {
				continue
			}
			if _, ok := pluginRepositories[plugin.Name]; !ok {
				t.Errorf("%s requires plugin %q, which pluginRepositories does not name", typeKey, plugin.Name)
			}
		}
	}
}

// setupDokkuPins reads the plugin pins the setup-dokku action installs, as
// name to repository.
func setupDokkuPins(t *testing.T) map[string]string {
	t.Helper()
	raw, err := os.ReadFile("../../.github/actions/setup-dokku/setup-dokku.sh")
	if err != nil {
		t.Fatalf("read setup-dokku.sh: %v", err)
	}
	body := string(raw)
	start := strings.Index(body, "PLUGIN_PINS=(")
	if start < 0 {
		t.Fatal("setup-dokku.sh declares no PLUGIN_PINS")
	}
	end := strings.Index(body[start:], "\n)")
	if end < 0 {
		t.Fatal("setup-dokku.sh's PLUGIN_PINS is not closed")
	}
	pins := map[string]string{}
	for _, line := range strings.Split(body[start:start+end], "\n")[1:] {
		fields := strings.Fields(strings.Trim(strings.TrimSpace(line), `"`))
		if len(fields) == 3 {
			pins[fields[0]] = fields[1]
		}
	}
	return pins
}

// TestPluginRepositoriesMatchCIPins holds the repositories a dokku_plugin
// requirement names to the ones CI installs, and checks CI pins every plugin
// a published example requires, so the example integration tests can run
// each one.
func TestPluginRepositoriesMatchCIPins(t *testing.T) {
	pins := setupDokkuPins(t)
	for name, url := range pins {
		want, ok := pluginRepositories[name]
		if !ok {
			want = datastorePluginRepository(name)
		}
		if url != want {
			t.Errorf("setup-dokku.sh pins %s at %s, but a requirement installs it from %s", name, url, want)
		}
	}
	for typeKey, examples := range catalogExamples(t) {
		for _, example := range examples {
			for _, entry := range example.Requires {
				plugin, ok := taskAsPointer(decodeRequirement(t, entry)).(*PluginTask)
				if !ok {
					continue
				}
				if _, ok := pins[plugin.Name]; !ok {
					t.Errorf("%s example %q requires plugin %s, which setup-dokku.sh does not pin", typeKey, example.Name, plugin.Name)
				}
			}
		}
	}
}
