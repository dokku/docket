package sdk_test

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dokku/docket/sdk"
)

// These tests live in package sdk_test so they use the package the way a
// caller outside the module does: only through what sdk exports.

func TestNewTaskAppliesDefaults(t *testing.T) {
	task, err := sdk.NewTask("dokku_app")
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	app, ok := task.(*sdk.AppTask)
	if !ok {
		t.Fatalf("NewTask(dokku_app) = %T, want *sdk.AppTask", task)
	}
	if app.State != sdk.StatePresent {
		t.Errorf("State = %q, want %q", app.State, sdk.StatePresent)
	}

	if _, err := sdk.NewTask("dokku_nope"); err == nil {
		t.Errorf("NewTask on an unregistered type succeeded")
	}
}

func TestDecodeTask(t *testing.T) {
	task, err := sdk.DecodeTask("dokku_config", []byte("app: api\nconfig:\n  KEY: value\n"))
	if err != nil {
		t.Fatalf("DecodeTask: %v", err)
	}
	cfg, ok := task.(*sdk.ConfigTask)
	if !ok {
		t.Fatalf("DecodeTask(dokku_config) = %T, want *sdk.ConfigTask", task)
	}
	if cfg.App != "api" || cfg.Config["KEY"] != "value" {
		t.Errorf("decoded %+v, want app api and KEY=value", cfg)
	}
	if cfg.State != sdk.StatePresent {
		t.Errorf("State = %q, want the default %q", cfg.State, sdk.StatePresent)
	}
}

func TestAs(t *testing.T) {
	exported := sdk.ExportedTask{Type: "dokku_config", Body: sdk.ConfigTask{App: "api"}}

	cfg, ok := sdk.As[sdk.ConfigTask](exported)
	if !ok || cfg.App != "api" {
		t.Errorf("As[ConfigTask] = %+v, %v; want the body", cfg, ok)
	}
	if _, ok := sdk.As[sdk.AppTask](exported); ok {
		t.Errorf("As[AppTask] matched a dokku_config body")
	}
	if _, ok := sdk.As[*sdk.ConfigTask](exported); ok {
		t.Errorf("As[*ConfigTask] matched; bodies are values, never pointers")
	}
}

func TestIdentityAddressRoundTrip(t *testing.T) {
	task, err := sdk.NewTask("dokku_config")
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	task.(*sdk.ConfigTask).App = "api"

	address := sdk.IdentityAddress("dokku_config", task)
	if address != "dokku_config[app=api]" {
		t.Fatalf("IdentityAddress = %q, want dokku_config[app=api]", address)
	}
	typeKey, keys, err := sdk.ParseIdentityAddress(address)
	if err != nil {
		t.Fatalf("ParseIdentityAddress: %v", err)
	}
	if typeKey != "dokku_config" || !reflect.DeepEqual(keys, map[string]string{"app": "api"}) {
		t.Errorf("ParseIdentityAddress = %q, %v", typeKey, keys)
	}
}

func TestParseResourceSelectors(t *testing.T) {
	sel, err := sdk.ParseResourceSelectors([]string{"dokku_config[app=api]"})
	if err != nil {
		t.Fatalf("ParseResourceSelectors: %v", err)
	}
	want := []sdk.ResourceSelector{{Address: "dokku_config[app=api]", TypeKey: "dokku_config", Keys: map[string]string{"app": "api"}}}
	if !reflect.DeepEqual(sel, want) {
		t.Errorf("ParseResourceSelectors = %+v, want %+v", sel, want)
	}

	if _, err := sdk.ParseResourceSelectors([]string{"dokku_config[application=api]"}); err == nil {
		t.Errorf("an address naming an undeclared key parsed")
	}
}

func TestTaskTypesLookup(t *testing.T) {
	types := sdk.TaskTypes()
	if len(types) == 0 {
		t.Fatal("TaskTypes returned nothing")
	}
	if !sort.StringsAreSorted(types) {
		t.Errorf("TaskTypes is not sorted")
	}
	for _, typeKey := range types {
		if _, ok := sdk.Lookup(typeKey); !ok {
			t.Errorf("Lookup(%q) failed for a listed type", typeKey)
		}
	}
	if _, ok := sdk.Lookup("dokku_nope"); ok {
		t.Errorf("Lookup on an unregistered type succeeded")
	}
}

// TestMetadataHelpersMatchCatalog asserts the per-task helpers and the catalog
// describe every task the same way, so a caller can use either.
func TestMetadataHelpersMatchCatalog(t *testing.T) {
	catalog, err := sdk.Catalog()
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	if catalog.Version != sdk.CatalogVersion {
		t.Errorf("Version = %d, want %d", catalog.Version, sdk.CatalogVersion)
	}
	if len(catalog.Tasks) != len(sdk.TaskTypes()) {
		t.Errorf("catalog has %d tasks, TaskTypes has %d", len(catalog.Tasks), len(sdk.TaskTypes()))
	}

	for _, schema := range catalog.Tasks {
		task, ok := sdk.Lookup(schema.Type)
		if !ok {
			t.Errorf("%s: not registered", schema.Type)
			continue
		}
		if got := strings.TrimSpace(sdk.TaskSynopsis(task)); got != schema.Synopsis {
			t.Errorf("%s: TaskSynopsis = %q, catalog = %q", schema.Type, got, schema.Synopsis)
		}
		if got := strings.TrimSpace(sdk.TaskDeprecation(task)); got != schema.Deprecation {
			t.Errorf("%s: TaskDeprecation = %q, catalog = %q", schema.Type, got, schema.Deprecation)
		}
		if got, _ := sdk.TaskProbeSupport(task); got != schema.Probe {
			t.Errorf("%s: TaskProbeSupport = %+v, catalog = %+v", schema.Type, got, schema.Probe)
		}
		if got, _ := sdk.TaskExportSupport(task); got != schema.Export {
			t.Errorf("%s: TaskExportSupport = %+v, catalog = %+v", schema.Type, got, schema.Export)
		}
	}
}

func TestCatalogFor(t *testing.T) {
	catalog, err := sdk.CatalogFor([]string{"dokku_config", "dokku_app"})
	if err != nil {
		t.Fatalf("CatalogFor: %v", err)
	}
	var got []string
	for _, schema := range catalog.Tasks {
		got = append(got, schema.Type)
	}
	if want := []string{"dokku_app", "dokku_config"}; !reflect.DeepEqual(got, want) {
		t.Errorf("CatalogFor = %v, want %v", got, want)
	}

	if _, err := sdk.CatalogFor([]string{"dokku_nope"}); err == nil {
		t.Errorf("CatalogFor on an unregistered type succeeded")
	}
}

func TestSessionCloseIsIdempotent(t *testing.T) {
	session := sdk.NewSession(sdk.NewMasker("s3cr3t"))
	if err := session.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestMasker(t *testing.T) {
	masker := sdk.NewMasker("s3cr3t")
	if got, want := masker.String("token=s3cr3t"), "token="+sdk.MaskPlaceholder; got != want {
		t.Errorf("String = %q, want %q", got, want)
	}
}
