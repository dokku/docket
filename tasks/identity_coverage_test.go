package tasks

import (
	"reflect"
	"testing"
)

// taskStructType returns the struct type behind a registered task prototype.
func taskStructType(task Task) reflect.Type {
	rt := reflect.TypeOf(task)
	for rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}
	return rt
}

// TestEveryTaskDeclaresIdentity asserts that every registered task says which
// resource it manages. This is what makes "docket knows what a task addresses"
// enforceable: a new task without an `identity:"key"` field fails the build
// here rather than silently shipping with no address, which would leave it
// unnamed in the event stream and unreachable from `export --resource`.
func TestEveryTaskDeclaresIdentity(t *testing.T) {
	for name, task := range RegisteredTasks {
		if len(TaskIdentity(task)) == 0 {
			t.Errorf("task %q declares no identity:\"key\" field (tag the fields that select the resource it manages)", name)
		}
	}
}

// TestIdentityKeysAreNeverSensitive is the guard on the one stream that is
// deliberately unmasked. `--list-tasks --json` prints resolved values by
// design (docs/json-output.md), and since #427 a task's name embeds its
// identity fields - so a field that is both an identity key and a declared
// secret would put that secret into the listing. Every task satisfies this
// today: the payload fields that are sensitive (an image reference, an
// archive URL, a password, a certificate) are never the address.
func TestIdentityKeysAreNeverSensitive(t *testing.T) {
	for name, task := range RegisteredTasks {
		rt := taskStructType(task)
		for i := 0; i < rt.NumField(); i++ {
			field := rt.Field(i)
			if field.Tag.Get(identityTag) != identityTagKey {
				continue
			}
			if field.Tag.Get("sensitive") == "true" {
				t.Errorf("task %q field %s is both identity:%q and sensitive:\"true\"; an identity key is rendered into the task name, which --list-tasks does not mask",
					name, field.Name, identityTagKey)
			}
		}
	}
}

// TestIdentityTagsAreWellFormed checks the shape of every identity tag: the
// value is one of the two the reader understands, a key is a scalar, a
// collection is a slice or a map, and the desired state is never either.
//
// `state:` is excluded on purpose. The address answers "which resource", not
// "what should happen to it": including the state would rename a task when its
// state changed, and would give `dokku_domains` four addresses for one app's
// domain list.
func TestIdentityTagsAreWellFormed(t *testing.T) {
	for name, task := range RegisteredTasks {
		rt := taskStructType(task)
		for i := 0; i < rt.NumField(); i++ {
			field := rt.Field(i)
			tag, ok := field.Tag.Lookup(identityTag)
			if !ok {
				continue
			}

			if field.Name == "State" {
				t.Errorf("task %q tags State as identity:%q; the desired state is not part of the resource address", name, tag)
				continue
			}

			ft := field.Type
			for ft.Kind() == reflect.Ptr {
				ft = ft.Elem()
			}

			switch tag {
			case identityTagKey:
				switch ft.Kind() {
				case reflect.String, reflect.Bool,
					reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
					reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
				default:
					t.Errorf("task %q field %s is identity:%q but has kind %s; a key must render as a scalar", name, field.Name, tag, ft.Kind())
				}
			case identityTagCollection:
				if ft.Kind() != reflect.Slice && ft.Kind() != reflect.Array && ft.Kind() != reflect.Map {
					t.Errorf("task %q field %s is identity:%q but has kind %s; a collection must be a slice or a map", name, field.Name, tag, ft.Kind())
				}
			default:
				t.Errorf("task %q field %s has identity:%q; expected %q or %q", name, field.Name, tag, identityTagKey, identityTagCollection)
			}
		}
	}
}

// TestCollectionItemsDeclareTheirOwnIdentity asserts that a collection of
// structs says what identifies one entry. Both cases in the tree state it in a
// comment today - PortMapping's "no two may share a scheme and host port",
// HttpAuthUser's username - and a comment is exactly what #427 set out to
// replace with something the code can read.
func TestCollectionItemsDeclareTheirOwnIdentity(t *testing.T) {
	for name, task := range RegisteredTasks {
		for _, collection := range TaskIdentityCollections(task) {
			if collection.Item != ItemIdentityFields {
				continue
			}
			if len(collection.ItemKeys) == 0 {
				t.Errorf("task %q collection %s holds structs but its element type declares no identity:%q field",
					name, collection.YAMLName, identityTagKey)
			}
		}
	}
}

// TestSetValuedTasksDeclareTheirCollection pins the decision #427 asked for on
// the three tasks whose resource is an unordered set. Each keys on its scope
// alone - the collection is the resource, not part of its key - and each says
// so by tagging the collection rather than leaving the reader to infer it from
// the absence of a tag.
func TestSetValuedTasksDeclareTheirCollection(t *testing.T) {
	want := map[string]string{
		"dokku_domains": "domains",
		"dokku_ports":   "port_mappings",
		"dokku_acl_app": "users",
	}
	for name, field := range want {
		task, ok := RegisteredTasks[name]
		if !ok {
			t.Errorf("task %q is not registered", name)
			continue
		}
		collections := TaskIdentityCollections(task)
		if len(collections) != 1 {
			t.Errorf("task %q declares %d collections, want exactly 1", name, len(collections))
			continue
		}
		if collections[0].YAMLName != field {
			t.Errorf("task %q declares collection %q, want %q", name, collections[0].YAMLName, field)
		}
	}
}

// TestIdentityKeysAreDocumentedParameters asserts that every identity key is a
// field a recipe can actually set. A key hidden behind `yaml:"-"` would render
// into task names and export addresses while appearing nowhere in the task's
// Parameters table, so a user could neither reproduce nor target it.
func TestIdentityKeysAreDocumentedParameters(t *testing.T) {
	for name, task := range RegisteredTasks {
		rt := taskStructType(task)
		for i := 0; i < rt.NumField(); i++ {
			field := rt.Field(i)
			if field.Tag.Get(identityTag) != identityTagKey {
				continue
			}
			if field.Tag.Get("yaml") == "-" {
				t.Errorf("task %q field %s is an identity key but is excluded from yaml", name, field.Name)
			}
		}
	}
}
