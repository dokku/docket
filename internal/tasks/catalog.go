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
// deliberately absent - an in-repo caller that wants those still has Lookup
// and reflect.

// The catalog version is deliberately independent of the --json event stream's
// version (commands.jsonSchemaVersion): the two are different formats with
// different consumers, and a breaking change to one must not force the other's
// consumers to re-branch.

// CatalogVersion is the TaskCatalog.Version this release emits, the `version`
// field of every catalog. It changes only when the catalog's shape does, so a
// consumer can branch on it: adding a key does not bump it.
//
// One change was made within version 1 against that rule: a field's `default`
// was a string whatever its type, and is now a typed value matching the
// field's type (#645), so a bool field's default reads false, not "false".
//
//sdk:export
const CatalogVersion = 1

// Field and item type names. These are the doc-facing type vocabulary, shared
// with the Parameters table the generator renders, and they are what the
// published JSON Schema enumerates.
//
//sdk:export
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
//
//sdk:export
const (
	// IdentityRoleKey marks a field that decides which resource the task
	// manages. Declaration order is key order.
	IdentityRoleKey = "key"

	// IdentityRoleCollection marks a collection the task manages wholesale.
	IdentityRoleCollection = "collection"
)

// Scopes a property can be set in.
//
//sdk:export
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
//
//sdk:export
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

	// Synopsis is the task's Doc(), trimmed. Empty for a task that does not
	// implement Documented.
	Synopsis string `json:"synopsis"`

	// Deprecation is the notice a deprecated task declares. Absent when the
	// task is current.
	Deprecation string `json:"deprecation,omitempty"`

	// Imperative is true for a task that runs an operation rather than
	// declaring a resource, so it has nothing to undo and offers no absent or
	// clear state. Absent for every declarative task.
	Imperative bool `json:"imperative,omitempty"`

	// Requirements are the non-core dokku plugins the task needs, in declared
	// order, as the prose a task page prints. Absent when the task needs
	// nothing beyond dokku core. The Dokku floor is MinimumDokkuVersion;
	// this array stays plugin prose only.
	Requirements []string `json:"requirements,omitempty"`

	// MinimumDokkuVersion is the oldest Dokku release the task's probe parses,
	// as major.minor.patch. Absent when the task declares no floor.
	MinimumDokkuVersion string `json:"minimum_dokku_version,omitempty"`

	// PluginRequirements are the non-core plugins the task needs, in declared
	// order. name is the plugin:list name when it is fixed. name_field is the
	// recipe key whose value is that name. when is the recipe key that must
	// be true before the requirement applies.
	PluginRequirements []PluginRequirementSchema `json:"plugin_requirements,omitempty"`

	// Export says whether `docket export` can reconstruct the task from a live
	// server.
	Export ExportSupport `json:"export"`

	// Probe says how much of its own state the task's Plan() can read back. An
	// "unsupported" task plans as drift on every run and never converges.
	Probe ProbeSupport `json:"probe"`

	// Identity is what the task addresses on the server.
	Identity IdentitySchema `json:"identity"`

	// Objects are the app and the datastore service the task changes, in a
	// fixed order: the app, then the service. Concurrent tasks on one object
	// are serialized; Creates and Restarts say how to order them. Absent on a
	// task that changes neither.
	Objects []ObjectSchema `json:"objects,omitempty"`

	// Fields are the recipe keys the task accepts, in declaration order -
	// which is the order the reference page lists them in.
	Fields []FieldSchema `json:"fields"`

	// Constraints are the rules over several of Fields, in declaration
	// order: which must be set together, which exclude each other, which
	// need one another. Absent when the task declares none. Docket enforces
	// every one of them, so a consumer that attaches them to its own schema
	// rejects exactly what docket would.
	Constraints []Constraint `json:"constraints,omitempty"`

	// PropertySchema is present only for a task that manages an enumerable
	// dokku property table. Its absence on a task that has a `property` field
	// means the legal names are not enumerable from docket, not that there are
	// none - see dokku_service_property.
	PropertySchema *PropertySchema `json:"property_schema,omitempty"`

	// Notes are the task's reference-page notes, rendered, in the order the
	// page prints them: the Deprecated and Imperative admonitions, the
	// Requirements, Runner requirements, Export support, Probe support,
	// Identity, Objects and Constraints sections, and the Properties table.
	// Each is rendered from the keys above, and a note whose text depends on
	// the state also carries one rendering per state. Absent notes do not
	// apply to the task.
	Notes []NoteSchema `json:"notes,omitempty"`

	// Examples are the task's documented examples, the same ones its reference
	// page publishes.
	Examples []ExampleSchema `json:"examples,omitempty"`
}

// PluginRequirementSchema is one non-core plugin a task needs. Name is the
// plugin:list name when the task names a fixed plugin. NameField is the
// recipe key whose value is that name, for a task such as the datastore
// family whose plugin follows `service`. When is the recipe key that must be
// true before the requirement applies. The keys are recipe keys, never Go
// field names.
type PluginRequirementSchema struct {
	Name      string `json:"name,omitempty"`
	Minimum   string `json:"minimum"`
	When      string `json:"when,omitempty"`
	NameField string `json:"name_field,omitempty"`
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

// ObjectSchema is one object a task changes, projected from its `serialize`
// field tags and its ObjectEffects.
type ObjectSchema struct {
	// Kind is "app" or "service".
	Kind ObjectKind `json:"kind"`

	// Keys are the recipe keys naming the object: the app's key, or the
	// datastore type's key then the service name's key. The object applies
	// only when every one of them is set, so an app-or-global task with
	// `global: true` changes no app.
	Keys []string `json:"keys"`

	// Creates is true for the task that creates the object, and destroys it
	// in its absent state. Every other task on the object depends on it.
	Creates bool `json:"creates,omitempty"`

	// Restarts is present when applying the task can restart or redeploy the
	// object, and says when. An empty object means whenever it applies.
	Restarts *Restart `json:"restarts,omitempty"`
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

	// RequiredWhen are the states in which the field must be set, from its
	// `required_when:` tag, in tag order. Absent when the field is never
	// conditionally required. The states are values of the task's `state`
	// field, and on an element's field they are still the task's states.
	RequiredWhen []string `json:"required_when,omitempty"`

	// ForbiddenWhen are the states in which the field must not be set, from
	// its `forbidden_when:` tag, in tag order. Absent when the field is
	// allowed in every state.
	ForbiddenWhen []string `json:"forbidden_when,omitempty"`

	// Default is the field's `default:` tag as a typed value matching Type -
	// a bool, a number, a string, or a list of those - as ParseFieldDefault
	// reads it. Absent when the field has no default.
	// TestCatalogDefaultsMatchSetDefaults holds it to what defaults.SetDefaults
	// puts in a fresh task. go-defaults leaves pointer fields untouched, so on
	// a *bool the value documents what the task coerces nil to, not a value
	// the loader writes into the struct.
	Default any `json:"default,omitempty"`

	// Choices are the `options:` tag values, in tag order. Absent when the
	// field is not an enum.
	Choices []string `json:"choices,omitempty"`

	// Description is the raw `description:` tag. Prose the reference page
	// appends - "(sensitive)", "Each item has: ..." - is published in Note
	// rather than baked in here.
	Description string `json:"description,omitempty"`

	// Note is what the reference page appends to Description, rendered from
	// the keys of this field, overall and per state. Absent when the page
	// appends nothing.
	Note *FieldNoteSchema `json:"note,omitempty"`

	// Sensitive marks a field whose value is a secret. A consumer that echoes
	// a recipe must mask it.
	Sensitive bool `json:"sensitive,omitempty"`

	// RunnerFile marks a field naming a file docket reads on the machine it
	// runs on rather than on the dokku server, so the file must exist there
	// even when docket drives the server over --host. A path a task hands to
	// dokku as an argument resolves on the server and is not marked.
	RunnerFile bool `json:"runner_file,omitempty"`

	// WriteOnly marks a field whose value the server stores but a read of the
	// resource never returns, such as a password applied at create time:
	// PlanResult.Observed, and so sdk.ReadTask, leaves it empty, and a
	// consumer refreshing state keeps the configured value rather than
	// reading the absence back as drift. An exported task leaves it empty or
	// lifts it into a required input with no value, unless the task's export
	// caveat names another command that reads it back, as dokku_certs does
	// for its PEM. A probe may still verify the value by comparison
	// (registry:auth-status, a fingerprint), which is why this says nothing
	// about drift detection.
	WriteOnly bool `json:"write_only,omitempty"`

	// ApplyOnly marks a field that steers how apply runs - a restart flag, a
	// commit author, a forced password re-issue - and is not server state at
	// all. An exported task carries its zero value or default, so a consumer
	// refreshing state keeps the configured value the same way it does for a
	// WriteOnly field.
	ApplyOnly bool `json:"apply_only,omitempty"`

	// Ordered is present on every list field and absent on every other kind.
	// True means docket applies and compares the elements in order, so a
	// reorder is drift (buildpacks, exposed service ports). False means docket
	// ignores element order. It says nothing about uniqueness, which Unique
	// and an identity collection's item identity declare. A consumer choosing
	// between a list and a set type reads it instead of keeping its own table,
	// and a list without it comes from a docket that predates the key (#605).
	Ordered *bool `json:"ordered,omitempty"`

	// Unique marks a list whose members must be distinct, from its
	// `unique:"true"` tag: validation rejects a member equal to an earlier
	// one. Absent on any other list and on every other kind. An identity
	// collection does not carry it, since its item identity already makes
	// members distinct, so every unordered list is one or the other and can
	// be modelled as a set (#648).
	Unique bool `json:"unique,omitempty"`

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

	// Constraints are the rules over several of the element's Fields, read
	// the same way as TaskSchema.Constraints; their states are the task's.
	// Present only when Type is "object" and the element declares any.
	Constraints []Constraint `json:"constraints,omitempty"`
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

	// Rejected are the name families this task refuses because another task
	// manages them. A name matching one of these prefixes is absent from
	// Properties for a different reason than a typo is, and a consumer that
	// reports it as an unknown name sends the user looking through a list it
	// will never be in.
	Rejected []RejectedPropertySchema `json:"rejected,omitempty"`
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

	// Scopes is a non-empty subset of ["app", "global"], in that order, read
	// the same way as PropertyEntrySchema.Scopes: a family listed only under
	// "global" is rejected for an app at validate time, matching dokku's own
	// rejection. traefik's `dns-provider-*` credentials are global-only that
	// way, while letsencrypt's identically named family takes both.
	Scopes []string `json:"scopes"`
}

// RejectedPropertySchema is a family of property names the task refuses,
// matched by prefix, and the task that manages them instead.
type RejectedPropertySchema struct {
	// Prefix is what a member's name starts with.
	Prefix string `json:"prefix"`

	// Replacement is the task type to write the property against instead.
	Replacement string `json:"replacement"`

	// Reason says why this task does not manage the family.
	Reason string `json:"reason"`
}

// PropertyFieldName is the recipe key every property table constrains.
//
//sdk:export
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

	schema.Dynamic = append(schema.Dynamic, DynamicPropertyFamilies(table.Plugin())...)
	schema.Rejected = append(schema.Rejected, RejectedPropertyFamilies(table)...)
	return schema
}

// Catalog describes every registered task type, sorted by type key. It is what
// `docket schema` emits.
//
//sdk:export
func Catalog() (TaskCatalog, error) {
	return CatalogFor(nil)
}

// CatalogFor describes only the given task types, sorted by type key. A nil or
// empty selection means every registered task, so the whole catalog and a
// narrowed one are the same code path rather than two that can drift.
//
// Sorting is by type key whatever order the keys arrived in, which is what lets
// a narrowed catalog be diffed against a full one and makes two runs of the
// same selection byte-identical. Duplicates collapse: the document is a set
// keyed by type, so naming a type twice is not an error.
//
// An unregistered type is an error rather than a silently smaller document - a
// caller asking for a type that does not exist wants to know. Keys are checked
// in the order given, so the message names the first one the caller wrote, and
// all of them are checked before any schema is built.
//
//sdk:export
func CatalogFor(typeKeys []string) (TaskCatalog, error) {
	// The other error comes from examples(), which yaml-marshals each example
	// struct. Nothing else here can fail.
	names := TaskTypes()
	if len(typeKeys) > 0 {
		seen := make(map[string]bool, len(typeKeys))
		names = make([]string, 0, len(typeKeys))
		for _, key := range typeKeys {
			if _, ok := registeredType(key); !ok {
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
		task, _ := Lookup(name)
		schema, err := TaskSchemaOf(name, task)
		if err != nil {
			return TaskCatalog{}, err
		}
		catalog.Tasks = append(catalog.Tasks, schema)
	}
	return catalog, nil
}

// TaskSchemaOf describes one task. Exported so a caller holding a single
// prototype does not have to build all of them.
func TaskSchemaOf(typeKey string, task Task) (TaskSchema, error) {
	schema := TaskSchema{
		Type:        typeKey,
		Synopsis:    strings.TrimSpace(TaskSynopsis(task)),
		Identity:    IdentitySchema{Keys: IdentityKeyNames(task)},
		Constraints: cloneConstraints(TaskConstraints(task)),
	}

	fields, err := buildFields(taskStructOf(task))
	if err != nil {
		return TaskSchema{}, fmt.Errorf("fields for task %s: %w", typeKey, err)
	}
	schema.Fields = fields

	schema.Deprecation = strings.TrimSpace(TaskDeprecation(task))
	schema.Imperative = TaskImperative(task)
	if support, ok := TaskMinimumVersions(task); ok {
		schema.MinimumDokkuVersion = support.Dokku
		for _, plugin := range support.Plugins {
			if plugin.Text != "" {
				schema.Requirements = append(schema.Requirements, plugin.Text)
			}
			item := PluginRequirementSchema{Minimum: plugin.Minimum}
			if plugin.NameField != "" {
				item.NameField = recipeKeyFor(task, plugin.NameField)
			} else {
				item.Name = plugin.Name
			}
			if plugin.WhenField != "" {
				item.When = recipeKeyFor(task, plugin.WhenField)
			}
			schema.PluginRequirements = append(schema.PluginRequirements, item)
		}
	}
	schema.Export, _ = TaskExportSupport(task)
	schema.Probe, _ = TaskProbeSupport(task)

	for _, object := range TaskObjects(task) {
		schema.Objects = append(schema.Objects, ObjectSchema{
			Kind:     object.Kind,
			Keys:     object.Keys,
			Creates:  object.Creates,
			Restarts: object.Restarts,
		})
	}

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

	examples, err := TaskExamples(task)
	if err != nil {
		return TaskSchema{}, fmt.Errorf("examples for task %s: %w", typeKey, err)
	}
	for _, example := range examples {
		schema.Examples = append(schema.Examples, ExampleSchema{
			Name: example.Name,
			YAML: strings.TrimSpace(example.Codeblock) + "\n",
		})
	}

	applyFieldNotes(schema.Fields, noteStates(schema.Fields))
	schema.Notes = buildTaskNotes(schema)

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
	for rt != nil && rt.Kind() == reflect.Pointer {
		rt = rt.Elem()
	}
	return rt
}

// buildFields reflects over a task (or element) struct and returns its recipe
// keys in declaration order. The error is a `default:` tag ParseFieldDefault
// cannot read.
func buildFields(rt reflect.Type) ([]FieldSchema, error) {
	if rt == nil || rt.Kind() != reflect.Struct {
		return nil, nil
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
			Required:      field.Tag.Get("required") == "true" && def == "",
			RequiredWhen:  conditionalStates(field.Tag.Get(requiredWhenTag)),
			ForbiddenWhen: conditionalStates(field.Tag.Get(forbiddenWhenTag)),
			Description:   field.Tag.Get("description"),
			Sensitive:     field.Tag.Get("sensitive") == "true",
			RunnerFile:    field.Tag.Get("runner_file") == "true",
			WriteOnly:     field.Tag.Get("write_only") == "true",
			ApplyOnly:     field.Tag.Get("apply_only") == "true",
		}
		if opts := field.Tag.Get("options"); opts != "" {
			schema.Choices = strings.Split(opts, ",")
		}
		if schema.Type == TypeList {
			schema.Ordered = orderedTag(field.Tag)
			schema.Unique = field.Tag.Get(uniqueTag) == "true"
		}
		switch field.Tag.Get(identityTag) {
		case identityTagKey:
			schema.Identity = IdentityRoleKey
		case identityTagCollection:
			schema.Identity = IdentityRoleCollection
		}
		item, err := buildItem(field.Type, schema.Identity == IdentityRoleCollection)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		schema.Item = item

		itemType := ""
		if item != nil {
			itemType = item.Type
		}
		value, err := ParseFieldDefault(schema.Type, itemType, def)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		schema.Default = value

		fields = append(fields, schema)
	}
	return fields, nil
}

// orderedTag reads a list field's `ordered:` tag. A missing or malformed tag
// returns nil rather than guessing, and TestCatalogOrderedMatchesTags fails on
// it, so a new list field cannot ship without deciding.
func orderedTag(tag reflect.StructTag) *bool {
	var ordered bool
	switch tag.Get("ordered") {
	case "true":
		ordered = true
	case "false":
		ordered = false
	default:
		return nil
	}
	return &ordered
}

// buildItem describes the element shape of a list or dict field, and returns
// nil for a scalar. collection says whether the field is tagged
// `identity:"collection"`, which is what decides whether item identity is
// reported: an untagged list is an attribute of the resource (storage_mount's
// `phases` narrows one mount), not the set of items the task manages, so it
// has no item identity to declare. The error is an element field's
// `default:` tag ParseFieldDefault cannot read.
func buildItem(t reflect.Type, collection bool) (*ItemSchema, error) {
	for t.Kind() == reflect.Pointer {
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
		return nil, nil
	}

	if item.Type == TypeObject {
		fields, err := buildFields(structElem(t.Elem()))
		if err != nil {
			return nil, err
		}
		item.Fields = fields
		item.Constraints = cloneConstraints(structConstraints(structElem(t.Elem())))
	}
	if collection {
		item.Identity, item.Keys = collectionItemIdentity(t)
	}
	return &item, nil
}

// elementType names a collection's element type. A struct element is
// "object" - the shape is then spelled out in ItemSchema.Fields - and an
// interface element is "any", since map[string]any accepts whatever the
// recipe supplies.
func elementType(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
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
	for t.Kind() == reflect.Pointer {
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
	if t.Kind() == reflect.Pointer {
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
