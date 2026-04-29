package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dokku/docket/tasks"
	flag "github.com/spf13/pflag"
)

func TestIsTrueString(t *testing.T) {
	trueValues := []string{"true", "yes", "on", "y", "Y"}
	for _, v := range trueValues {
		if !isTrueString(v) {
			t.Errorf("isTrueString(%q) = false, want true", v)
		}
	}

	falseValues := []string{"false", "no", "off", "n", "N", "", "maybe", "1", "0"}
	for _, v := range falseValues {
		if isTrueString(v) {
			t.Errorf("isTrueString(%q) = true, want false", v)
		}
	}
}

func TestIsFalseString(t *testing.T) {
	falseValues := []string{"false", "no", "off", "n", "N"}
	for _, v := range falseValues {
		if !isFalseString(v) {
			t.Errorf("isFalseString(%q) = false, want true", v)
		}
	}

	trueValues := []string{"true", "yes", "on", "y", "Y", "", "maybe", "1", "0"}
	for _, v := range trueValues {
		if isFalseString(v) {
			t.Errorf("isFalseString(%q) = true, want false", v)
		}
	}
}

func TestIsTrueAndFalseAreMutuallyExclusive(t *testing.T) {
	allValues := []string{"true", "false", "yes", "no", "on", "off", "y", "n", "Y", "N", "", "maybe"}
	for _, v := range allValues {
		isTrue := isTrueString(v)
		isFalse := isFalseString(v)
		if isTrue && isFalse {
			t.Errorf("value %q is both true and false", v)
		}
	}
}

func TestGetTaskYamlFilename(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected string
	}{
		{
			name:     "default when no --tasks flag",
			args:     []string{"docket"},
			expected: "tasks.yml",
		},
		{
			name:     "separate --tasks flag",
			args:     []string{"docket", "--tasks", "custom.yml"},
			expected: "custom.yml",
		},
		{
			name:     "equals --tasks=flag",
			args:     []string{"docket", "--tasks=custom.yml"},
			expected: "custom.yml",
		},
		{
			name:     "--tasks at end with no value",
			args:     []string{"docket", "--tasks"},
			expected: "tasks.yml",
		},
		{
			name:     "--tasks with other flags before",
			args:     []string{"docket", "--app", "myapp", "--tasks", "other.yml"},
			expected: "other.yml",
		},
		{
			name:     "uses passed parameter not os.Args",
			args:     []string{"anything", "--tasks", "from-param.yml"},
			expected: "from-param.yml",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getTaskYamlFilename(tt.args)
			if result != tt.expected {
				t.Errorf("getTaskYamlFilename(%v) = %q, want %q", tt.args, result, tt.expected)
			}
		})
	}
}

func TestArgumentGetValue(t *testing.T) {
	t.Run("bool value", func(t *testing.T) {
		b := true
		arg := Argument{}
		arg.SetBoolValue(&b)
		if arg.GetValue() == nil {
			t.Error("GetValue() returned nil for bool argument")
		}
		if !arg.HasValue() {
			t.Error("HasValue() returned false for bool argument")
		}
	})

	t.Run("int value", func(t *testing.T) {
		i := 42
		arg := Argument{}
		arg.SetIntValue(&i)
		if arg.GetValue() == nil {
			t.Error("GetValue() returned nil for int argument")
		}
		if !arg.HasValue() {
			t.Error("HasValue() returned false for int argument")
		}
	})

	t.Run("float value", func(t *testing.T) {
		f := 3.14
		arg := Argument{}
		arg.SetFloatValue(&f)
		if arg.GetValue() == nil {
			t.Error("GetValue() returned nil for float argument")
		}
		if !arg.HasValue() {
			t.Error("HasValue() returned false for float argument")
		}
	})

	t.Run("string value non-empty", func(t *testing.T) {
		s := "hello"
		arg := Argument{}
		arg.SetStringValue(&s)
		if arg.GetValue() == nil {
			t.Error("GetValue() returned nil for non-empty string argument")
		}
		if !arg.HasValue() {
			t.Error("HasValue() returned false for non-empty string argument")
		}
	})

	t.Run("string value empty", func(t *testing.T) {
		s := ""
		arg := Argument{}
		arg.SetStringValue(&s)
		if arg.GetValue() != nil {
			t.Error("GetValue() returned non-nil for empty string argument")
		}
		if arg.HasValue() {
			t.Error("HasValue() returned true for empty string argument")
		}
	})

	t.Run("no value set", func(t *testing.T) {
		arg := Argument{}
		if arg.GetValue() != nil {
			t.Error("GetValue() returned non-nil for unset argument")
		}
		if arg.HasValue() {
			t.Error("HasValue() returned true for unset argument")
		}
	})
}

func TestParseInputDocumentJSON5(t *testing.T) {
	data := []byte(`[
  {
    inputs: [
      // primary input
      { name: "app", default: "myapp", description: "Application name", required: true, type: "string" },
      { name: "port", default: "8080", description: "Port number", type: "int" },
    ],
    tasks: [],
  },
]
`)
	inputs, err := parseInputDocument(data, tasks.FormatNameJSON5)
	if err != nil {
		t.Fatalf("parseInputDocument failed: %v", err)
	}
	if len(inputs) != 2 {
		t.Fatalf("expected 2 inputs, got %d", len(inputs))
	}
	if app, ok := inputs["app"]; !ok || app.Default != "myapp" || !app.Required || app.Type != "string" {
		t.Errorf("'app' input mismatched: %+v ok=%v", app, ok)
	}
	if port, ok := inputs["port"]; !ok || port.Default != "8080" || port.Type != "int" {
		t.Errorf("'port' input mismatched: %+v ok=%v", port, ok)
	}
}

func TestGetInputVariablesJSON5(t *testing.T) {
	data := []byte(`[
  {
    inputs: [
      { name: "app", default: "myapp" },
    ],
    tasks: [],
  },
]`)
	inputs, err := getInputVariables(data, tasks.FormatNameJSON5)
	if err != nil {
		t.Fatalf("getInputVariables: %v", err)
	}
	if app, ok := inputs["app"]; !ok || app.Default != "myapp" {
		t.Errorf("'app' input mismatched: %+v ok=%v", app, ok)
	}
}

func TestParseInputYamlValidInputs(t *testing.T) {
	data := []byte(`---
- inputs:
    - name: app
      default: "myapp"
      description: "Application name"
      required: true
      type: string
    - name: port
      default: "8080"
      description: "Port number"
      type: int
  tasks: []
`)
	inputs, err := parseInputYaml(data)
	if err != nil {
		t.Fatalf("parseInputYaml failed: %v", err)
	}

	if len(inputs) != 2 {
		t.Fatalf("expected 2 inputs, got %d", len(inputs))
	}

	app, ok := inputs["app"]
	if !ok {
		t.Fatal("expected 'app' input")
	}
	if app.Default != "myapp" {
		t.Errorf("app.Default = %q, want %q", app.Default, "myapp")
	}
	if !app.Required {
		t.Error("app.Required = false, want true")
	}
	if app.Type != "string" {
		t.Errorf("app.Type = %q, want %q", app.Type, "string")
	}

	port, ok := inputs["port"]
	if !ok {
		t.Fatal("expected 'port' input")
	}
	if port.Default != "8080" {
		t.Errorf("port.Default = %q, want %q", port.Default, "8080")
	}
	if port.Type != "int" {
		t.Errorf("port.Type = %q, want %q", port.Type, "int")
	}
}

func TestParseInputYamlNoInputs(t *testing.T) {
	data := []byte("---\n- tasks: []\n")
	inputs, err := parseInputYaml(data)
	if err != nil {
		t.Fatalf("parseInputYaml failed: %v", err)
	}
	if len(inputs) != 0 {
		t.Errorf("expected 0 inputs, got %d", len(inputs))
	}
}

func TestParseInputYamlInvalidYaml(t *testing.T) {
	data := []byte("not valid yaml: [[[")
	_, err := parseInputYaml(data)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestParseInputYamlAllTypes(t *testing.T) {
	data := []byte(`---
- inputs:
    - name: str_input
      type: string
      default: "hello"
    - name: int_input
      type: int
      default: "42"
    - name: float_input
      type: float
      default: "3.14"
    - name: bool_input
      type: bool
      default: "true"
  tasks: []
`)
	inputs, err := parseInputYaml(data)
	if err != nil {
		t.Fatalf("parseInputYaml failed: %v", err)
	}

	tests := []struct {
		name     string
		wantType string
	}{
		{"str_input", "string"},
		{"int_input", "int"},
		{"float_input", "float"},
		{"bool_input", "bool"},
	}

	for _, tt := range tests {
		input, ok := inputs[tt.name]
		if !ok {
			t.Errorf("expected input %q", tt.name)
			continue
		}
		if input.Type != tt.wantType {
			t.Errorf("input %q type = %q, want %q", tt.name, input.Type, tt.wantType)
		}
	}
}

func TestParseInputYamlMultipleRecipes(t *testing.T) {
	data := []byte(`---
- inputs:
    - name: first
      default: "a"
  tasks: []
- inputs:
    - name: second
      default: "b"
  tasks: []
`)
	inputs, err := parseInputYaml(data)
	if err != nil {
		t.Fatalf("parseInputYaml failed: %v", err)
	}

	if _, ok := inputs["first"]; !ok {
		t.Error("expected 'first' input from first recipe section")
	}
	if _, ok := inputs["second"]; !ok {
		t.Error("expected 'second' input from second recipe section")
	}
}

func TestGetInputVariablesValid(t *testing.T) {
	data := []byte(`---
- inputs:
    - name: app
      default: "myapp"
      description: "App name"
      required: true
  tasks: []
`)
	inputs, err := getInputVariables(data, tasks.FormatYAML)
	if err != nil {
		t.Fatalf("getInputVariables failed: %v", err)
	}

	app, ok := inputs["app"]
	if !ok {
		t.Fatal("expected 'app' input")
	}
	if app.Default != "myapp" {
		t.Errorf("app.Default = %q, want %q", app.Default, "myapp")
	}
	if !app.Required {
		t.Error("app.Required = false, want true")
	}
}

func TestGetInputVariablesTemplateError(t *testing.T) {
	data := []byte(`---
- inputs:
    - name: {{ .broken
  tasks: []
`)
	_, err := getInputVariables(data, tasks.FormatYAML)
	if err == nil {
		t.Fatal("expected error for bad template syntax")
	}
	if !strings.Contains(err.Error(), "sigil error") {
		t.Errorf("expected 'sigil error', got: %v", err)
	}
}

func TestInputSetValueAndGetValue(t *testing.T) {
	input := tasks.Input{}
	err := input.SetValue("hello")
	if err != nil {
		t.Fatalf("SetValue failed: %v", err)
	}
	if input.GetValue() != "hello" {
		t.Errorf("GetValue() = %q, want %q", input.GetValue(), "hello")
	}
	if !input.HasValue() {
		t.Error("HasValue() = false, want true")
	}
}

func TestInputHasValueEmpty(t *testing.T) {
	input := tasks.Input{}
	if input.HasValue() {
		t.Error("HasValue() = true for unset input, want false")
	}
	if input.GetValue() != "" {
		t.Errorf("GetValue() = %q for unset input, want empty", input.GetValue())
	}
}

func TestInputSetValueOverwrite(t *testing.T) {
	input := tasks.Input{}
	input.SetValue("first")
	input.SetValue("second")
	if input.GetValue() != "second" {
		t.Errorf("GetValue() = %q after overwrite, want %q", input.GetValue(), "second")
	}
}

// writeTempFile is a small helper for the vars-file tests so each test reads
// like a single declarative block instead of three lines of plumbing.
func writeTempFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestLoadVarsFilesYAML(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "vars.yml", "app: api\nreplicas: 3\ndebug: true\n")

	merged, sources, err := loadVarsFiles([]string{path})
	if err != nil {
		t.Fatalf("loadVarsFiles failed: %v", err)
	}
	if merged["app"] != "api" {
		t.Errorf("merged[app] = %v, want %q", merged["app"], "api")
	}
	if merged["replicas"] != 3 {
		t.Errorf("merged[replicas] = %v (%T), want int 3", merged["replicas"], merged["replicas"])
	}
	if merged["debug"] != true {
		t.Errorf("merged[debug] = %v, want true", merged["debug"])
	}
	if sources["app"] != path {
		t.Errorf("sources[app] = %q, want %q", sources["app"], path)
	}
}

func TestLoadVarsFilesJSON(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "vars.json", `{"app":"api","replicas":3,"debug":false}`)

	merged, _, err := loadVarsFiles([]string{path})
	if err != nil {
		t.Fatalf("loadVarsFiles failed: %v", err)
	}
	if merged["app"] != "api" {
		t.Errorf("merged[app] = %v, want %q", merged["app"], "api")
	}
	// JSON numbers always decode as float64; coercion to int happens later
	// inside SetFromVarsFile when the input declares type: int.
	if merged["replicas"] != float64(3) {
		t.Errorf("merged[replicas] = %v (%T), want float64 3", merged["replicas"], merged["replicas"])
	}
	if merged["debug"] != false {
		t.Errorf("merged[debug] = %v, want false", merged["debug"])
	}
}

func TestLoadVarsFilesMissingFile(t *testing.T) {
	_, _, err := loadVarsFiles([]string{"/nonexistent/path/vars.yml"})
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "/nonexistent/path/vars.yml") {
		t.Errorf("error should mention path, got: %v", err)
	}
}

func TestLoadVarsFilesNonMappingError(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "list.yml", "- one\n- two\n")

	_, _, err := loadVarsFiles([]string{path})
	if err == nil {
		t.Fatal("expected error for top-level list")
	}
	if !strings.Contains(err.Error(), "mapping") {
		t.Errorf("error should mention mapping, got: %v", err)
	}
}

func TestLoadVarsFilesMultiFileLastWins(t *testing.T) {
	dir := t.TempDir()
	a := writeTempFile(t, dir, "a.yml", "app: from-a\nshared: from-a\n")
	b := writeTempFile(t, dir, "b.yml", "shared: from-b\nextra: only-b\n")

	merged, sources, err := loadVarsFiles([]string{a, b})
	if err != nil {
		t.Fatalf("loadVarsFiles failed: %v", err)
	}
	if merged["shared"] != "from-b" {
		t.Errorf("merged[shared] = %v, want %q (later file wins)", merged["shared"], "from-b")
	}
	if merged["app"] != "from-a" {
		t.Errorf("merged[app] = %v, want %q (only in a)", merged["app"], "from-a")
	}
	if merged["extra"] != "only-b" {
		t.Errorf("merged[extra] = %v, want %q (only in b)", merged["extra"], "only-b")
	}
	if sources["shared"] != b {
		t.Errorf("sources[shared] = %q, want %q (last writer)", sources["shared"], b)
	}
	if sources["app"] != a {
		t.Errorf("sources[app] = %q, want %q (originating file)", sources["app"], a)
	}
}

func TestLoadVarsFilesEmptyYAML(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "empty.yml", "")

	merged, _, err := loadVarsFiles([]string{path})
	if err != nil {
		t.Fatalf("loadVarsFiles on empty file failed: %v", err)
	}
	if len(merged) != 0 {
		t.Errorf("expected empty map, got %v", merged)
	}
}

// argFor builds an Argument with a typed pointer wired up the same way
// registerInputFlags would. The default value is what the equivalent CLI
// flag would have absent any user input. Tests use this to exercise
// SetFromVarsFile and applyVarsFiles without rebuilding a FlagSet.
func argFor(t *testing.T, declared string, def interface{}) *Argument {
	t.Helper()
	arg := &Argument{Type: declared}
	switch declared {
	case "string":
		s, _ := def.(string)
		arg.SetStringValue(&s)
	case "int":
		i, _ := def.(int)
		arg.SetIntValue(&i)
	case "float":
		f, _ := def.(float64)
		arg.SetFloatValue(&f)
	case "bool":
		b, _ := def.(bool)
		arg.SetBoolValue(&b)
	default:
		t.Fatalf("argFor: unknown type %q", declared)
	}
	return arg
}

func TestSetFromVarsFileString(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  string
	}{
		{"plain string", "hello", "hello"},
		{"int coerces to numeric string", 42, "42"},
		{"float coerces to numeric string", 3.14, "3.14"},
		{"bool coerces to literal", true, "true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arg := argFor(t, "string", "")
			if err := arg.SetFromVarsFile("k", tt.value); err != nil {
				t.Fatalf("SetFromVarsFile failed: %v", err)
			}
			if got := *arg.stringValue; got != tt.want {
				t.Errorf("stringValue = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSetFromVarsFileInt(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		want    int
		wantErr bool
	}{
		{"native int", 7, 7, false},
		{"int64", int64(9), 9, false},
		{"whole float", float64(11), 11, false},
		{"non-whole float rejected", 1.5, 0, true},
		{"numeric string", "42", 42, false},
		{"non-numeric string rejected", "abc", 0, true},
		{"bool rejected", true, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arg := argFor(t, "int", 0)
			err := arg.SetFromVarsFile("k", tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %v", tt.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("SetFromVarsFile failed: %v", err)
			}
			if got := *arg.intValue; got != tt.want {
				t.Errorf("intValue = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestSetFromVarsFileFloat(t *testing.T) {
	arg := argFor(t, "float", 0.0)
	if err := arg.SetFromVarsFile("k", "2.5"); err != nil {
		t.Fatalf("string coerce failed: %v", err)
	}
	if *arg.floatValue != 2.5 {
		t.Errorf("floatValue = %v, want 2.5", *arg.floatValue)
	}

	arg = argFor(t, "float", 0.0)
	if err := arg.SetFromVarsFile("k", 7); err != nil {
		t.Fatalf("int coerce failed: %v", err)
	}
	if *arg.floatValue != 7.0 {
		t.Errorf("floatValue = %v, want 7.0", *arg.floatValue)
	}

	arg = argFor(t, "float", 0.0)
	if err := arg.SetFromVarsFile("k", "not-a-number"); err == nil {
		t.Fatal("expected error for non-numeric string")
	}
}

func TestSetFromVarsFileBool(t *testing.T) {
	tests := []struct {
		name    string
		value   interface{}
		want    bool
		wantErr bool
	}{
		{"native true", true, true, false},
		{"native false", false, false, false},
		{"string true", "true", true, false},
		{"string yes", "yes", true, false},
		{"string on", "on", true, false},
		{"string false", "false", false, false},
		{"string no", "no", false, false},
		{"int rejected", 1, false, true},
		{"unrelated string rejected", "banana", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arg := argFor(t, "bool", false)
			err := arg.SetFromVarsFile("k", tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %v", tt.value)
				}
				return
			}
			if err != nil {
				t.Fatalf("SetFromVarsFile failed: %v", err)
			}
			if got := *arg.boolValue; got != tt.want {
				t.Errorf("boolValue = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyVarsFilesEmptyPathsNoOp(t *testing.T) {
	args := map[string]*Argument{"app": argFor(t, "string", "default")}
	flags := flag.NewFlagSet("t", flag.ContinueOnError)
	if _, err := applyVarsFiles(args, flags, nil); err != nil {
		t.Fatalf("expected nil error for empty paths, got %v", err)
	}
	if *args["app"].stringValue != "default" {
		t.Errorf("stringValue mutated to %q despite no vars files", *args["app"].stringValue)
	}
}

func TestApplyVarsFilesUpdatesUnsetArgument(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "vars.yml", "app: from-vars\n")

	args := map[string]*Argument{"app": argFor(t, "string", "default")}
	flags := flag.NewFlagSet("t", flag.ContinueOnError)
	flags.String("app", "default", "")

	if _, err := applyVarsFiles(args, flags, []string{path}); err != nil {
		t.Fatalf("applyVarsFiles failed: %v", err)
	}
	if got := *args["app"].stringValue; got != "from-vars" {
		t.Errorf("stringValue = %q, want %q", got, "from-vars")
	}
}

func TestApplyVarsFilesCLIOverridesVarsFile(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "vars.yml", "app: from-vars\n")

	args := map[string]*Argument{"app": argFor(t, "string", "from-cli")}
	flags := flag.NewFlagSet("t", flag.ContinueOnError)
	flags.String("app", "default", "")
	// Simulate "user typed --app=from-cli" by parsing it; pflag.Visit will
	// then report `app` as Changed and applyVarsFiles must respect that.
	if err := flags.Parse([]string{"--app", "from-cli"}); err != nil {
		t.Fatalf("flags.Parse failed: %v", err)
	}

	if _, err := applyVarsFiles(args, flags, []string{path}); err != nil {
		t.Fatalf("applyVarsFiles failed: %v", err)
	}
	if got := *args["app"].stringValue; got != "from-cli" {
		t.Errorf("stringValue = %q, want %q (CLI must win)", got, "from-cli")
	}
}

func TestApplyVarsFilesUnknownKey(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "vars.yml", "appp: typo\n")

	args := map[string]*Argument{"app": argFor(t, "string", "")}
	flags := flag.NewFlagSet("t", flag.ContinueOnError)

	_, err := applyVarsFiles(args, flags, []string{path})
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
	for _, want := range []string{`unknown input "appp"`, path, `did you mean "app"`} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q\nfull: %v", want, err)
		}
	}
}

func TestApplyVarsFilesUnknownKeyNoSuggestionWhenFar(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "vars.yml", "totallyunrelated: x\n")

	args := map[string]*Argument{"app": argFor(t, "string", "")}
	flags := flag.NewFlagSet("t", flag.ContinueOnError)

	_, err := applyVarsFiles(args, flags, []string{path})
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
	if strings.Contains(err.Error(), "did you mean") {
		t.Errorf("did-you-mean should suppress for far edit distance, got: %v", err)
	}
}

func TestApplyVarsFilesMultiFileLastWins(t *testing.T) {
	dir := t.TempDir()
	a := writeTempFile(t, dir, "a.yml", "app: from-a\n")
	b := writeTempFile(t, dir, "b.yml", "app: from-b\n")

	args := map[string]*Argument{"app": argFor(t, "string", "")}
	flags := flag.NewFlagSet("t", flag.ContinueOnError)

	if _, err := applyVarsFiles(args, flags, []string{a, b}); err != nil {
		t.Fatalf("applyVarsFiles failed: %v", err)
	}
	if got := *args["app"].stringValue; got != "from-b" {
		t.Errorf("stringValue = %q, want %q (last file wins)", got, "from-b")
	}
}

func TestApplyVarsFilesCoercionFailureNamesInput(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "vars.yml", "replicas: not-a-number\n")

	args := map[string]*Argument{"replicas": argFor(t, "int", 0)}
	flags := flag.NewFlagSet("t", flag.ContinueOnError)

	_, err := applyVarsFiles(args, flags, []string{path})
	if err == nil {
		t.Fatal("expected coercion error")
	}
	for _, want := range []string{`replicas`, path} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q\nfull: %v", want, err)
		}
	}
}

func TestApplyVarsFilesJSONFloatCoercesToInt(t *testing.T) {
	dir := t.TempDir()
	path := writeTempFile(t, dir, "vars.json", `{"replicas": 5}`)

	args := map[string]*Argument{"replicas": argFor(t, "int", 0)}
	flags := flag.NewFlagSet("t", flag.ContinueOnError)

	if _, err := applyVarsFiles(args, flags, []string{path}); err != nil {
		t.Fatalf("applyVarsFiles failed: %v", err)
	}
	if got := *args["replicas"].intValue; got != 5 {
		t.Errorf("intValue = %d, want 5 (JSON float64 5.0 → int)", got)
	}
}

func TestNearestInputNameSuggestion(t *testing.T) {
	names := []string{"app", "repo", "replicas"}
	tests := []struct {
		candidate string
		want      string
	}{
		{"appp", "app"},
		{"replics", "replicas"},
		{"banana", ""}, // distance > 2 → no suggestion
	}
	for _, tt := range tests {
		t.Run(tt.candidate, func(t *testing.T) {
			if got := nearestInputName(tt.candidate, names); got != tt.want {
				t.Errorf("nearestInputName(%q) = %q, want %q", tt.candidate, got, tt.want)
			}
		})
	}
}
