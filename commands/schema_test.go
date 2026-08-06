package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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

// jsonLines splits a captured JSON-lines stream into its non-empty
// lines. Shared by every helper that walks an emitted stream so the
// "trim trailing newline, skip blanks" rule lives in one place.
func jsonLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		lines = append(lines, line)
	}
	return lines
}

// assertLinesMatchSchema validates every non-empty line of out.
func assertLinesMatchSchema(t *testing.T, path, out string) {
	t.Helper()
	for _, line := range jsonLines(out) {
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

// problemCodeLiteral matches the `Code: "..."` field of a tasks.Problem
// composite literal. The leading \b keeps it off `ExitCode:` - there is
// no word boundary between "t" and "C".
var problemCodeLiteral = regexp.MustCompile(`\bCode:\s*"([a-z0-9_]+)"`)

// problemCodeCall matches the problem("code", ...) / addProblem("code",
// ...) closures tasks/parse.go records structural findings through.
var problemCodeCall = regexp.MustCompile(`\b(?:add)?[Pp]roblem\("([a-z0-9_]+)"`)

// TestValidateSchemaCodeEnumCoversEmittedCodes keeps the `code` enum in
// docs/schemas/validate-v1.schema.json in sync with the codes the source
// actually emits. additionalProperties: false only guards field *names*;
// `code` is a value, so nothing else catches a new problem category that
// never reaches the published schema (and therefore fails validation in
// a wrapper that trusts it). The scan reads string literals, so a code
// routed through a variable would be missed - keep them literal.
func TestValidateSchemaCodeEnumCoversEmittedCodes(t *testing.T) {
	emitted := map[string]bool{}
	for _, dir := range []string{".", "../tasks"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read %s: %v", dir, err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				t.Fatalf("read %s/%s: %v", dir, name, err)
			}
			for _, re := range []*regexp.Regexp{problemCodeLiteral, problemCodeCall} {
				for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
					emitted[m[1]] = true
				}
			}
		}
	}
	if len(emitted) == 0 {
		t.Fatal("found no problem codes in the source; the scan patterns have drifted")
	}

	raw, err := os.ReadFile(validateSchemaPath)
	if err != nil {
		t.Fatalf("read %s: %v", validateSchemaPath, err)
	}
	var schema struct {
		Properties struct {
			Code struct {
				Enum []string `json:"enum"`
			} `json:"code"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatalf("parse %s: %v", validateSchemaPath, err)
	}
	listed := map[string]bool{}
	for _, code := range schema.Properties.Code.Enum {
		listed[code] = true
	}

	var missing, stale []string
	for code := range emitted {
		if !listed[code] {
			missing = append(missing, code)
		}
	}
	for code := range listed {
		if !emitted[code] {
			stale = append(stale, code)
		}
	}
	sort.Strings(missing)
	sort.Strings(stale)

	if len(missing) > 0 {
		t.Errorf("%s omits %d emitted code(s): %s\nAdd each to the code enum and to the table in docs/json-output.md.",
			validateSchemaPath, len(missing), strings.Join(missing, ", "))
	}
	if len(stale) > 0 {
		t.Errorf("%s lists %d code(s) nothing emits: %s\nDrop them, or fix the typo.",
			validateSchemaPath, len(stale), strings.Join(stale, ", "))
	}
}
