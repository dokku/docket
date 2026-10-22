package tasks

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	defaults "github.com/mcuadros/go-defaults"
)

// setDefaultsValue is what defaults.SetDefaults stores in a fresh field of
// type typ carrying tag as its `default:`. It is how the tests below ask
// go-defaults about a pointer field's pointed-to type, which it never fills
// in on the pointer itself.
func setDefaultsValue(typ reflect.Type, tag string) any {
	st := reflect.StructOf([]reflect.StructField{{
		Name: "F",
		Type: typ,
		Tag:  reflect.StructTag(`default:"` + tag + `"`),
	}})
	fresh := reflect.New(st)
	defaults.SetDefaults(fresh.Interface())
	return fresh.Elem().Field(0).Interface()
}

func jsonOf(t *testing.T, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal %#v: %v", value, err)
	}
	return string(b)
}

// checkDefaultsAgainstSetDefaults holds every published default in fields to
// what defaults.SetDefaults put in fresh, a new rt with defaults applied, and
// recurses into list and dict items whose element is a struct.
//
// The pointer rule: go-defaults leaves a pointer nil, so a pointer field must
// still be nil in fresh, and its published default is held to what
// SetDefaults stores in a field of the pointed-to type with the same tag.
// That value is what the task coerces nil to.
func checkDefaultsAgainstSetDefaults(t *testing.T, at string, rt reflect.Type, fields []FieldSchema) {
	t.Helper()
	fresh := reflect.New(rt)
	defaults.SetDefaults(fresh.Interface())

	for _, field := range fields {
		where := at + "." + field.Name
		structField, ok := structFieldByYAMLName(rt, field.Name)
		if !ok {
			t.Errorf("%s has no matching struct field", where)
			continue
		}
		tag := structField.Tag.Get("default")
		got := fresh.Elem().FieldByIndex(structField.Index)

		if structField.Type.Kind() == reflect.Pointer {
			if !got.IsNil() {
				t.Errorf("%s is a pointer and SetDefaults set it; the pointer rule no longer holds", where)
			}
			if field.Default != nil {
				want := jsonOf(t, setDefaultsValue(structField.Type.Elem(), tag))
				if have := jsonOf(t, field.Default); have != want {
					t.Errorf("%s default = %s; SetDefaults gives the pointed-to type %s", where, have, want)
				}
			}
		} else if field.Default == nil {
			if !got.IsZero() {
				t.Errorf("%s publishes no default but SetDefaults set it to %#v", where, got.Interface())
			}
		} else if have, want := jsonOf(t, field.Default), jsonOf(t, got.Interface()); have != want {
			t.Errorf("%s default = %s; SetDefaults gives %s", where, have, want)
		}

		if field.Item != nil && field.Item.Type == TypeObject {
			container := structField.Type
			for container.Kind() == reflect.Pointer {
				container = container.Elem()
			}
			checkDefaultsAgainstSetDefaults(t, where+"[]", structElem(container.Elem()), field.Item.Fields)
		}
	}
}

// TestCatalogDefaultsMatchSetDefaults holds every published default, on every
// registered task and every list-item field, to what the loader's
// defaults.SetDefaults actually puts in a fresh task (#645).
func TestCatalogDefaultsMatchSetDefaults(t *testing.T) {
	published := 0
	var count func(fields []FieldSchema)
	count = func(fields []FieldSchema) {
		for _, field := range fields {
			if field.Default != nil {
				published++
			}
			if field.Item != nil {
				count(field.Item.Fields)
			}
		}
	}
	for _, schema := range wholeCatalog(t).Tasks {
		checkDefaultsAgainstSetDefaults(t, schema.Type, taskStructType(lookupTask(schema.Type)), schema.Fields)
		count(schema.Fields)
	}
	if published == 0 {
		t.Fatal("no published defaults; the walk is not reaching the fields")
	}
}

// TestCatalogDefaultIsTyped pins the JSON a consumer sees: a bool field's
// default is a JSON boolean, a string field's a JSON string.
func TestCatalogDefaultIsTyped(t *testing.T) {
	catalog := wholeCatalog(t)
	cases := []struct {
		task, field, want string
	}{
		{"dokku_ps_scale", "skip_deploy", `false`},
		{"dokku_config", "restart", `true`},
		{"dokku_app", "state", `"present"`},
	}
	for _, tc := range cases {
		field := fieldFor(t, schemaFor(t, catalog, tc.task).Fields, tc.field)
		if got := jsonOf(t, field.Default); got != tc.want {
			t.Errorf("%s.%s default = %s; want %s", tc.task, tc.field, got, tc.want)
		}
		if raw := jsonOf(t, field); !strings.Contains(raw, `"default":`+tc.want) {
			t.Errorf("%s.%s encodes as %s; want \"default\":%s", tc.task, tc.field, raw, tc.want)
		}
	}
	if raw := jsonOf(t, fieldFor(t, schemaFor(t, catalog, "dokku_app").Fields, "app")); strings.Contains(raw, `"default"`) {
		t.Errorf("a field with no default still encodes the key: %s", raw)
	}
}

// TestParseFieldDefault covers each kind ParseFieldDefault reads, and checks
// every value it returns against what go-defaults stores for the same Go type
// and tag. The Go type is turned into the catalog's type names the same way
// the catalog does, so the cases also cover that naming.
func TestParseFieldDefault(t *testing.T) {
	cases := []struct {
		name    string
		typ     reflect.Type
		raw     string
		want    any
		wantErr bool
	}{
		{name: "no default", typ: reflect.TypeOf(""), raw: "", want: nil},
		{name: "string", typ: reflect.TypeOf(""), raw: "present", want: "present"},
		{name: "string that looks like a bool", typ: reflect.TypeOf(""), raw: "true", want: "true"},
		{name: "named string", typ: reflect.TypeOf(State("")), raw: "absent", want: "absent"},
		{name: "bool", typ: reflect.TypeOf(false), raw: "true", want: true},
		{name: "bool false", typ: reflect.TypeOf(false), raw: "false", want: false},
		{name: "int", typ: reflect.TypeOf(0), raw: "3", want: int64(3)},
		{name: "negative int", typ: reflect.TypeOf(int32(0)), raw: "-2", want: int64(-2)},
		{name: "float", typ: reflect.TypeOf(0.0), raw: "1.5", want: 1.5},
		{name: "string list", typ: reflect.TypeOf([]string{}), raw: "[a,b]", want: []any{"a", "b"}},
		{name: "list keeps spaces", typ: reflect.TypeOf([]string{}), raw: "[a, b]", want: []any{"a", " b"}},
		{name: "int list", typ: reflect.TypeOf([]int{}), raw: "[1,2]", want: []any{int64(1), int64(2)}},
		{name: "empty list", typ: reflect.TypeOf([]string{}), raw: "[]", want: []any{}},
		{name: "bad bool", typ: reflect.TypeOf(false), raw: "yes", wantErr: true},
		{name: "bad int", typ: reflect.TypeOf(0), raw: "one", wantErr: true},
		{name: "list without brackets", typ: reflect.TypeOf([]string{}), raw: "a,b", wantErr: true},
		{name: "bad list element", typ: reflect.TypeOf([]bool{}), raw: "[true,maybe]", wantErr: true},
		{name: "list of objects", typ: reflect.TypeOf([]PortMapping{}), raw: "[x]", wantErr: true},
		{name: "dict", typ: reflect.TypeOf(map[string]string{}), raw: "a=b", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fieldType := catalogFieldType(tc.typ)
			itemType := ""
			if tc.typ.Kind() == reflect.Slice || tc.typ.Kind() == reflect.Map {
				itemType = elementType(tc.typ.Elem())
			}
			got, err := ParseFieldDefault(fieldType, itemType, tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseFieldDefault(%q, %q, %q) = %#v; want an error", fieldType, itemType, tc.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseFieldDefault(%q, %q, %q): %v", fieldType, itemType, tc.raw, err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseFieldDefault(%q, %q, %q) = %#v; want %#v", fieldType, itemType, tc.raw, got, tc.want)
			}
			if tc.raw == "" {
				return
			}
			if have, want := jsonOf(t, got), jsonOf(t, setDefaultsValue(tc.typ, tc.raw)); have != want {
				t.Errorf("parsed %s; go-defaults stores %s", have, want)
			}
		})
	}
}

// TestCatalogRejectsUnreadableDefault proves a tag go-defaults would silently
// turn into a zero value fails the catalog build instead of publishing it.
func TestCatalogRejectsUnreadableDefault(t *testing.T) {
	type example struct {
		Flag bool `yaml:"flag" default:"yes"`
	}
	if _, err := buildFields(reflect.TypeOf(example{})); err == nil || !strings.Contains(err.Error(), "flag") {
		t.Errorf("buildFields error = %v; want one naming the flag field", err)
	}

	type item struct {
		Size int `yaml:"size" default:"big"`
	}
	type withItems struct {
		Items []item `yaml:"items" ordered:"false"`
	}
	if _, err := buildFields(reflect.TypeOf(withItems{})); err == nil || !strings.Contains(err.Error(), "items: size") {
		t.Errorf("buildFields error = %v; want one naming items: size", err)
	}
}
