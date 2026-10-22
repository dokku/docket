package tasks

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// walkCatalogFields walks every task's fields, element fields included, and
// calls visit with each field's `type.path` name and its struct field.
func walkCatalogFields(t *testing.T, visit func(at string, structField reflect.StructField, field FieldSchema)) {
	t.Helper()
	var walk func(where string, rt reflect.Type, fields []FieldSchema)
	walk = func(where string, rt reflect.Type, fields []FieldSchema) {
		for _, field := range fields {
			at := where + "." + field.Name
			structField, ok := structFieldByYAMLName(rt, field.Name)
			if !ok {
				t.Errorf("%s has no struct field", at)
				continue
			}
			visit(at, structField, field)
			if field.Item != nil && field.Item.Type == TypeObject {
				walk(at+"[]", structElem(sliceOrMapElem(structField.Type)), field.Item.Fields)
			}
		}
	}
	for _, schema := range wholeCatalog(t).Tasks {
		walk(schema.Type, taskStructType(lookupTask(schema.Type)), schema.Fields)
	}
}

// TestCatalogUniqueMatchesTags holds the `unique:` tag to the catalog: it is
// absent or exactly "true", only on a list that is not an identity
// collection, and the catalog publishes unique on exactly the tagged fields
// (#648).
func TestCatalogUniqueMatchesTags(t *testing.T) {
	var unique []string
	walkCatalogFields(t, func(at string, structField reflect.StructField, field FieldSchema) {
		tag, tagged := structField.Tag.Lookup(uniqueTag)
		if tagged && tag != "true" {
			t.Errorf("%s is tagged unique:%q; the tag is unique:\"true\" or absent", at, tag)
		}
		if tagged && field.Type != TypeList {
			t.Errorf("%s is a %s and must not carry a unique tag", at, field.Type)
		}
		if tagged && field.Identity == IdentityRoleCollection {
			t.Errorf("%s is an identity collection, whose item identity already makes members distinct, and must not carry a unique tag", at)
		}
		if field.Unique != (tagged && tag == "true" && field.Type == TypeList) {
			t.Errorf("%s publishes unique = %v; tag says %q", at, field.Unique, tag)
		}
		if field.Unique {
			unique = append(unique, at)
		}
	})

	// The lists #648 declared. Adding one is a deliberate edit here.
	want := []string{
		"dokku_config.preserve",
		"dokku_scheduler_k3s_profile.kubelet_args",
		"dokku_storage_mount.mounts[].phases",
		"dokku_storage_mount.phases",
	}
	slices.Sort(unique)
	if !slices.Equal(unique, want) {
		t.Errorf("unique lists = %q; want %q", unique, want)
	}
}

// TestCatalogUnorderedListsAreSets holds every list whose order docket
// ignores to set semantics: it is an identity collection, whose item identity
// makes its members distinct, or it is unique, so a consumer can model every
// unordered list as a set (#648).
func TestCatalogUnorderedListsAreSets(t *testing.T) {
	unordered := 0
	walkCatalogFields(t, func(at string, _ reflect.StructField, field FieldSchema) {
		if field.Type != TypeList || field.Ordered == nil || *field.Ordered {
			return
		}
		unordered++
		collection := field.Identity == IdentityRoleCollection
		if collection == field.Unique {
			t.Errorf("%s is unordered and must be exactly one of an identity collection or unique:\"true\" (collection %v, unique %v)", at, collection, field.Unique)
		}
	})
	if unordered == 0 {
		t.Fatal("no unordered lists found")
	}
}

// TestCatalogUniqueNote checks the "(no duplicates)" field note on each
// unique list, element fields included, and its absence elsewhere.
func TestCatalogUniqueNote(t *testing.T) {
	walkCatalogFields(t, func(at string, _ reflect.StructField, field FieldSchema) {
		has := field.Note != nil && strings.Contains(field.Note.Suffix, "(no duplicates)")
		if has != field.Unique {
			t.Errorf("%s note %v; unique %v", at, field.Note, field.Unique)
		}
	})

	catalog := wholeCatalog(t)
	kubeletArgs := fieldFor(t, schemaFor(t, catalog, "dokku_scheduler_k3s_profile").Fields, "kubelet_args")
	if kubeletArgs.Note == nil || kubeletArgs.Note.Suffix != " (no duplicates)" {
		t.Errorf("kubelet_args note = %+v; want suffix %q", kubeletArgs.Note, " (no duplicates)")
	}
}
