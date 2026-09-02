package tasks

import (
	"bytes"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// wholeCatalog builds the unnarrowed catalog once for a test, failing fast if
// it cannot. Named to keep it clear of CatalogFor, which selects task types.
func wholeCatalog(t *testing.T) TaskCatalog {
	t.Helper()
	catalog, err := Catalog()
	if err != nil {
		t.Fatalf("Catalog(): %v", err)
	}
	return catalog
}

// schemaFor returns the catalog entry for one task type.
func schemaFor(t *testing.T, catalog TaskCatalog, typeKey string) TaskSchema {
	t.Helper()
	for _, schema := range catalog.Tasks {
		if schema.Type == typeKey {
			return schema
		}
	}
	t.Fatalf("catalog has no entry for %q", typeKey)
	return TaskSchema{}
}

// fieldFor returns the named field from a list, failing when it is absent.
func fieldFor(t *testing.T, fields []FieldSchema, name string) FieldSchema {
	t.Helper()
	for _, field := range fields {
		if field.Name == name {
			return field
		}
	}
	t.Fatalf("no field named %q (have %v)", name, fieldNames(fields))
	return FieldSchema{}
}

func fieldNames(fields []FieldSchema) []string {
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		out = append(out, field.Name)
	}
	return out
}

func TestCatalogCoversEveryRegisteredTask(t *testing.T) {
	catalog := wholeCatalog(t)

	if catalog.Version != CatalogVersion {
		t.Errorf("Version = %d; want %d", catalog.Version, CatalogVersion)
	}
	if len(catalog.Tasks) != len(RegisteredTasks) {
		t.Errorf("catalog has %d tasks; registry has %d", len(catalog.Tasks), len(RegisteredTasks))
	}

	seen := map[string]bool{}
	for i, schema := range catalog.Tasks {
		if _, ok := RegisteredTasks[schema.Type]; !ok {
			t.Errorf("catalog names %q, which is not registered", schema.Type)
		}
		if seen[schema.Type] {
			t.Errorf("catalog lists %q twice", schema.Type)
		}
		seen[schema.Type] = true
		if i > 0 && catalog.Tasks[i-1].Type >= schema.Type {
			t.Errorf("catalog is not sorted: %q precedes %q", catalog.Tasks[i-1].Type, schema.Type)
		}
	}
}

func TestCatalogEveryTaskHasSynopsisAndFields(t *testing.T) {
	for _, schema := range wholeCatalog(t).Tasks {
		if schema.Synopsis == "" {
			t.Errorf("task %q has an empty synopsis", schema.Type)
		}
		if len(schema.Fields) == 0 {
			t.Errorf("task %q has no fields", schema.Type)
		}
		if len(schema.Identity.Keys) == 0 {
			t.Errorf("task %q has no identity keys", schema.Type)
		}
		for _, field := range schema.Fields {
			if field.Name == "" {
				t.Errorf("task %q has a field with no name", schema.Type)
			}
		}
	}
}

// TestCatalogFieldTypesAreKnown is what makes the catalog's type vocabulary a
// closed set. The reflection falls back to TypeUnknown rather than to the Go
// type name, so adding a field of an exotic type to a task fails here instead
// of leaking `tasks.State`-style Go identifiers into the published JSON.
func TestCatalogFieldTypesAreKnown(t *testing.T) {
	fieldTypes := map[string]bool{
		TypeString: true, TypeBool: true, TypeInt: true, TypeFloat: true,
		TypeList: true, TypeDict: true, TypeAny: true,
	}
	elementTypes := map[string]bool{
		TypeString: true, TypeBool: true, TypeInt: true, TypeFloat: true,
		TypeList: true, TypeDict: true, TypeAny: true, TypeObject: true,
	}

	var check func(t *testing.T, where string, fields []FieldSchema)
	check = func(t *testing.T, where string, fields []FieldSchema) {
		for _, field := range fields {
			at := where + "." + field.Name
			if !fieldTypes[field.Type] {
				t.Errorf("%s has type %q, which is not a known field type", at, field.Type)
			}
			if field.Item == nil {
				continue
			}
			if !elementTypes[field.Item.Type] {
				t.Errorf("%s item has type %q, which is not a known element type", at, field.Item.Type)
			}
			if field.Item.KeyType != "" && field.Item.KeyType != TypeString {
				t.Errorf("%s item has key type %q; only %q is expected", at, field.Item.KeyType, TypeString)
			}
			check(t, at+"[]", field.Item.Fields)
		}
	}

	for _, schema := range wholeCatalog(t).Tasks {
		check(t, schema.Type, schema.Fields)
	}
}

// TestCatalogRequiredMatchesTags re-derives Required from the struct tags, so
// the catalog cannot drift from what the loader actually enforces: a field
// carrying a default is filled in before the zero-check runs, and so is never
// really required no matter what its required tag says.
func TestCatalogRequiredMatchesTags(t *testing.T) {
	catalog := wholeCatalog(t)
	for _, schema := range catalog.Tasks {
		rt := taskStructType(RegisteredTasks[schema.Type])
		for _, field := range schema.Fields {
			structField, ok := structFieldByYAMLName(rt, field.Name)
			if !ok {
				t.Errorf("%s.%s has no matching struct field", schema.Type, field.Name)
				continue
			}
			def := structField.Tag.Get("default")
			want := structField.Tag.Get("required") == "true" && def == ""
			if field.Required != want {
				t.Errorf("%s.%s Required = %v; want %v", schema.Type, field.Name, field.Required, want)
			}
			if field.Default != def {
				t.Errorf("%s.%s Default = %q; want %q", schema.Type, field.Name, field.Default, def)
			}
			if field.Required && field.Default != "" {
				t.Errorf("%s.%s is required and defaulted, which the loader cannot express", schema.Type, field.Name)
			}
		}
	}
}

// TestCatalogSensitiveMatchesTags covers the nested case too: a consumer that
// echoes a recipe has to know that a HttpAuthUser's password is a secret, and
// that fact lives on the element struct rather than on the task.
func TestCatalogSensitiveMatchesTags(t *testing.T) {
	catalog := wholeCatalog(t)

	var check func(t *testing.T, where string, rt reflect.Type, fields []FieldSchema)
	check = func(t *testing.T, where string, rt reflect.Type, fields []FieldSchema) {
		for _, field := range fields {
			structField, ok := structFieldByYAMLName(rt, field.Name)
			if !ok {
				continue
			}
			want := structField.Tag.Get("sensitive") == "true"
			if field.Sensitive != want {
				t.Errorf("%s.%s Sensitive = %v; want %v", where, field.Name, field.Sensitive, want)
			}
			if field.Item != nil && field.Item.Type == TypeObject {
				check(t, where+"."+field.Name+"[]", structElem(sliceOrMapElem(structField.Type)), field.Item.Fields)
			}
		}
	}

	for _, schema := range catalog.Tasks {
		check(t, schema.Type, taskStructType(RegisteredTasks[schema.Type]), schema.Fields)
	}

	// Pin the case the nesting exists for.
	user := fieldFor(t, schemaFor(t, catalog, "dokku_http_auth_user").Fields, "users")
	if user.Item == nil {
		t.Fatal("dokku_http_auth_user.users has no item schema")
	}
	for _, name := range []string{"password", "hash"} {
		if !fieldFor(t, user.Item.Fields, name).Sensitive {
			t.Errorf("dokku_http_auth_user.users[].%s must be sensitive", name)
		}
	}
}

// TestCatalogIdentityMatchesTaskIdentity checks the catalog against the same
// helpers the loader and the export filter read. The "every key is a field"
// half also guards the two yaml-name helpers staying in agreement: a key hidden
// behind `yaml:"-"` would be in Identity.Keys but absent from Fields.
func TestCatalogIdentityMatchesTaskIdentity(t *testing.T) {
	for _, schema := range wholeCatalog(t).Tasks {
		task := RegisteredTasks[schema.Type]

		if want := IdentityKeyNames(task); !reflect.DeepEqual(schema.Identity.Keys, want) {
			t.Errorf("%s Identity.Keys = %v; want %v", schema.Type, schema.Identity.Keys, want)
		}

		var want []IdentityCollectionSchema
		for _, collection := range TaskIdentityCollections(task) {
			want = append(want, IdentityCollectionSchema{
				Name:     collection.YAMLName,
				Item:     collection.Item,
				ItemKeys: collection.ItemKeys,
			})
		}
		if !reflect.DeepEqual(schema.Identity.Collections, want) {
			t.Errorf("%s Identity.Collections = %+v; want %+v", schema.Type, schema.Identity.Collections, want)
		}

		names := map[string]bool{}
		for _, field := range schema.Fields {
			names[field.Name] = true
		}
		for _, key := range schema.Identity.Keys {
			if !names[key] {
				t.Errorf("%s identity key %q is not a documented field", schema.Type, key)
			}
		}
	}
}

func TestCatalogSupportMatchesDeclarations(t *testing.T) {
	for _, schema := range wholeCatalog(t).Tasks {
		task := RegisteredTasks[schema.Type]
		if want, _ := TaskExportSupport(task); schema.Export != want {
			t.Errorf("%s Export = %+v; want %+v", schema.Type, schema.Export, want)
		}
		if want, _ := TaskProbeSupport(task); schema.Probe != want {
			t.Errorf("%s Probe = %+v; want %+v", schema.Type, schema.Probe, want)
		}
		if want := strings.TrimSpace(TaskDeprecation(task)); schema.Deprecation != want {
			t.Errorf("%s Deprecation = %q; want %q", schema.Type, schema.Deprecation, want)
		}
	}
}

// TestCatalogFieldShapes pins the shapes a bare type name cannot express: a
// list of structs, a dict of scalars, a dict of anything, and a *bool whose
// default documents a runtime coercion rather than a value the loader writes.
func TestCatalogFieldShapes(t *testing.T) {
	catalog := wholeCatalog(t)

	ports := fieldFor(t, schemaFor(t, catalog, "dokku_ports").Fields, "port_mappings")
	if ports.Type != TypeList || ports.Identity != IdentityRoleCollection {
		t.Errorf("port_mappings = {type %q, identity %q}; want {list, collection}", ports.Type, ports.Identity)
	}
	if ports.Item == nil || ports.Item.Type != TypeObject {
		t.Fatalf("port_mappings item = %+v; want an object", ports.Item)
	}
	if got := fieldNames(ports.Item.Fields); !reflect.DeepEqual(got, []string{"scheme", "host", "container"}) {
		t.Errorf("port_mappings item fields = %v", got)
	}
	if host := fieldFor(t, ports.Item.Fields, "host"); host.Type != TypeInt || host.Identity != IdentityRoleKey {
		t.Errorf("port_mappings item host = {type %q, identity %q}; want {int, key}", host.Type, host.Identity)
	}
	if ports.Item.Identity != ItemIdentityFields {
		t.Errorf("port_mappings item identity = %q; want %q", ports.Item.Identity, ItemIdentityFields)
	}
	if !reflect.DeepEqual(ports.Item.Keys, []string{"scheme", "host"}) {
		t.Errorf("port_mappings item keys = %v; want [scheme host]", ports.Item.Keys)
	}

	config := schemaFor(t, catalog, "dokku_config")
	cfg := fieldFor(t, config.Fields, "config")
	if cfg.Type != TypeDict || cfg.Item == nil || cfg.Item.KeyType != TypeString || cfg.Item.Type != TypeString {
		t.Errorf("config = {type %q, item %+v}; want a dict of string", cfg.Type, cfg.Item)
	}
	if cfg.Item.Identity != ItemIdentityMapKey {
		t.Errorf("config item identity = %q; want %q", cfg.Item.Identity, ItemIdentityMapKey)
	}
	restart := fieldFor(t, config.Fields, "restart")
	if restart.Type != TypeBool || restart.Required || restart.Default != "true" {
		t.Errorf("restart = {type %q, required %v, default %q}; want {bool, false, \"true\"}", restart.Type, restart.Required, restart.Default)
	}

	values := fieldFor(t, schemaFor(t, catalog, "dokku_scheduler_k3s_chart").Fields, "values")
	if values.Type != TypeDict || values.Item == nil || values.Item.Type != TypeAny {
		t.Errorf("values = {type %q, item %+v}; want a dict of any", values.Type, values.Item)
	}

	scale := fieldFor(t, schemaFor(t, catalog, "dokku_ps_scale").Fields, "scale")
	if scale.Type != TypeDict || scale.Item == nil || scale.Item.Type != TypeInt {
		t.Errorf("scale = {type %q, item %+v}; want a dict of int", scale.Type, scale.Item)
	}

	domains := fieldFor(t, schemaFor(t, catalog, "dokku_domains").Fields, "domains")
	if domains.Item == nil || domains.Item.Type != TypeString || domains.Item.Identity != ItemIdentityValue {
		t.Errorf("domains item = %+v; want a string identified by value", domains.Item)
	}

	// An untagged collection is an attribute of the resource, not the set of
	// items the task manages, so it declares no item identity.
	phases := fieldFor(t, schemaFor(t, catalog, "dokku_storage_mount").Fields, "phases")
	if phases.Identity != "" || phases.Item == nil || phases.Item.Identity != "" {
		t.Errorf("phases = {identity %q, item %+v}; want no identity", phases.Identity, phases.Item)
	}

	state := fieldFor(t, config.Fields, "state")
	if !reflect.DeepEqual(state.Choices, []string{"present", "absent"}) {
		t.Errorf("state choices = %v; want [present absent]", state.Choices)
	}
}

// TestCatalogPropertySchemaMatchesTable checks the published property list
// against the runtime table each task actually validates and probes with.
func TestCatalogPropertySchemaMatchesTable(t *testing.T) {
	for _, schema := range wholeCatalog(t).Tasks {
		table, declared := TaskPropertyTable(RegisteredTasks[schema.Type])
		if !declared {
			if schema.PropertySchema != nil {
				t.Errorf("%s publishes a property schema without declaring a table", schema.Type)
			}
			continue
		}
		if schema.PropertySchema == nil {
			t.Errorf("%s declares a property table but publishes no property schema", schema.Type)
			continue
		}

		published := schema.PropertySchema
		if published.Plugin != table.Plugin() {
			t.Errorf("%s plugin = %q; want %q", schema.Type, published.Plugin, table.Plugin())
		}
		if published.Subcommand != table.Subcommand {
			t.Errorf("%s subcommand = %q; want %q", schema.Type, published.Subcommand, table.Subcommand)
		}
		if published.Field != PropertyFieldName {
			t.Errorf("%s property field = %q; want %q", schema.Type, published.Field, PropertyFieldName)
		}

		var names []string
		for _, property := range published.Properties {
			names = append(names, property.Name)

			entry := table.Keys[property.Name]
			if property.AppReportKey != entry.PerApp || property.GlobalReportKey != entry.Global {
				t.Errorf("%s[%q] keys = (%q, %q); want (%q, %q)",
					schema.Type, property.Name, property.AppReportKey, property.GlobalReportKey, entry.PerApp, entry.Global)
			}
			if property.Sensitive != entry.Sensitive {
				t.Errorf("%s[%q] Sensitive = %v; want %v", schema.Type, property.Name, property.Sensitive, entry.Sensitive)
			}

			var wantScopes []string
			if entry.PerApp != "" {
				wantScopes = append(wantScopes, PropertyScopeApp)
			}
			if entry.Global != "" {
				wantScopes = append(wantScopes, PropertyScopeGlobal)
			}
			if !reflect.DeepEqual(property.Scopes, wantScopes) {
				t.Errorf("%s[%q] scopes = %v; want %v", schema.Type, property.Name, property.Scopes, wantScopes)
			}
		}

		want := make([]string, 0, len(table.Keys))
		for name := range table.Keys {
			want = append(want, name)
		}
		sort.Strings(want)
		if !reflect.DeepEqual(names, want) {
			t.Errorf("%s properties = %v; want %v", schema.Type, names, want)
		}

		if !reflect.DeepEqual(published.Dynamic, DynamicPropertyFamilies(table.Plugin())) {
			t.Errorf("%s dynamic = %+v; want %+v", schema.Type, published.Dynamic, DynamicPropertyFamilies(table.Plugin()))
		}

		if !reflect.DeepEqual(published.Rejected, RejectedPropertyFamilies(table)) {
			t.Errorf("%s rejected = %+v; want %+v", schema.Type, published.Rejected, RejectedPropertyFamilies(table))
		}

		// The table constrains a field that has to exist for the constraint
		// to mean anything.
		fieldFor(t, schema.Fields, published.Field)
	}
}

// TestCatalogPropertySchemaSpotChecks pins the two ends of the range: a table
// whose properties are split across scopes, and one with a dynamic family.
func TestCatalogPropertySchemaSpotChecks(t *testing.T) {
	catalog := wholeCatalog(t)

	apps := schemaFor(t, catalog, "dokku_apps_property").PropertySchema
	if apps == nil {
		t.Fatal("dokku_apps_property has no property schema")
	}
	if apps.Plugin != "apps" || apps.Subcommand != "apps:set" {
		t.Errorf("apps property schema = {%q, %q}; want {apps, apps:set}", apps.Plugin, apps.Subcommand)
	}
	if len(apps.Dynamic) != 0 {
		t.Errorf("apps has dynamic families %+v; want none", apps.Dynamic)
	}
	for _, tc := range []struct {
		name   string
		scopes []string
	}{
		{"deploy-source", []string{PropertyScopeApp}},
		{"disable-autocreation", []string{PropertyScopeGlobal}},
	} {
		found := false
		for _, property := range apps.Properties {
			if property.Name != tc.name {
				continue
			}
			found = true
			if !reflect.DeepEqual(property.Scopes, tc.scopes) {
				t.Errorf("apps %q scopes = %v; want %v", tc.name, property.Scopes, tc.scopes)
			}
		}
		if !found {
			t.Errorf("apps property schema is missing %q", tc.name)
		}
	}

	letsencrypt := schemaFor(t, catalog, "dokku_letsencrypt_property").PropertySchema
	if letsencrypt == nil {
		t.Fatal("dokku_letsencrypt_property has no property schema")
	}
	want := []DynamicPropertySchema{{
		Prefix:    "dns-provider-",
		Probeable: true,
		Sensitive: true,
		Scopes:    []string{PropertyScopeApp, PropertyScopeGlobal},
	}}
	if !reflect.DeepEqual(letsencrypt.Dynamic, want) {
		t.Errorf("letsencrypt dynamic = %+v; want %+v", letsencrypt.Dynamic, want)
	}

	// traefik holds the same credentials and reports them the same way as of
	// dokku 0.38.27 (#450), but `traefik:set` refuses the family outside
	// --global, so the catalog has to publish it as global-only or a consumer
	// validating offline accepts a recipe dokku will reject (#457, #458).
	traefik := schemaFor(t, catalog, "dokku_traefik_property").PropertySchema
	if traefik == nil {
		t.Fatal("dokku_traefik_property has no property schema")
	}
	want = []DynamicPropertySchema{{
		Prefix:    "dns-provider-",
		Probeable: true,
		Sensitive: true,
		Scopes:    []string{PropertyScopeGlobal},
	}}
	if !reflect.DeepEqual(traefik.Dynamic, want) {
		t.Errorf("traefik dynamic = %+v; want %+v", traefik.Dynamic, want)
	}

	// scheduler-k3s refuses the chart.* family outright. Publishing it is what
	// lets a consumer validating a recipe offline answer the way docket does -
	// with the task that owns the name - instead of reporting it as unknown and
	// offering a list it will never be in (#458).
	k3s := schemaFor(t, catalog, "dokku_scheduler_k3s_property").PropertySchema
	if k3s == nil {
		t.Fatal("dokku_scheduler_k3s_property has no property schema")
	}
	wantRejected := []RejectedPropertySchema{{
		Prefix:      "chart.",
		Replacement: "dokku_scheduler_k3s_chart",
		Reason:      "the scheduler-k3s:set path for chart values is deprecated in dokku",
	}}
	if !reflect.DeepEqual(k3s.Rejected, wantRejected) {
		t.Errorf("scheduler-k3s rejected = %+v; want %+v", k3s.Rejected, wantRejected)
	}
	for _, property := range k3s.Properties {
		if strings.HasPrefix(property.Name, "chart.") {
			t.Errorf("scheduler-k3s publishes %q as supported and rejected at once", property.Name)
		}
	}
	if _, ok := RegisteredTasks[wantRejected[0].Replacement]; !ok {
		t.Errorf("scheduler-k3s points at %q, which is not a registered task", wantRejected[0].Replacement)
	}

	// A table with nothing to refuse omits the key rather than publishing an
	// empty list, the same way Dynamic does.
	if len(apps.Rejected) != 0 {
		t.Errorf("apps has rejected families %+v; want none", apps.Rejected)
	}

	// A free-form property field is not a property table, and must not be
	// published as an empty one.
	if service := schemaFor(t, catalog, "dokku_service_property"); service.PropertySchema != nil {
		t.Errorf("dokku_service_property publishes %+v; its names are not enumerable", service.PropertySchema)
	}
}

// TestCatalogEncodeIsStable guards the ordering: RegisteredTasks is a map, and
// a consumer diffing two catalogs (or a test comparing two runs) needs the
// bytes to depend only on the code.
func TestCatalogEncodeIsStable(t *testing.T) {
	var first, second bytes.Buffer
	for _, buf := range []*bytes.Buffer{&first, &second} {
		catalog, err := Catalog()
		if err != nil {
			t.Fatalf("Catalog(): %v", err)
		}
		if err := catalog.Encode(buf); err != nil {
			t.Fatalf("Encode: %v", err)
		}
	}
	if first.String() != second.String() {
		t.Error("two catalog builds encoded differently")
	}
	if !strings.HasSuffix(first.String(), "\n") {
		t.Error("encoded catalog does not end with a newline")
	}
}

// TestCatalogEncodeDoesNotEscapeHTML keeps a description mentioning a
// `dokku <plugin>:report` command readable instead of rendering it as
// <plugin>.
func TestCatalogEncodeDoesNotEscapeHTML(t *testing.T) {
	catalog := TaskCatalog{
		Version: CatalogVersion,
		Tasks: []TaskSchema{{
			Type:     "dokku_example",
			Synopsis: "Reads dokku <plugin>:report & reports drift",
		}},
	}

	var buf bytes.Buffer
	if err := catalog.Encode(&buf); err != nil {
		t.Fatalf("Encode: %v", err)
	}
	for _, escape := range []string{`\u003c`, `\u003e`, `\u0026`} {
		if strings.Contains(buf.String(), escape) {
			t.Errorf("encoded catalog contains the HTML escape %s:\n%s", escape, buf.String())
		}
	}
	if !strings.Contains(buf.String(), "dokku <plugin>:report & reports drift") {
		t.Errorf("encoded catalog lost its content:\n%s", buf.String())
	}
}

// TestCatalogSkipsUnserializedFields proves the catalog asks "is this a key a
// recipe can set", not "what would this field be called" - the distinction
// between catalogYAMLName and yamlFieldName.
func TestCatalogSkipsUnserializedFields(t *testing.T) {
	type example struct {
		App    string `yaml:"app"`
		Hidden string `yaml:"-"`
		lower  string //nolint:unused // unexported fields are never recipe keys
	}

	got := fieldNames(buildFields(reflect.TypeOf(example{})))
	if !reflect.DeepEqual(got, []string{"app"}) {
		t.Errorf("fields = %v; want [app]", got)
	}
}

// structFieldByYAMLName finds the struct field a recipe key came from.
func structFieldByYAMLName(rt reflect.Type, name string) (reflect.StructField, bool) {
	if rt == nil || rt.Kind() != reflect.Struct {
		return reflect.StructField{}, false
	}
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if field.PkgPath == "" && catalogYAMLName(field.Tag, field.Name) == name {
			return field, true
		}
	}
	return reflect.StructField{}, false
}

// sliceOrMapElem returns a collection field's element type.
func sliceOrMapElem(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return t.Elem()
	}
	return t
}

// catalogTypes returns the type keys a catalog carries, in document order.
func catalogTypes(catalog TaskCatalog) []string {
	out := make([]string, 0, len(catalog.Tasks))
	for _, schema := range catalog.Tasks {
		out = append(out, schema.Type)
	}
	return out
}

// TestCatalogForSelectsNamedTasks covers the narrowing #459 added: only the
// named types come back, and they come back sorted whatever order they were
// asked for, so a selection is byte-stable and diffs against the full catalog.
func TestCatalogForSelectsNamedTasks(t *testing.T) {
	catalog, err := CatalogFor([]string{"dokku_domains", "dokku_config"})
	if err != nil {
		t.Fatalf("CatalogFor: %v", err)
	}
	if catalog.Version != CatalogVersion {
		t.Errorf("Version = %d; want %d", catalog.Version, CatalogVersion)
	}
	want := []string{"dokku_config", "dokku_domains"}
	if got := catalogTypes(catalog); !reflect.DeepEqual(got, want) {
		t.Errorf("types = %v; want %v", got, want)
	}
}

// TestCatalogForWithNoSelectionMatchesCatalog pins that Catalog() is the same
// code path, so the whole catalog cannot drift from a narrowed one.
func TestCatalogForWithNoSelectionMatchesCatalog(t *testing.T) {
	full := wholeCatalog(t)
	for _, selection := range [][]string{nil, {}} {
		catalog, err := CatalogFor(selection)
		if err != nil {
			t.Fatalf("CatalogFor(%v): %v", selection, err)
		}
		if !reflect.DeepEqual(catalog, full) {
			t.Errorf("CatalogFor(%v) does not match Catalog()", selection)
		}
	}
}

// TestCatalogForDedupesRepeatedTypes: the document is a set keyed by type, so
// naming one twice - a shell loop, say - emits it once rather than failing.
func TestCatalogForDedupesRepeatedTypes(t *testing.T) {
	catalog, err := CatalogFor([]string{"dokku_config", "dokku_config", "dokku_config"})
	if err != nil {
		t.Fatalf("CatalogFor: %v", err)
	}
	if got := catalogTypes(catalog); !reflect.DeepEqual(got, []string{"dokku_config"}) {
		t.Errorf("types = %v; want [dokku_config]", got)
	}
}

// TestCatalogForRejectsUnknownType: an unregistered key is an error rather
// than a silently smaller document, which would look like a complete answer.
func TestCatalogForRejectsUnknownType(t *testing.T) {
	catalog, err := CatalogFor([]string{"dokku_config", "dokku_confg"})
	if err == nil {
		t.Fatal("expected an error for an unregistered task type")
	}
	if !strings.Contains(err.Error(), `unknown task type "dokku_confg"`) {
		t.Errorf("err = %q; want it to name the unknown type", err)
	}
	if !reflect.DeepEqual(catalog, TaskCatalog{}) {
		t.Errorf("catalog = %+v; want the zero value on error", catalog)
	}
}

// TestCatalogForEntriesMatchTheFullCatalog proves narrowing is a projection,
// not a re-derivation: a consumer reading one task out of a narrowed catalog
// sees exactly what it would have seen in the whole one.
func TestCatalogForEntriesMatchTheFullCatalog(t *testing.T) {
	full := wholeCatalog(t)
	selection := []string{"dokku_config", "dokku_domains", "dokku_apps_property"}
	catalog, err := CatalogFor(selection)
	if err != nil {
		t.Fatalf("CatalogFor: %v", err)
	}
	for _, schema := range catalog.Tasks {
		if want := schemaFor(t, full, schema.Type); !reflect.DeepEqual(schema, want) {
			t.Errorf("narrowed entry for %q differs from the full catalog's", schema.Type)
		}
	}
}

// TestRegisteredTaskNamesIsSortedAndComplete guards the helper the catalog,
// the --task validator and the shell completion all read from.
func TestRegisteredTaskNamesIsSortedAndComplete(t *testing.T) {
	names := RegisteredTaskNames()
	if len(names) != len(RegisteredTasks) {
		t.Errorf("got %d names; registry has %d", len(names), len(RegisteredTasks))
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("names are not sorted: %v", names)
	}
	for _, name := range names {
		if _, ok := RegisteredTasks[name]; !ok {
			t.Errorf("name %q is not in the registry", name)
		}
	}

	// The result is a copy, so a caller may sort or truncate it in place.
	if len(names) > 0 {
		names[0] = "mutated"
		if again := RegisteredTaskNames(); again[0] == "mutated" {
			t.Error("RegisteredTaskNames returned a slice aliasing shared state")
		}
	}
}
