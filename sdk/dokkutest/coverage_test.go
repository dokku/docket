package dokkutest_test

import (
	"context"
	"sort"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/dokku/docket/sdk"
	"github.com/dokku/docket/sdk/dokkutest"
)

// coreTypes are the task types the Server models besides the property tasks,
// which coveredTypes adds from the catalog.
var coreTypes = []string{
	"dokku_app",
	"dokku_buildpacks",
	"dokku_config",
	"dokku_domains",
	"dokku_ports",
	"dokku_ps_scale",
	"dokku_service_create",
	"dokku_service_link",
}

// coveredTypes returns every task type the Server models: the core types and
// every task managing a plugin's property table, sorted.
func coveredTypes(t *testing.T) []string {
	t.Helper()
	catalog, err := sdk.Catalog()
	if err != nil {
		t.Fatal(err)
	}
	types := append([]string(nil), coreTypes...)
	for _, task := range catalog.Tasks {
		if task.PropertySchema != nil {
			types = append(types, task.Type)
		}
	}
	sort.Strings(types)
	return types
}

// TestCatalogExamplesStayInStep keeps the Server in step with the probes:
// every documented example of every covered task is planned, applied, read
// back from the task and from its address, and exported, against a Server
// seeded with the app and service the task depends on. A command the Server
// does not model fails the test, so a probe that changes the command it runs
// or a task newly covered here shows up as the command to model.
func TestCatalogExamplesStayInStep(t *testing.T) {
	t.Parallel()
	catalog, err := sdk.CatalogFor(coveredTypes(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, schema := range catalog.Tasks {
		if len(schema.Examples) == 0 {
			t.Errorf("%s has no catalog example to check the fake against", schema.Type)
			continue
		}
		for _, example := range schema.Examples {
			t.Run(schema.Type+"/"+example.Name, func(t *testing.T) {
				t.Parallel()
				task := decodeExample(t, schema.Type, example.YAML)
				server := dokkutest.New(t)
				seedObjects(server, task)
				ctx := sdk.ContextWithVersionGate(server.Context(context.Background()))

				plan := task.Plan(ctx)
				if plan.Error != nil {
					t.Fatalf("plan: %v", plan.Error)
				}
				if state := sdk.ExecutePlan(ctx, plan); state.Error != nil {
					t.Fatalf("apply: %v (%s)", state.Error, state.Stderr)
				}
				if _, _, err := sdk.ReadTask(ctx, task); err != nil {
					t.Errorf("ReadTask: %v", err)
				}
				address := sdk.IdentityAddress(schema.Type, task)
				if _, _, err := sdk.ReadAddress(ctx, address); err != nil {
					t.Errorf("ReadAddress(%s): %v", address, err)
				}
				selectors, err := sdk.ParseResourceSelectors([]string{address})
				if err != nil {
					t.Fatal(err)
				}
				if _, err := sdk.ExportRecipe(ctx, sdk.ExportOptions{Resources: selectors, Inline: true}); err != nil {
					t.Errorf("export %s: %v", address, err)
				}
				if unknown := server.Unknown(); len(unknown) != 0 {
					t.Errorf("the fake does not model: %q", unknown)
				}
			})
		}
	}
}

// decodeExample builds the task a catalog example declares. An example is a
// one-key map from the task type to its body.
func decodeExample(t *testing.T, typeKey, doc string) sdk.Task {
	t.Helper()
	var example map[string]yaml.Node
	if err := yaml.Unmarshal([]byte(doc), &example); err != nil {
		t.Fatalf("parse example: %v\n%s", err, doc)
	}
	node, ok := example[typeKey]
	if !ok || len(example) != 1 {
		t.Fatalf("example is not a single %s task:\n%s", typeKey, doc)
	}
	body, err := yaml.Marshal(&node)
	if err != nil {
		t.Fatal(err)
	}
	task, err := sdk.DecodeTask(typeKey, body)
	if err != nil {
		t.Fatalf("decode example: %v\n%s", err, doc)
	}
	return task
}

// seedObjects creates every app and service task depends on but does not
// create itself, and installs the datastore of a service it does create.
func seedObjects(server *dokkutest.Server, task sdk.Task) {
	for _, object := range sdk.TaskObjects(task) {
		if !object.Set {
			continue
		}
		switch {
		case object.Kind == sdk.ObjectService && object.Creates:
			server.SeedDatastore(object.Values[0], object.Values[0]+":latest")
		case object.Creates:
		case object.Kind == sdk.ObjectApp:
			server.SeedApp(object.Values[0])
		case object.Kind == sdk.ObjectService:
			server.SeedService(object.Values[0], object.Values[1], nil)
		}
	}
}

// TestCoveredTypesHaveALifecycle keeps TestLifecycle covering every core type.
func TestCoveredTypesHaveALifecycle(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, c := range lifecycleCases() {
		seen[c.typeKey] = true
	}
	for _, typeKey := range coreTypes {
		if !seen[typeKey] {
			t.Errorf("%s has no lifecycle case", typeKey)
		}
	}
}
