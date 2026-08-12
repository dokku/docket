package tasks

import (
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
)

// The catalog is the machine-readable form of everything docket knows about
// its own task types. It exists because that knowledge used to live only in
// generate/docs.go - a `package main` binary run by `make docs` - so the only
// thing that could be done with it was render markdown, and anything else that
// wanted it had to scrape docs/tasks/*.md or vendor the reflection (#422).
//
// Now the reflection lives here and the docs generator renders *from* the
// catalog, which is what stops the published JSON and the published markdown
// from ever describing different things.
//
// The document describes the recipe surface, not the Go one: every name in it
// is a key a recipe author types. Go field names and Go type names are
// deliberately absent - an in-repo caller that wants those still has
// RegisteredTasks and reflect.

// CatalogVersion is the wire-format version every emitted catalog carries in
// its `version` field.
//
// It is deliberately independent of the --json event stream's version
// (commands.jsonSchemaVersion): the two are different formats with different
// consumers, and a breaking change to one must not force the other's consumers
// to re-branch. Bumps are reserved for breaking changes; adding a key within a
// major version does not bump.
const CatalogVersion = 1

// Field and item type names. These are the doc-facing type vocabulary, shared
// with the Parameters table the generator renders, and they are what the
// published JSON Schema enumerates.
const (
	// TypeString covers Go strings and named string types such as State.
	TypeString = "string"

	// TypeBool covers bool and *bool.
	TypeBool = "bool"

	// TypeInt covers every sized integer kind.
	TypeInt = "int"

	// TypeFloat covers float32 and float64. No task field uses one today.
	TypeFloat = "float"

	// TypeList is a slice or array field.
	TypeList = "list"

	// TypeDict is a map field.
	TypeDict = "dict"

	// TypeObject is a struct element inside a list, such as a PortMapping. It
	// never appears as a field's own type - a task has no bare struct field.
	TypeObject = "object"

	// TypeAny is an element with no declared type, as in map[string]any.
	TypeAny = "any"

	// TypeUnknown is the fallback for a Go kind none of the above covers.
	// TestCatalogFieldTypesAreKnown fails on it, so adding an exotic field type
	// to a task is a build failure rather than a leaked Go type name in the
	// published JSON.
	TypeUnknown = "unknown"
)

// Identity roles a field can carry, from its `identity:` struct tag.
const (
	// IdentityRoleKey marks a field that decides which resource the task
	// manages. Declaration order is key order.
	IdentityRoleKey = "key"

	// IdentityRoleCollection marks a collection the task manages wholesale.
	IdentityRoleCollection = "collection"
)

// Scopes a property can be set in.
const (
	// PropertyScopeApp means the property has a per-app form, set by naming an
	// app.
	PropertyScopeApp = "app"

	// PropertyScopeGlobal means the property has a global form, set with
	// `global: true`.
	PropertyScopeGlobal = "global"
)

// TaskCatalog is the machine-readable description of a set of registered task
// types. It is what `docket schema` emits.
type TaskCatalog struct {
	// Version is CatalogVersion. Consumers should branch on it so a future
	// schema change does not silently break them.
	Version int `json:"version"`

	// Tasks are the requested task types - every registered one unless the
	// caller narrowed the selection - sorted by Type.
	Tasks []TaskSchema `json:"tasks"`
}

// TaskSchema describes one registered task type. The Go field order here is
// the key order of the emitted JSON.
type TaskSchema struct {
	// Type is the registry key - the name a recipe uses to invoke the task,
	// such as "dokku_apps_property".
	Type string `json:"type"`

	// Synopsis is the task's Doc(), verbatim.
	Synopsis string `json:"synopsis"`

	// Deprecation is the notice a deprecated task declares. Absent when the
	// task is current.
	Deprecation string `json:"deprecation,omitempty"`

	// Requirements are the non-core dokku plugins the task needs, in declared
	// order. Absent when the task needs nothing beyond dokku core.
	Requirements []string `json:"requirements,omitempty"`

	// Export says whether `docket export` can reconstruct the task from a live
	// server.
	Export ExportSupport `json:"export"`

	// Probe says how much of its own state the task's Plan() can read back. An
	// "unsupported" task plans as drift on every run and never converges.
	Probe ProbeSupport `json:"probe"`

	// Identity is what the task addresses on the server.
	Identity IdentitySchema `json:"identity"`

	// Fields are the recipe keys the task accepts, in declaration order -
	// which is the order the reference page lists them in.
	Fields []FieldSchema `json:"fields"`

	// PropertySchema is present only for a task that manages an enumerable
	// dokku property table. Its absence on a task that has a `property` field
	// means the legal names are not enumerable from docket, not that there are
	// none - see dokku_service_property.
	PropertySchema *PropertySchema `json:"property_schema,omitempty"`

	// Examples are the task's documented examples, the same ones its reference
	// page publishes.
	Examples []ExampleSchema `json:"examples,omitempty"`
}

// IdentitySchema is what a task addresses, projected from the `identity:"key"`
// and `identity:"collection"` field tags.
type IdentitySchema struct {
	// Keys are the recipe keys of the identity key fields, in declaration
	// order - the order IdentityAddress renders them in. A key holding its
	// zero value is omitted from the rendered address, which is what lets the
	// mutually exclusive app / global pair produce two different addresses.
	//
	// Derivable from the fields carrying `"identity": "key"`, and duplicated
	// here because building a resource address is the main thing a consumer
	// does with this.
	Keys []string `json:"keys"`

	// Collections are the collection fields the task manages wholesale.
	Collections []IdentityCollectionSchema `json:"collections,omitempty"`
}

// IdentityCollectionSchema is one `identity:"collection"` field.
type IdentityCollectionSchema struct {
	// Name is the collection's recipe key.
	Name string `json:"name"`

	// Item says what identifies one entry: "value", "map_key", or "fields".
	Item ItemIdentity `json:"item"`

	// ItemKeys are the element struct's own identity key names, in declaration
	// order. Populated only when Item is "fields".
	ItemKeys []string `json:"item_keys,omitempty"`
}

// FieldSchema is one recipe key a task accepts.
type FieldSchema struct {
	// Name is the recipe key, from the yaml tag. A field tagged `yaml:"-"` is
	// absent from the catalog entirely.
	Name string `json:"name"`

	// Type is the doc-facing type name.
	Type string `json:"type"`

	// Required mirrors what the loader enforces: `required:"true"` with no
	// default. A field carrying a default is never actually required, because
	// defaults.SetDefaults fills it in before the loader's zero-check runs.
	Required bool `json:"required"`

	// Default is the literal `default:` tag text, to be read according to
	// Type. It is not pre-parsed, so it cannot diverge from what go-defaults
	// does with it. Note that go-defaults leaves pointer fields untouched: on
	// a *bool the tag documents what the task coerces nil to, not a value the
	// loader writes into the struct.
	Default string `json:"default,omitempty"`

	// Choices are the `options:` tag values, in tag order. Absent when the
	// field is not an enum.
	Choices []string `json:"choices,omitempty"`

	// Description is the raw `description:` tag. Prose the reference page
	// appends - "(sensitive)", "Each item has: ..." - is derived from
	// Sensitive and Item rather than baked in here.
	Description string `json:"description,omitempty"`

	// Sensitive marks a field whose value is a secret. A consumer that echoes
	// a recipe must mask it.
	Sensitive bool `json:"sensitive,omitempty"`

	// Identity is "key" or "collection" when the field carries an identity
	// tag, and absent otherwise.
	Identity string `json:"identity,omitempty"`

	// Item describes the element shape of a list or dict field. Absent on a
	// scalar.
	Item *ItemSchema `json:"item,omitempty"`
}

// ItemSchema is the element shape of a list or dict field. `list` alone cannot
// tell a []string from a []PortMapping, which is the gap this closes.
type ItemSchema struct {
	// KeyType is the map key's type. Present only on a dict; always "string"
	// today.
	KeyType string `json:"key_type,omitempty"`

	// Type is the element's type: a scalar name, "object" when the element is
	// a struct, or "any" when it is untyped.
	Type string `json:"type"`

	// Identity is what identifies one entry, for a field tagged
	// `identity:"collection"`: "value", "map_key", or "fields". Absent on a
	// collection the task treats as an attribute rather than as the set of
	// items it manages.
	Identity ItemIdentity `json:"identity,omitempty"`

	// Keys are the element struct's own identity key names. Present only when
	// Identity is "fields".
	Keys []string `json:"keys,omitempty"`

	// Fields are the element struct's own fields, in declaration order.
	// Present only when Type is "object". An element struct declares the same
	// tags a task does, so this is where a consumer learns that a
	// HttpAuthUser's `password` is sensitive.
	Fields []FieldSchema `json:"fields,omitempty"`
}

// ExampleSchema is one documented example: the heading its reference page
// gives it, and the YAML snippet itself.
type ExampleSchema struct {
	Name string `json:"name"`
	YAML string `json:"yaml"`
}

// PropertySchema is the enumerable property table a *_property task manages -
// the real schema behind its free-form-looking `property` field.
type PropertySchema struct {
	// Plugin is the dokku plugin, for example "apps" or "scheduler-k3s".
	Plugin string `json:"plugin"`

	// Subcommand is the dokku subcommand a set or unset runs, for example
	// "apps:set" or "buildpacks:set-property".
	Subcommand string `json:"subcommand"`

	// Field is the recipe key the table constrains. Always "property".
	Field string `json:"field"`

	// Properties are the enumerable property names, sorted.
	Properties []PropertyEntrySchema `json:"properties"`

	// Dynamic are the name families the table cannot enumerate, because dokku
	// validates them through the set subcommand rather than through its report
	// schema. A name matching one of these prefixes is legal even though it is
	// absent from Properties.
	Dynamic []DynamicPropertySchema `json:"dynamic,omitempty"`
}

// PropertyEntrySchema is one property name and where it may be used.
type PropertyEntrySchema struct {
	// Name is the value a recipe puts in `property:`.
	Name string `json:"name"`

	// Scopes is a non-empty subset of ["app", "global"], in that order. Using
	// the property in a scope it does not list is rejected at validate time,
	// matching dokku's own CLI rejection.
	Scopes []string `json:"scopes"`

	// AppReportKey and GlobalReportKey are the keys
	// `dokku <plugin>:report --format json` emits for the property in each
	// scope. Absent for a scope the property has no form in.
	AppReportKey    string `json:"app_report_key,omitempty"`
	GlobalReportKey string `json:"global_report_key,omitempty"`

	// Sensitive marks a property whose value is a secret.
	Sensitive bool `json:"sensitive,omitempty"`
}

// DynamicPropertySchema is a family of property names matched by prefix.
type DynamicPropertySchema struct {
	// Prefix is what a member's name starts with.
	Prefix string `json:"prefix"`

	// Probeable is true when the plugin's report surfaces a row per set
	// member, so docket can read the value back. False means a recipe using
	// the family reports drift on every run and never converges.
	Probeable bool `json:"probeable"`

	// Sensitive is true when docket treats members as secrets.
	Sensitive bool `json:"sensitive,omitempty"`
}

// PropertyFieldName is the recipe key every property table constrains.
const PropertyFieldName = "property"

// buildPropertySchema projects a runtime PropertyTable into its published
// form: names sorted, scopes spelled out rather than left implicit in an empty
// report key, and the plugin's dynamic families attached.
func buildPropertySchema(table PropertyTable) PropertySchema {
	schema := PropertySchema{
		Plugin:     table.Plugin(),
		Subcommand: table.Subcommand,
		Field:      PropertyFieldName,
		Properties: make([]PropertyEntrySchema, 0, len(table.Keys)),
	}

	names := make([]string, 0, len(table.Keys))
	for name := range table.Keys {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		entry := table.Keys[name]
		property := PropertyEntrySchema{
			Name:            name,
			AppReportKey:    entry.PerApp,
			GlobalReportKey: entry.Global,
			Sensitive:       entry.Sensitive,
		}
		if entry.PerApp != "" {
			property.Scopes = append(property.Scopes, PropertyScopeApp)
		}
		if entry.Global != "" {
			property.Scopes = append(property.Scopes, PropertyScopeGlobal)
		}
		schema.Properties = append(schema.Properties, property)
	}

	for _, family := range DynamicPropertyFamilies(table.Plugin()) {
		schema.Dynamic = append(schema.Dynamic, family)
	}
	return schema
}

// Catalog returns the catalog for every registered task, sorted by type key.
func Catalog() (TaskCatalog, error) {
	return CatalogFor(nil)
}

// CatalogFor returns the catalog narrowed to typeKeys, sorted by type key. A
// nil or empty selection means every registered task, so the whole catalog and
// a narrowed one are the same code path rather than two that can drift.
//
// Sorting is by type key whatever order the keys arrived in, which is what lets
// a narrowed catalog be diffed against a full one and makes two runs of the
// same selection byte-identical. Duplicates collapse: the document is a set
// keyed by type, so naming a type twice is not an error.
//
// An unregistered key is an error rather than a silently smaller document - a
// caller asking for a type that does not exist wants to know. Keys are checked
// in the order given, so the message names the first one the caller wrote, and
// all of them are checked before any schema is built.
//
// The other error comes from Examples(), which yaml-marshals each example
// struct. Nothing else here can fail.
func CatalogFor(typeKeys []string) (TaskCatalog, error) {
	names := RegisteredTaskNames()
	if len(typeKeys) > 0 {
		seen := make(map[string]bool, len(typeKeys))
		names = make([]string, 0, len(typeKeys))
		for _, key := range typeKeys {
			if _, ok := RegisteredTasks[key]; !ok {
				return TaskCatalog{}, fmt.Errorf("unknown task type %q", key)
			}
			if seen[key] {
				continue
			}
			seen[key] = true
			names = append(names, key)
		}
		sort.Strings(names)
	}

	catalog := TaskCatalog{Version: CatalogVersion, Tasks: make([]TaskSchema, 0, len(names))}
	for _, name := range names {
		schema, err := TaskSchemaOf(name, RegisteredTasks[name])
		if err != nil {
			return TaskCatalog{}, err
		}
		catalog.Tasks = append(catalog.Tasks, schema)
	}
	return catalog, nil
}

// RegisteredTaskNames returns every registered task type, sorted. The result is
// a fresh slice, so a caller may sort, filter or truncate it without disturbing
// the registry.
func RegisteredTaskNames() []string {
	names := make([]string, 0, len(RegisteredTasks))
	for name := range RegisteredTasks {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// TaskSchemaOf describes one task. Exported so a caller holding a single
// prototype does not have to build all of them.
func TaskSchemaOf(typeKey string, task Task) (TaskSchema, error) {
	schema := TaskSchema{
		Type:     typeKey,
		Synopsis: strings.TrimSpace(task.Doc()),
		Fields:   buildFields(taskStructOf(task)),
		Identity: IdentitySchema{Keys: IdentityKeyNames(task)},
	}

	schema.Deprecation = strings.TrimSpace(TaskDeprecation(task))
	if doc, ok := task.(RequirementsDocer); ok {
		schema.Requirements = doc.Requirements()
	}
	schema.Export, _ = TaskExportSupport(task)
	schema.Probe, _ = TaskProbeSupport(task)

	for _, collection := range TaskIdentityCollections(task) {
		schema.Identity.Collections = append(schema.Identity.Collections, IdentityCollectionSchema{
			Name:     collection.YAMLName,
			Item:     collection.Item,
			ItemKeys: collection.ItemKeys,
		})
	}

	if table, ok := TaskPropertyTable(task); ok {
		propertySchema := buildPropertySchema(table)
		schema.PropertySchema = &propertySchema
	}

	examples, err := task.Examples()
	if err != nil {
		return TaskSchema{}, fmt.Errorf("examples for task %s: %w", typeKey, err)
	}
	for _, example := range examples {
		schema.Examples = append(schema.Examples, ExampleSchema{
			Name: example.Name,
			YAML: strings.TrimSpace(example.Codeblock) + "\n",
		})
	}

	return schema, nil
}

// Encode writes the catalog as an indented JSON document with a trailing
// newline. This is the byte format `docket schema` emits, so it lives with the
// catalog rather than with the command.
//
// HTML escaping is off: json.Marshal would turn a description mentioning
// `dokku <plugin>:report` into `<plugin>`, which is unreadable for a
// document a human also skims.
func (c TaskCatalog) Encode(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(c)
}

// taskStructOf dereferences a registered task down to its struct type. The
// registry stores pointer prototypes.
func taskStructOf(task Task) reflect.Type {
	rt := reflect.TypeOf(task)
	for rt != nil && rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}
	return rt
}

// buildFields reflects over a task (or element) struct and returns its recipe
// keys in declaration order.
func buildFields(rt reflect.Type) []FieldSchema {
	if rt == nil || rt.Kind() != reflect.Struct {
		return nil
	}

	var fields []FieldSchema
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if field.PkgPath != "" { // unexported
			continue
		}
		name := catalogYAMLName(field.Tag, field.Name)
		if name == "" {
			continue
		}

		def := field.Tag.Get("default")
		schema := FieldSchema{
			Name: name,
			Type: catalogFieldType(field.Type),
			// A field with a default is effectively optional even if it is
			// tagged required:"true".
			Required:    field.Tag.Get("required") == "true" && def == "",
			Default:     def,
			Description: field.Tag.Get("description"),
			Sensitive:   field.Tag.Get("sensitive") == "true",
		}
		if opts := field.Tag.Get("options"); opts != "" {
			schema.Choices = strings.Split(opts, ",")
		}
		switch field.Tag.Get(identityTag) {
		case identityTagKey:
			schema.Identity = IdentityRoleKey
		case identityTagCollection:
			schema.Identity = IdentityRoleCollection
		}
		schema.Item = buildItem(field.Type, schema.Identity == IdentityRoleCollection)

		fields = append(fields, schema)
	}
	return fields
}

// buildItem describes the element shape of a list or dict field, and returns
// nil for a scalar. collection says whether the field is tagged
// `identity:"collection"`, which is what decides whether item identity is
// reported: an untagged list is an attribute of the resource (storage_mount's
// `phases` narrows one mount), not the set of items the task manages, so it
// has no item identity to declare.
func buildItem(t reflect.Type, collection bool) *ItemSchema {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	var item ItemSchema
	switch t.Kind() {
	case reflect.Map:
		item.KeyType = catalogFieldType(t.Key())
		item.Type = elementType(t.Elem())
	case reflect.Slice, reflect.Array:
		item.Type = elementType(t.Elem())
	default:
		return nil
	}

	if item.Type == TypeObject {
		item.Fields = buildFields(structElem(t.Elem()))
	}
	if collection {
		item.Identity, item.Keys = collectionItemIdentity(t)
	}
	return &item
}

// elementType names a collection's element type. A struct element is
// "object" - the shape is then spelled out in ItemSchema.Fields - and an
// interface element is "any", since map[string]any accepts whatever the
// recipe supplies.
func elementType(t reflect.Type) string {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() == reflect.Struct {
		return TypeObject
	}
	if t.Kind() == reflect.Interface {
		return TypeAny
	}
	return catalogFieldType(t)
}

// structElem returns the struct type behind a collection's element type, or
// nil when the element is not a struct.
func structElem(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return nil
	}
	return t
}

// catalogFieldType maps a Go field type to the doc-facing type name. Named
// string types such as State render as "string"; slices render as "list" and
// maps as "dict", mirroring Ansible's type vocabulary. Pointer fields (e.g. an
// optional *bool) render as their element type so they read as "bool" rather
// than "*bool".
//
// The fallback is TypeUnknown rather than the Go type name: a Go identifier
// has no meaning to a consumer of the recipe surface, and
// TestCatalogFieldTypesAreKnown fails on it so an exotic new field type is
// caught at build time instead of shipping.
func catalogFieldType(t reflect.Type) string {
	if t.Kind() == reflect.Ptr {
		return catalogFieldType(t.Elem())
	}
	switch t.Kind() {
	case reflect.String:
		return TypeString
	case reflect.Bool:
		return TypeBool
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return TypeInt
	case reflect.Float32, reflect.Float64:
		return TypeFloat
	case reflect.Slice, reflect.Array:
		return TypeList
	case reflect.Map:
		return TypeDict
	case reflect.Interface:
		return TypeAny
	default:
		return TypeUnknown
	}
}

// catalogYAMLName extracts the recipe key for a field from its yaml tag,
// dropping any ",omitempty"-style suffix. Returns "" when the field is not
// serialized, which drops it from the catalog entirely.
//
// This is deliberately not yamlFieldName (export.go): that one maps `yaml:"-"`
// to the lowercased Go field name, because its callers ask "what would this
// field be called" about a field they already know is exported. Here the
// question is "is this a key a recipe can set", and the answer for `yaml:"-"`
// is no.
func catalogYAMLName(tag reflect.StructTag, fieldName string) string {
	name := tag.Get("yaml")
	if comma := strings.IndexByte(name, ','); comma >= 0 {
		name = name[:comma]
	}
	if name == "-" {
		return ""
	}
	if name == "" {
		return strings.ToLower(fieldName)
	}
	return name
}
