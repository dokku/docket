package commands

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// TestTaskCatalogSchemaShapesNotes is the self-check on the rendered notes
// (#651): the schema accepts what docket publishes, and refuses an unknown
// note kind, an unknown key on a task note or a field note, and a per-state
// rendering that is not a string.
func TestTaskCatalogSchemaShapesNotes(t *testing.T) {
	t.Parallel()
	validate := func(mutate func(task map[string]interface{})) error {
		t.Helper()
		out, stderr, exit := runSchema(t, "--task", "dokku_domains")
		if exit != 0 {
			t.Fatalf("exit = %d, want 0 (stderr: %s)", exit, stderr)
		}
		doc := decodeCatalog(t, out)
		mutate(catalogTasks(t, doc)["dokku_domains"])

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
	firstNote := func(task map[string]interface{}) map[string]interface{} {
		notes, _ := task["notes"].([]interface{})
		if len(notes) == 0 {
			t.Fatal("dokku_domains publishes no notes")
		}
		note, _ := notes[0].(map[string]interface{})
		return note
	}
	domainsNote := func(task map[string]interface{}) map[string]interface{} {
		fields, _ := task["fields"].([]interface{})
		for _, entry := range fields {
			field, _ := entry.(map[string]interface{})
			if field["name"] == "domains" {
				note, ok := field["note"].(map[string]interface{})
				if !ok {
					t.Fatal("dokku_domains.domains publishes no note")
				}
				return note
			}
		}
		t.Fatal("dokku_domains has no domains field")
		return nil
	}

	if err := validate(func(task map[string]interface{}) {
		if _, ok := domainsNote(task)["states"].(map[string]interface{}); !ok {
			t.Fatal("dokku_domains.domains note is not keyed by state")
		}
	}); err != nil {
		t.Errorf("the published notes must validate: %v", err)
	}

	refused := map[string]func(task map[string]interface{}){
		"an unknown note kind":         func(task map[string]interface{}) { firstNote(task)["kind"] = "not_a_kind" },
		"an unknown task note key":     func(task map[string]interface{}) { firstNote(task)["extra"] = "x" },
		"a task note without markdown": func(task map[string]interface{}) { delete(firstNote(task), "markdown") },
		"an unknown field note key":    func(task map[string]interface{}) { domainsNote(task)["extra"] = "x" },
		"a non-string state rendering": func(task map[string]interface{}) {
			domainsNote(task)["states"] = map[string]interface{}{"present": true}
		},
	}
	for name, mutate := range refused {
		if err := validate(mutate); err == nil {
			t.Errorf("expected %s to fail validation", name)
		}
	}
}
