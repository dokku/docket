package commands

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// TestTaskCatalogSchemaShapesExampleRequires is the self-check on each
// example's requirement list (#652): the schema accepts what docket
// publishes, and refuses an unknown key on a requirement, a requirement
// without its type or snippet, and an empty list, which docket leaves out.
func TestTaskCatalogSchemaShapesExampleRequires(t *testing.T) {
	t.Parallel()
	validate := func(mutate func(example map[string]interface{})) error {
		t.Helper()
		out, stderr, exit := runSchema(t, "--task", "dokku_service_link")
		if exit != 0 {
			t.Fatalf("exit = %d, want 0 (stderr: %s)", exit, stderr)
		}
		doc := decodeCatalog(t, out)
		examples, _ := catalogTasks(t, doc)["dokku_service_link"]["examples"].([]interface{})
		if len(examples) == 0 {
			t.Fatal("dokku_service_link publishes no examples")
		}
		example, _ := examples[0].(map[string]interface{})
		mutate(example)

		raw, err := json.Marshal(doc)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		inst, err := jsonschema.UnmarshalJSON(strings.NewReader(string(raw)))
		if err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		return loadSchema(t, taskCatalogSchemaPath).Validate(inst)
	}
	firstRequirement := func(example map[string]interface{}) map[string]interface{} {
		requires, _ := example["requires"].([]interface{})
		if len(requires) == 0 {
			t.Fatal("the dokku_service_link example publishes no requires")
		}
		required, _ := requires[0].(map[string]interface{})
		return required
	}

	if err := validate(func(example map[string]interface{}) { firstRequirement(example) }); err != nil {
		t.Errorf("the published requires must validate: %v", err)
	}

	refused := map[string]func(example map[string]interface{}){
		"an unknown requirement key":     func(example map[string]interface{}) { firstRequirement(example)["extra"] = "x" },
		"a requirement without a type":   func(example map[string]interface{}) { delete(firstRequirement(example), "type") },
		"a requirement without a yaml":   func(example map[string]interface{}) { delete(firstRequirement(example), "yaml") },
		"an empty requires list":         func(example map[string]interface{}) { example["requires"] = []interface{}{} },
		"a requirement that is a string": func(example map[string]interface{}) { example["requires"] = []interface{}{"dokku_app"} },
	}
	for name, mutate := range refused {
		if err := validate(mutate); err == nil {
			t.Errorf("expected %s to fail validation", name)
		}
	}
}
