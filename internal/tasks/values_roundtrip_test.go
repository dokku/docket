package tasks

import (
	"reflect"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

// TestTaskValuesRoundTripCatalogExamples pins, over every task's catalog
// examples, that TaskFromValues builds the task DecodeTask builds from the
// same body, and that TaskValues reads back the body with every field the
// example leaves out at its default (#644).
func TestTaskValuesRoundTripCatalogExamples(t *testing.T) {
	t.Parallel()
	catalog, err := Catalog()
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	checked := 0
	for _, schema := range catalog.Tasks {
		for _, example := range schema.Examples {
			name := schema.Type + "/" + example.Name
			var doc map[string]any
			if err := yaml.Unmarshal([]byte(example.YAML), &doc); err != nil {
				t.Fatalf("%s: example is not YAML: %v", name, err)
			}
			body, ok := doc[schema.Type].(map[string]any)
			if !ok {
				if doc[schema.Type] != nil {
					t.Fatalf("%s: example body is %T, want a mapping", name, doc[schema.Type])
				}
				body = map[string]any{}
			}

			built, err := TaskFromValues(schema.Type, body)
			if err != nil {
				t.Errorf("%s: TaskFromValues: %v", name, err)
				continue
			}
			raw, err := yaml.Marshal(body)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			decoded, err := DecodeTask(schema.Type, raw)
			if err != nil {
				t.Fatalf("%s: DecodeTask: %v", name, err)
			}
			if !reflect.DeepEqual(built, decoded) {
				t.Errorf("%s: TaskFromValues = %+v, DecodeTask = %+v", name, built, decoded)
			}

			want := withCatalogDefaults(schema.Fields, body)
			if got := TaskValues(built); !reflect.DeepEqual(got, want) {
				t.Errorf("%s: TaskValues = %#v\nwant %#v", name, got, want)
			}
			checked++
		}
	}
	if checked == 0 {
		t.Fatal("no catalog examples were checked")
	}
}

// withCatalogDefaults is what TaskValues should read for a task built from
// given: each given value with YAML's int widened to int64, and every field
// given leaves out at its catalog default or zero, list elements included.
// It reads the published catalog, not the struct, so it checks TaskValues
// against the documented contract rather than against its own reflection.
func withCatalogDefaults(fields []FieldSchema, given map[string]any) map[string]any {
	out := make(map[string]any, len(fields))
	for _, field := range fields {
		value, ok := given[field.Name]
		// A string given as "" takes its default too, as the loader's
		// defaults.SetDefaults gives it one: an example that leaves `state`
		// unset still spells it `state: ""`. Every other default sits on a
		// *bool, where an explicit false is kept.
		if !ok || value == nil || (field.Default != nil && value == "") {
			out[field.Name] = defaultOrZero(field)
			continue
		}
		out[field.Name] = expectedValue(field, value)
	}
	return out
}

// expectedValue widens one given value the way TaskValues types it.
func expectedValue(field FieldSchema, value any) any {
	if field.Type == TypeList && field.Item != nil && field.Item.Type == TypeObject {
		items, _ := value.([]any)
		out := make([]any, len(items))
		for i, item := range items {
			element, _ := item.(map[string]any)
			out[i] = withCatalogDefaults(field.Item.Fields, element)
		}
		return out
	}
	return widenInts(value)
}

// widenInts converts every int a YAML decode produced to int64.
func widenInts(value any) any {
	switch v := value.(type) {
	case int:
		return int64(v)
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = widenInts(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = widenInts(item)
		}
		return out
	}
	return value
}
