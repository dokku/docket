package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// The JSON-lines streams are hand-written schemas under docs/schemas/
// rather than reflected types, because every event is built as a
// map[string]interface{} in output_json.go / list_tasks.go /
// validate.go. These helpers close that gap: every test that decodes an
// emitted event validates it against the published schema first, so a
// new or renamed field cannot ship without the schema (and therefore
// docs/json-output.md and docs/ansible-dokku.md) being updated to match.
//
// Every schema sets "additionalProperties": false, so an undocumented
// field fails loudly instead of being silently tolerated.
const (
	eventsSchemaPath    = "../docs/schemas/events-v1.schema.json"
	listTasksSchemaPath = "../docs/schemas/list-tasks-v1.schema.json"
	validateSchemaPath  = "../docs/schemas/validate-v1.schema.json"
)

var (
	schemaMu    sync.Mutex
	schemaCache = map[string]*jsonschema.Schema{}
)

// loadSchema compiles the schema at path once per process.
func loadSchema(t *testing.T, path string) *jsonschema.Schema {
	t.Helper()
	schemaMu.Lock()
	defer schemaMu.Unlock()
	if s, ok := schemaCache[path]; ok {
		return s
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("resolve %s: %v", path, err)
	}
	f, err := os.Open(abs)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	doc, err := jsonschema.UnmarshalJSON(f)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(abs, doc); err != nil {
		t.Fatalf("add %s: %v", path, err)
	}
	s, err := c.Compile(abs)
	if err != nil {
		t.Fatalf("compile %s: %v", path, err)
	}
	schemaCache[path] = s
	return s
}

// assertMatchesSchema validates one raw JSON-lines event against the
// schema at path.
func assertMatchesSchema(t *testing.T, path, line string) {
	t.Helper()
	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(line))
	if err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %q", err, line)
	}
	if err := loadSchema(t, path).Validate(inst); err != nil {
		t.Errorf("event does not match %s: %v\nraw: %s", filepath.Base(path), err, line)
	}
}

// assertLinesMatchSchema validates every non-empty line of out.
func assertLinesMatchSchema(t *testing.T, path, out string) {
	t.Helper()
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		assertMatchesSchema(t, path, line)
	}
}

// TestEventsSchemaRejectsUnknownField is a self-check on the guard: if
// the schema ever stopped forbidding extra properties, every other
// conformance assertion in this package would quietly become a no-op.
func TestEventsSchemaRejectsUnknownField(t *testing.T) {
	e, ui := emitterTestUI()
	e.PlayStart("tasks", "")

	var ev map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimRight(ui.OutputWriter.String(), "\n")), &ev); err != nil {
		t.Fatalf("decode play_start: %v", err)
	}
	ev["not_a_real_field"] = "x"
	raw, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if err := loadSchema(t, eventsSchemaPath).Validate(inst); err == nil {
		t.Error("expected an unknown field to fail validation; schema must set additionalProperties: false")
	}
}
