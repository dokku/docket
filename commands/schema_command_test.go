package commands

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/tasks"

	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/mitchellh/cli"
	"github.com/posener/complete"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

// runSchema drives the command and returns its stdout, the Ui's stderr, and
// the exit code. The catalog goes to os.Stdout rather than through the Ui, so
// the capture has to happen at the file descriptor.
func runSchema(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	c := &SchemaCommand{}
	c.Meta = command.Meta{Ui: cli.NewMockUi()}
	out, exit := captureStdout(t, func(w io.Writer) int {
		c.Stdout = w
		return c.Run(args)
	})
	return out, c.Ui.(*cli.MockUi).ErrorWriter.String(), exit
}

// decodeCatalog parses an emitted catalog into a generic document.
func decodeCatalog(t *testing.T, out string) map[string]interface{} {
	t.Helper()
	var doc map[string]interface{}
	if err := json.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("decode catalog: %v\nraw: %s", err, out)
	}
	return doc
}

// catalogTasks returns the emitted task entries keyed by type.
func catalogTasks(t *testing.T, doc map[string]interface{}) map[string]map[string]interface{} {
	t.Helper()
	list, ok := doc["tasks"].([]interface{})
	if !ok {
		t.Fatalf("catalog has no tasks array: %v", doc["tasks"])
	}
	out := map[string]map[string]interface{}{}
	for _, entry := range list {
		task, ok := entry.(map[string]interface{})
		if !ok {
			t.Fatalf("task entry is not an object: %v", entry)
		}
		name, _ := task["type"].(string)
		out[name] = task
	}
	return out
}

func TestSchemaCommandMetadata(t *testing.T) {
	c := &SchemaCommand{}
	if c.Name() != "schema" {
		t.Errorf("Name = %q, want %q", c.Name(), "schema")
	}
	if c.Synopsis() == "" {
		t.Error("Synopsis must not be empty")
	}
}

func TestSchemaCommandExamples(t *testing.T) {
	c := &SchemaCommand{}
	examples := c.Examples()
	if len(examples) == 0 {
		t.Fatal("expected at least one example")
	}
	for label, example := range examples {
		if example == "" {
			t.Errorf("example %q is empty", label)
		}
	}
}

func TestSchemaCommandHelpDoesNotPanic(t *testing.T) {
	c := &SchemaCommand{}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("FlagSet panicked: %v", r)
		}
	}()
	_ = c.FlagSet()
}

func TestSchemaCommandEmitsEveryRegisteredTask(t *testing.T) {
	out, stderr, exit := runSchema(t)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", exit, stderr)
	}

	doc := decodeCatalog(t, out)
	if version, _ := doc["version"].(float64); int(version) != tasks.CatalogVersion {
		t.Errorf("version = %v; want %d", doc["version"], tasks.CatalogVersion)
	}

	emitted := catalogTasks(t, doc)
	if len(emitted) != len(tasks.RegisteredTasks) {
		t.Errorf("catalog has %d tasks; registry has %d", len(emitted), len(tasks.RegisteredTasks))
	}
	for name := range tasks.RegisteredTasks {
		if _, ok := emitted[name]; !ok {
			t.Errorf("catalog is missing task %q", name)
		}
	}
}

// TestSchemaCommandMatchesSchema validates the real emitted document against
// the published JSON Schema, so a field that exists in the code but not in the
// schema fails CI - the same contract the three JSON-lines streams have.
func TestSchemaCommandMatchesSchema(t *testing.T) {
	out, stderr, exit := runSchema(t)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", exit, stderr)
	}
	assertMatchesSchema(t, taskCatalogSchemaPath, out)
}

// TestTaskCatalogSchemaRejectsUnknownField is the self-check on the guard: if
// the schema stopped forbidding extra properties, TestSchemaCommandMatchesSchema
// would quietly become a no-op.
func TestTaskCatalogSchemaRejectsUnknownField(t *testing.T) {
	out, _, exit := runSchema(t)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}

	doc := decodeCatalog(t, out)
	list, _ := doc["tasks"].([]interface{})
	if len(list) == 0 {
		t.Fatal("catalog has no tasks to corrupt")
	}
	first, _ := list[0].(map[string]interface{})
	first["not_a_real_field"] = "x"

	raw, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := loadSchema(t, taskCatalogSchemaPath).Validate(inst); err == nil {
		t.Error("expected an unknown field to fail validation; schema must set additionalProperties: false")
	}
}

// TestSchemaCommandPublishesPropertyTables covers the half of the catalog the
// reference pages could not express before: which names a property task's
// free-form-looking `property` field actually accepts.
func TestSchemaCommandPublishesPropertyTables(t *testing.T) {
	out, _, exit := runSchema(t)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	emitted := catalogTasks(t, decodeCatalog(t, out))

	apps, ok := emitted["dokku_apps_property"]["property_schema"].(map[string]interface{})
	if !ok {
		t.Fatalf("dokku_apps_property has no property_schema: %v", emitted["dokku_apps_property"])
	}
	if apps["plugin"] != "apps" || apps["subcommand"] != "apps:set" || apps["field"] != "property" {
		t.Errorf("apps property_schema = %v", apps)
	}

	properties, _ := apps["properties"].([]interface{})
	scopes := map[string][]interface{}{}
	for _, entry := range properties {
		property, _ := entry.(map[string]interface{})
		name, _ := property["name"].(string)
		scopes[name], _ = property["scopes"].([]interface{})
	}
	if got := scopes["disable-autocreation"]; len(got) != 1 || got[0] != "global" {
		t.Errorf("disable-autocreation scopes = %v; want [global]", got)
	}
	if got := scopes["deploy-source"]; len(got) != 1 || got[0] != "app" {
		t.Errorf("deploy-source scopes = %v; want [app]", got)
	}

	// A free-form property field must not be published as an empty table: the
	// difference between "no legal names" and "docket cannot enumerate them"
	// is the whole point.
	if _, ok := emitted["dokku_service_property"]["property_schema"]; ok {
		t.Error("dokku_service_property must not publish a property_schema")
	}
}

// TestSchemaCommandMarksSensitiveFields guards the fact a consumer most needs
// from the catalog: which values it must not echo.
func TestSchemaCommandMarksSensitiveFields(t *testing.T) {
	out, _, exit := runSchema(t)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	emitted := catalogTasks(t, decodeCatalog(t, out))

	fields, _ := emitted["dokku_git_from_image"]["fields"].([]interface{})
	found := false
	for _, entry := range fields {
		field, _ := entry.(map[string]interface{})
		if field["name"] != "image" {
			continue
		}
		found = true
		if field["sensitive"] != true {
			t.Errorf("dokku_git_from_image.image sensitive = %v; want true", field["sensitive"])
		}
	}
	if !found {
		t.Error("dokku_git_from_image has no image field")
	}
}

// TestSchemaCommandIncludesExamples checks the catalog carries the snippets the
// reference pages publish, so a consumer does not have to scrape the markdown
// to get them.
func TestSchemaCommandIncludesExamples(t *testing.T) {
	out, _, exit := runSchema(t)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}

	for name, task := range catalogTasks(t, decodeCatalog(t, out)) {
		examples, _ := task["examples"].([]interface{})
		if len(examples) == 0 {
			t.Errorf("task %q publishes no examples", name)
			continue
		}
		for _, entry := range examples {
			example, _ := entry.(map[string]interface{})
			if example["name"] == "" || example["yaml"] == "" {
				t.Errorf("task %q has an empty example: %v", name, example)
			}
			if body, _ := example["yaml"].(string); !strings.Contains(body, name+":") {
				t.Errorf("task %q example does not name the task type: %q", name, body)
			}
		}
	}
}

// TestSchemaCommandOutputIsStable pins that two runs of the same binary agree,
// so a consumer can diff catalogs across versions and see only real changes.
func TestSchemaCommandOutputIsStable(t *testing.T) {
	first, _, _ := runSchema(t)
	second, _, _ := runSchema(t)
	if first != second {
		t.Error("two runs of docket schema produced different output")
	}
}

func TestSchemaCommandWritesToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")

	out, stderr, exit := runSchema(t, "--output", path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", exit, stderr)
	}
	if out != "" {
		t.Errorf("--output wrote %d bytes to stdout; want none", len(out))
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	stdout, _, _ := runSchema(t)
	if string(written) != stdout {
		t.Error("the file and the stream disagree")
	}
}

func TestSchemaCommandRejectsUnknownFlag(t *testing.T) {
	_, stderr, exit := runSchema(t, "--nope")
	if exit != 1 {
		t.Errorf("exit = %d, want 1", exit)
	}
	if stderr == "" {
		t.Error("expected an error message on stderr")
	}
}

func TestSchemaCommandRejectsPositionalArgument(t *testing.T) {
	_, stderr, exit := runSchema(t, "dokku_app")
	if exit != 1 {
		t.Errorf("exit = %d, want 1", exit)
	}
	if !strings.Contains(stderr, "takes no arguments") {
		t.Errorf("stderr = %q; want it to explain the command takes no arguments", stderr)
	}
}

func TestSchemaCommandReportsAnUnwritableOutput(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing-dir", "catalog.json")

	_, stderr, exit := runSchema(t, "--output", path)
	if exit != 1 {
		t.Errorf("exit = %d, want 1", exit)
	}
	if !strings.Contains(stderr, "write error") {
		t.Errorf("stderr = %q; want a write error", stderr)
	}
}

// catalogTypeList returns the emitted type keys in document order, so a test
// can assert both membership and ordering.
func catalogTypeList(t *testing.T, doc map[string]interface{}) []string {
	t.Helper()
	list, ok := doc["tasks"].([]interface{})
	if !ok {
		t.Fatalf("catalog has no tasks array: %v", doc["tasks"])
	}
	out := make([]string, 0, len(list))
	for _, entry := range list {
		task, ok := entry.(map[string]interface{})
		if !ok {
			t.Fatalf("task entry is not an object: %v", entry)
		}
		name, _ := task["type"].(string)
		out = append(out, name)
	}
	return out
}

// TestSchemaCommandFiltersToRequestedTaskTypes is the headline of #459: --task
// narrows the catalog to the named types and nothing else.
func TestSchemaCommandFiltersToRequestedTaskTypes(t *testing.T) {
	out, stderr, exit := runSchema(t, "--task", "dokku_config", "--task", "dokku_domains")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", exit, stderr)
	}

	doc := decodeCatalog(t, out)
	if version, _ := doc["version"].(float64); int(version) != tasks.CatalogVersion {
		t.Errorf("version = %v; want %d", doc["version"], tasks.CatalogVersion)
	}
	want := []string{"dokku_config", "dokku_domains"}
	if got := catalogTypeList(t, doc); !reflect.DeepEqual(got, want) {
		t.Errorf("types = %v; want %v", got, want)
	}
}

// TestSchemaCommandFilteredOutputMatchesSchema is the other half of the issue's
// contract: a narrowed catalog is the same document shape, so the published
// JSON Schema still covers it and a consumer parses one format either way.
func TestSchemaCommandFilteredOutputMatchesSchema(t *testing.T) {
	out, stderr, exit := runSchema(t, "--task", "dokku_config")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", exit, stderr)
	}
	assertMatchesSchema(t, taskCatalogSchemaPath, out)
}

// TestSchemaCommandFilterIgnoresFlagOrder keeps the documented stability
// contract true for a narrowed run: tasks are sorted by type, so the bytes do
// not depend on the order the flags were typed.
func TestSchemaCommandFilterIgnoresFlagOrder(t *testing.T) {
	forward, _, _ := runSchema(t, "--task", "dokku_config", "--task", "dokku_domains")
	reverse, _, _ := runSchema(t, "--task", "dokku_domains", "--task", "dokku_config")
	if forward != reverse {
		t.Error("reversing the --task flags changed the output")
	}
}

// TestSchemaCommandFilterDedupesRepeatedTypes: the document is a set keyed by
// type, so naming one twice emits it once rather than failing.
func TestSchemaCommandFilterDedupesRepeatedTypes(t *testing.T) {
	out, _, exit := runSchema(t, "--task", "dokku_config", "--task", "dokku_config")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	if got := catalogTypeList(t, decodeCatalog(t, out)); !reflect.DeepEqual(got, []string{"dokku_config"}) {
		t.Errorf("types = %v; want [dokku_config]", got)
	}
}

// TestSchemaCommandFilteredEntryMatchesFullCatalog pins that --task changes
// which entries are emitted and nothing about any entry, so it is purely
// additive for a consumer that already reads the whole catalog.
func TestSchemaCommandFilteredEntryMatchesFullCatalog(t *testing.T) {
	narrowed, _, exit := runSchema(t, "--task", "dokku_config")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	full, _, _ := runSchema(t)

	got := catalogTasks(t, decodeCatalog(t, narrowed))["dokku_config"]
	want := catalogTasks(t, decodeCatalog(t, full))["dokku_config"]
	if !reflect.DeepEqual(got, want) {
		t.Error("the narrowed dokku_config entry differs from the full catalog's")
	}
}

// TestSchemaCommandRejectsUnknownTaskType: an unknown type exits non-zero and
// names the closest match, the way an unknown task type in a recipe address
// already does. Emitting an empty tasks array would read as "docket has no
// such task", which is exactly the wrong answer for a typo.
func TestSchemaCommandRejectsUnknownTaskType(t *testing.T) {
	out, stderr, exit := runSchema(t, "--task", "dokku_confg")
	if exit != 1 {
		t.Errorf("exit = %d, want 1", exit)
	}
	if !strings.Contains(stderr, `unknown task type "dokku_confg"`) {
		t.Errorf("stderr = %q; want it to name the unknown type", stderr)
	}
	if !strings.Contains(stderr, `did you mean "dokku_config"`) {
		t.Errorf("stderr = %q; want a did-you-mean hint", stderr)
	}
	if out != "" {
		t.Errorf("wrote %d bytes to stdout; want none", len(out))
	}
}

func TestSchemaCommandUnknownTaskTypeWithoutNearMatch(t *testing.T) {
	_, stderr, exit := runSchema(t, "--task", "nonsense")
	if exit != 1 {
		t.Errorf("exit = %d, want 1", exit)
	}
	if !strings.Contains(stderr, `unknown task type "nonsense"`) {
		t.Errorf("stderr = %q; want it to name the unknown type", stderr)
	}
	if strings.Contains(stderr, "did you mean") {
		t.Errorf("stderr = %q; want no hint when nothing is close", stderr)
	}
}

func TestSchemaCommandRejectsEmptyTaskType(t *testing.T) {
	_, stderr, exit := runSchema(t, "--task=")
	if exit != 1 {
		t.Errorf("exit = %d, want 1", exit)
	}
	if !strings.Contains(stderr, "a task type is required") {
		t.Errorf("stderr = %q; want the empty-value message", stderr)
	}
}

// TestSchemaCommandTaskTypeFilterIsCaseSensitive pins the decision: type keys
// are case-sensitive at every other lookup, so --task does not case-fold.
func TestSchemaCommandTaskTypeFilterIsCaseSensitive(t *testing.T) {
	_, stderr, exit := runSchema(t, "--task", "DOKKU_CONFIG")
	if exit != 1 {
		t.Errorf("exit = %d, want 1", exit)
	}
	if !strings.Contains(stderr, `unknown task type "DOKKU_CONFIG"`) {
		t.Errorf("stderr = %q; want the unknown-type message", stderr)
	}
}

func TestSchemaCommandFilterWritesToFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")

	out, stderr, exit := runSchema(t, "--task", "dokku_config", "--output", path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", exit, stderr)
	}
	if out != "" {
		t.Errorf("--output wrote %d bytes to stdout; want none", len(out))
	}

	written, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	stdout, _, _ := runSchema(t, "--task", "dokku_config")
	if string(written) != stdout {
		t.Error("the file and the stream disagree")
	}
}

// TestSchemaCommandUnknownTaskTypeWritesNoFile is why the filter is validated
// before anything is built: a typo must not leave a file behind.
func TestSchemaCommandUnknownTaskTypeWritesNoFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.json")

	_, _, exit := runSchema(t, "--task", "nonsense", "--output", path)
	if exit != 1 {
		t.Errorf("exit = %d, want 1", exit)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("os.Stat(%s) = %v; want the file not to exist", path, err)
	}
}

func TestSchemaCommandAutocompletesTaskTypes(t *testing.T) {
	c := &SchemaCommand{}
	predictor, ok := c.AutocompleteFlags()["--task"]
	if !ok || predictor == nil {
		t.Fatal("--task has no completion predictor")
	}
	if got := predictor.Predict(complete.Args{}); !reflect.DeepEqual(got, tasks.RegisteredTaskNames()) {
		t.Errorf("--task predicts %v; want every registered task type", got)
	}
}

// TestSchemaCommandPositionalTaskTypeSuggestsTheFlag: a bare task type is the
// mistake --task invites, so the rejection points at the flag.
func TestSchemaCommandPositionalTaskTypeSuggestsTheFlag(t *testing.T) {
	_, stderr, exit := runSchema(t, "dokku_app")
	if exit != 1 {
		t.Errorf("exit = %d, want 1", exit)
	}
	if !strings.Contains(stderr, "did you mean --task dokku_app?") {
		t.Errorf("stderr = %q; want it to point at --task", stderr)
	}
}
