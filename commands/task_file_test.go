package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/posener/complete"
)

func TestDetectTaskFileFormat(t *testing.T) {
	cases := map[string]string{
		"tasks.yml":         taskFileFormatYAML,
		"tasks.yaml":        taskFileFormatYAML,
		"tasks.YML":         taskFileFormatYAML,
		"tasks.json":        taskFileFormatJSON5,
		"tasks.JSON":        taskFileFormatJSON5,
		"tasks.json5":       taskFileFormatJSON5,
		"path/to/tasks.yml": taskFileFormatYAML,
		"recipe.txt":        taskFileFormatYAML,
		"":                  taskFileFormatYAML,
		// stdin has no extension; detection defaults to YAML here and
		// the caller sniffs instead. See taskFileFormatFor.
		taskFileStdin: taskFileFormatYAML,
	}
	for path, want := range cases {
		if got := detectTaskFileFormat(path); got != want {
			t.Errorf("detectTaskFileFormat(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestParseRecipeFormatFlag(t *testing.T) {
	valid := map[string]string{
		"":      "",
		"yaml":  taskFileFormatYAML,
		"YAML":  taskFileFormatYAML,
		"yml":   taskFileFormatYAML,
		"json":  taskFileFormatJSON5,
		"json5": taskFileFormatJSON5,
		"JSON5": taskFileFormatJSON5,
		" yaml": taskFileFormatYAML,
	}
	for value, want := range valid {
		got, err := parseRecipeFormatFlag("--tasks-format", value)
		if err != nil {
			t.Errorf("parseRecipeFormatFlag(%q) returned error: %v", value, err)
			continue
		}
		if got != want {
			t.Errorf("parseRecipeFormatFlag(%q) = %q, want %q", value, got, want)
		}
	}

	for _, value := range []string{"toml", "hcl", "ini", "yamlish"} {
		if _, err := parseRecipeFormatFlag("--tasks-format", value); err == nil {
			t.Errorf("parseRecipeFormatFlag(%q) = nil error, want a rejection", value)
		} else if !strings.Contains(err.Error(), "yaml, json5") {
			t.Errorf("parseRecipeFormatFlag(%q) error %q should name the valid values", value, err)
		}
	}

	// The flag name in the message follows the caller, so --format and
	// --tasks-format each blame themselves rather than the other.
	for _, flagName := range []string{"--tasks-format", "--format"} {
		_, err := parseRecipeFormatFlag(flagName, "toml")
		if err == nil {
			t.Fatalf("parseRecipeFormatFlag(%q, toml) = nil error, want a rejection", flagName)
		}
		if !strings.Contains(err.Error(), flagName) {
			t.Errorf("error %q should name the flag it rejects (%s)", err, flagName)
		}
	}
}

// TestTaskFileFormatFor pins the precedence rule: an explicit
// --tasks-format beats extension detection, which beats a content
// sniff. The return value must never be empty - tasks.IsJSON5Format("")
// is false, so an empty format silently means YAML downstream.
func TestTaskFileFormatFor(t *testing.T) {
	jsonish := []byte("[{tasks: []}]")
	yamlish := []byte("---\n- tasks: []\n")

	tests := []struct {
		name     string
		detected string
		override string
		data     []byte
		want     string
	}{
		{name: "override beats detection", detected: taskFileFormatYAML, override: taskFileFormatJSON5, data: yamlish, want: taskFileFormatJSON5},
		{name: "override beats sniff", detected: "", override: taskFileFormatYAML, data: jsonish, want: taskFileFormatYAML},
		{name: "detection beats sniff", detected: taskFileFormatYAML, data: jsonish, want: taskFileFormatYAML},
		{name: "sniff picks json5 for stdin", detected: "", data: jsonish, want: taskFileFormatJSON5},
		{name: "sniff picks yaml for stdin", detected: "", data: yamlish, want: taskFileFormatYAML},
		{name: "empty stdin defaults to yaml", detected: "", data: nil, want: taskFileFormatYAML},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := taskFileFormatFor(tt.detected, tt.override, tt.data); got != tt.want {
				t.Errorf("taskFileFormatFor(%q, %q, %q) = %q, want %q", tt.detected, tt.override, tt.data, got, tt.want)
			}
		})
	}
}

// TestResolveRecipeOutput pins the output-side precedence rule (#410):
// an explicit --format beats the --output extension, which beats the
// YAML fallback stdout is left with. It also pins the one case where the
// format changes the path: --format json5 with an untouched --output
// moves the default to tasks.json rather than writing a JSON5 document
// into a file named tasks.yml.
func TestResolveRecipeOutput(t *testing.T) {
	tests := []struct {
		name          string
		output        string
		override      string
		outputChanged bool
		wantPath      string
		wantFormat    string
	}{
		{name: "default with no override", output: defaultRecipeOutput, wantPath: defaultRecipeOutput, wantFormat: taskFileFormatYAML},
		{name: "default with json5 moves the path", output: defaultRecipeOutput, override: taskFileFormatJSON5, wantPath: defaultRecipeOutputJSON5, wantFormat: taskFileFormatJSON5},
		{name: "default with yaml stays put", output: defaultRecipeOutput, override: taskFileFormatYAML, wantPath: defaultRecipeOutput, wantFormat: taskFileFormatYAML},
		{name: "explicit path is never rewritten", output: "deploy/prod.yml", override: taskFileFormatJSON5, outputChanged: true, wantPath: "deploy/prod.yml", wantFormat: taskFileFormatJSON5},
		{name: "explicit default path is never rewritten", output: defaultRecipeOutput, override: taskFileFormatJSON5, outputChanged: true, wantPath: defaultRecipeOutput, wantFormat: taskFileFormatJSON5},
		{name: "override beats a json extension", output: "tasks.json", override: taskFileFormatYAML, outputChanged: true, wantPath: "tasks.json", wantFormat: taskFileFormatYAML},
		{name: "extension decides with no override", output: "tasks.json5", outputChanged: true, wantPath: "tasks.json5", wantFormat: taskFileFormatJSON5},
		{name: "unknown extension falls back to yaml", output: "recipe.txt", outputChanged: true, wantPath: "recipe.txt", wantFormat: taskFileFormatYAML},
		{name: "stdin with no override is yaml", output: taskFileStdin, outputChanged: true, wantPath: taskFileStdin, wantFormat: taskFileFormatYAML},
		{name: "stdin honours the override", output: taskFileStdin, override: taskFileFormatJSON5, outputChanged: true, wantPath: taskFileStdin, wantFormat: taskFileFormatJSON5},
		{name: "stdin is never rewritten to a path", output: taskFileStdin, override: taskFileFormatJSON5, wantPath: taskFileStdin, wantFormat: taskFileFormatJSON5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, gotFormat := resolveRecipeOutput(tt.output, tt.override, tt.outputChanged)
			if gotPath != tt.wantPath || gotFormat != tt.wantFormat {
				t.Errorf("resolveRecipeOutput(%q, %q, %t) = (%q, %q), want (%q, %q)",
					tt.output, tt.override, tt.outputChanged, gotPath, gotFormat, tt.wantPath, tt.wantFormat)
			}
		})
	}
}

// TestResolveRecipeOutputDefaultsAreProbeCandidates is what makes the
// path swap safe: init tells the user to run a bare `docket validate`
// next, and that probes defaultTaskFileCandidates. A default output that
// is not in that list would leave the scaffold unreachable.
func TestResolveRecipeOutputDefaultsAreProbeCandidates(t *testing.T) {
	for _, want := range []string{defaultRecipeOutput, defaultRecipeOutputJSON5} {
		found := false
		for _, candidate := range defaultTaskFileCandidates {
			if candidate == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("%q is a default --output but not in defaultTaskFileCandidates %v", want, defaultTaskFileCandidates)
		}
	}
}

// TestRecipeOutputFormatMismatch pins when the extension-lies warning
// fires. Only a recipe whose extension disagrees with --format earns
// one; stdout has no extension to disagree with, and no --format means
// the extension was the source of truth in the first place.
func TestRecipeOutputFormatMismatch(t *testing.T) {
	quiet := [][2]string{
		{"tasks.yml", ""},
		{taskFileStdin, taskFileFormatJSON5},
		{"tasks.json", taskFileFormatJSON5},
		{"tasks.json5", taskFileFormatJSON5},
		{"tasks.yml", taskFileFormatYAML},
		{"recipe.txt", taskFileFormatYAML},
	}
	for _, c := range quiet {
		if got := recipeOutputFormatMismatch(c[0], c[1]); got != "" {
			t.Errorf("recipeOutputFormatMismatch(%q, %q) = %q, want no warning", c[0], c[1], got)
		}
	}

	got := recipeOutputFormatMismatch("tasks.yml", taskFileFormatJSON5)
	if got == "" {
		t.Fatal("recipeOutputFormatMismatch(tasks.yml, json5) = no warning, want one")
	}
	if !strings.Contains(got, "tasks.yml") {
		t.Errorf("warning %q should name the path", got)
	}
	if !strings.Contains(got, "--tasks-format json5") {
		t.Errorf("warning %q should name the flag needed to read it back", got)
	}
}

func TestTaskFileDisplayName(t *testing.T) {
	cases := map[string]string{
		taskFileStdin: "<stdin>",
		"tasks.yml":   "tasks.yml",
		"a/b.json":    "a/b.json",
		"":            "",
	}
	for path, want := range cases {
		if got := taskFileDisplayName(path); got != want {
			t.Errorf("taskFileDisplayName(%q) = %q, want %q", path, got, want)
		}
	}
}

// TestResolveTaskFilePathStdin: "-" must resolve to itself with an
// empty format (so the caller sniffs) and must never fall through to
// the default candidate probe, which would silently prefer ./tasks.yml
// over the recipe the user piped in.
func TestResolveTaskFilePathStdin(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tasks.yml"), []byte("---\n"), 0o644); err != nil {
		t.Fatalf("write yml: %v", err)
	}
	withCwd(t, dir, func() {
		path, format, err := resolveTaskFilePath(taskFileStdin)
		if err != nil {
			t.Fatalf("resolveTaskFilePath: %v", err)
		}
		if path != taskFileStdin {
			t.Errorf("path = %q, want %q", path, taskFileStdin)
		}
		if format != "" {
			t.Errorf("format = %q, want empty so the caller sniffs", format)
		}
	})
}

// TestReadRecipeBytesURLPermission: validate is offline by contract, so
// it passes allowURL=false and an http(s) --tasks must not be fetched.
func TestReadRecipeBytesURLPermission(t *testing.T) {
	const url = "https://example.invalid/tasks.yml"
	if _, err := readRecipeBytes(url, false); err == nil {
		t.Fatal("readRecipeBytes(url, false) = nil error, want a local-read failure")
	} else if strings.Contains(err.Error(), "fetch") {
		t.Errorf("readRecipeBytes(url, false) attempted a fetch: %v", err)
	}
}

func TestResolveTaskFileArg(t *testing.T) {
	tests := []struct {
		name       string
		explicit   string
		positional []string
		want       string
		wantErr    string
	}{
		{name: "neither given", want: ""},
		{name: "flag only", explicit: "flag.yml", want: "flag.yml"},
		{name: "positional only", positional: []string{"pos.yml"}, want: "pos.yml"},
		{name: "both given is an error", explicit: "flag.yml", positional: []string{"pos.yml"}, wantErr: "both --tasks and a positional"},
		{name: "multiple positionals is an error", positional: []string{"a.yml", "b.yml"}, wantErr: "only one task file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveTaskFileArg(tt.explicit, tt.positional)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error %q, got: %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolveTaskFileFromArgsPositional(t *testing.T) {
	dir := t.TempDir()
	recipe := filepath.Join(dir, "staging.yml")
	if err := os.WriteFile(recipe, []byte("---\n- tasks: []\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	vars := filepath.Join(dir, "prod.yml")
	if err := os.WriteFile(vars, []byte("app: api\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// A --vars-file value that looks like a recipe must not be picked as
	// the positional; the real positional recipe path should win.
	got, _ := resolveTaskFileFromArgs([]string{"docket", "validate", "--vars-file", vars, recipe})
	if got != recipe {
		t.Errorf("expected positional %q, got %q", recipe, got)
	}

	// --tasks still takes precedence for preregistration.
	got, _ = resolveTaskFileFromArgs([]string{"docket", "validate", "--tasks", recipe})
	if got != recipe {
		t.Errorf("expected --tasks %q, got %q", recipe, got)
	}
}

func TestResolveTaskFilePathExplicit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.json")
	if err := os.WriteFile(path, []byte("[]"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	gotPath, gotFormat, err := resolveTaskFilePath(path)
	if err != nil {
		t.Fatalf("resolveTaskFilePath: %v", err)
	}
	if gotPath != path {
		t.Errorf("path = %q, want %q", gotPath, path)
	}
	if gotFormat != taskFileFormatJSON5 {
		t.Errorf("format = %q, want %q", gotFormat, taskFileFormatJSON5)
	}
}

func TestResolveTaskFilePathDefaultPrefersYAML(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tasks.yml"), []byte("---\n"), 0o644); err != nil {
		t.Fatalf("write yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), []byte("[]"), 0o644); err != nil {
		t.Fatalf("write json: %v", err)
	}
	withCwd(t, dir, func() {
		path, format, err := resolveTaskFilePath("")
		if err != nil {
			t.Fatalf("resolveTaskFilePath: %v", err)
		}
		if path != "tasks.yml" {
			t.Errorf("path = %q, want tasks.yml", path)
		}
		if format != taskFileFormatYAML {
			t.Errorf("format = %q, want yaml", format)
		}
	})
}

func TestResolveTaskFilePathDefaultFallsThroughToJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), []byte("[]"), 0o644); err != nil {
		t.Fatalf("write json: %v", err)
	}
	withCwd(t, dir, func() {
		path, format, err := resolveTaskFilePath("")
		if err != nil {
			t.Fatalf("resolveTaskFilePath: %v", err)
		}
		if path != "tasks.json" {
			t.Errorf("path = %q, want tasks.json", path)
		}
		if format != taskFileFormatJSON5 {
			t.Errorf("format = %q, want json5", format)
		}
	})
}

func TestResolveTaskFilePathDefaultErrorsWhenNoneExist(t *testing.T) {
	dir := t.TempDir()
	withCwd(t, dir, func() {
		_, _, err := resolveTaskFilePath("")
		if err == nil {
			t.Fatal("expected error when no candidate task file exists")
		}
		if !strings.Contains(err.Error(), "no task file found") {
			t.Errorf("error = %q, want substring 'no task file found'", err.Error())
		}
	})
}

func TestResolveTaskFileFromArgsUsesExplicitFlag(t *testing.T) {
	path, format := resolveTaskFileFromArgs([]string{"docket", "apply", "--tasks", "custom.json"})
	if path != "custom.json" {
		t.Errorf("path = %q, want custom.json", path)
	}
	if format != taskFileFormatJSON5 {
		t.Errorf("format = %q, want json5", format)
	}

	path, format = resolveTaskFileFromArgs([]string{"docket", "apply", "--tasks=other.yml"})
	if path != "other.yml" {
		t.Errorf("path = %q, want other.yml", path)
	}
	if format != taskFileFormatYAML {
		t.Errorf("format = %q, want yaml", format)
	}
}

// TestResolveTaskFileFromArgsStdin covers every spelling of the stdin
// recipe, plus the two ways a bare "-" was previously lost: the
// HasPrefix("-") branch swallowed it as a flag, and the post-loop
// os.Stat("-") failed and fell through to the ./tasks.yml probe - which
// would have preregistered inputs from the wrong recipe entirely.
func TestResolveTaskFileFromArgsStdin(t *testing.T) {
	tests := []struct {
		name string
		argv []string
	}{
		{name: "--tasks -", argv: []string{"docket", "apply", "--tasks", "-"}},
		{name: "--tasks=-", argv: []string{"docket", "apply", "--tasks=-"}},
		{name: "bare positional", argv: []string{"docket", "apply", "-"}},
		{name: "positional before flags", argv: []string{"docket", "apply", "-", "--json"}},
		{name: "positional after flags", argv: []string{"docket", "apply", "--json", "-"}},
	}

	// A tasks.yml in the working directory is exactly what the os.Stat
	// fallthrough used to resolve to; "-" must still win.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tasks.yml"), []byte("---\n- tasks: []\n"), 0o644); err != nil {
		t.Fatalf("seed tasks.yml: %v", err)
	}

	withCwd(t, dir, func() {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				path, format := resolveTaskFileFromArgs(tt.argv)
				if path != taskFileStdin {
					t.Errorf("path = %q, want %q", path, taskFileStdin)
				}
				if format != "" {
					t.Errorf("format = %q, want empty so the caller sniffs stdin", format)
				}
			})
		}
	})
}

// TestResolveTaskFileFromArgsStdinNotAFlagValue: a "-" that is the value
// of a value-taking flag is that flag's value, not the recipe. Testing
// the ordering of the bare-"-" check against the skipNext check.
func TestResolveTaskFileFromArgsStdinNotAFlagValue(t *testing.T) {
	dir := t.TempDir()
	recipe := filepath.Join(dir, "staging.yml")
	if err := os.WriteFile(recipe, []byte("---\n- tasks: []\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	for _, flagName := range []string{"--play", "--vars-file", "--host", "--start-at-task", "--tasks-format"} {
		t.Run(flagName, func(t *testing.T) {
			path, _ := resolveTaskFileFromArgs([]string{"docket", "apply", flagName, "-", recipe})
			if path != recipe {
				t.Errorf("path = %q, want %q (the %s value must not be read as the recipe)", path, recipe, flagName)
			}
		})
	}
}

func TestTasksFormatFromArgs(t *testing.T) {
	tests := []struct {
		name string
		argv []string
		want string
	}{
		{name: "absent", argv: []string{"docket", "apply", "-"}, want: ""},
		{name: "space separated", argv: []string{"docket", "apply", "--tasks-format", "json5", "-"}, want: "json5"},
		{name: "equals separated", argv: []string{"docket", "apply", "--tasks-format=yaml", "-"}, want: "yaml"},
		{name: "trailing flag with no value", argv: []string{"docket", "apply", "--tasks-format"}, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tasksFormatFromArgs(tt.argv); got != tt.want {
				t.Errorf("tasksFormatFromArgs(%v) = %q, want %q", tt.argv, got, tt.want)
			}
		})
	}
}

// TestTaskFileAutocompleteMatchesRecipeExtensions guards #340: the previous
// brace glob "*.{yml,yaml,json,json5}" matched no file through filepath.Glob,
// so completion only ever offered directories. Every recipe extension must now
// be offered, a non-recipe file must not, and a directory must appear once
// (the dedupe, since each per-extension sub-predictor lists it).
func TestTaskFileAutocompleteMatchesRecipeExtensions(t *testing.T) {
	dir := t.TempDir()
	recipes := []string{"tasks.yml", "config.yaml", "data.json", "recipe.json5"}
	for _, name := range recipes {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("---\n"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed notes.txt: %v", err)
	}
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatalf("mkdir sub: %v", err)
	}

	withCwd(t, dir, func() {
		counts := map[string]int{}
		for _, match := range taskFileAutocomplete().Predict(complete.Args{Last: ""}) {
			counts[match]++
		}
		for _, name := range recipes {
			if counts[name] == 0 {
				t.Errorf("expected %q to be offered, got %v", name, counts)
			}
		}
		if counts["notes.txt"] != 0 {
			t.Errorf("non-recipe notes.txt must not be offered, got %v", counts)
		}
		if counts["sub/"] != 1 {
			t.Errorf("directory sub/ should be offered exactly once, got %d (%v)", counts["sub/"], counts)
		}
	})
}

// TestPredictFilesByExtension proves the completion mechanism is generic and
// not hard-wired to the recipe extensions.
func TestPredictFilesByExtension(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"readme.md", "todo.txt", "ignore.yml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}

	withCwd(t, dir, func() {
		got := map[string]bool{}
		for _, match := range predictFilesByExtension([]string{"md", "txt"}).Predict(complete.Args{Last: ""}) {
			got[match] = true
		}
		if !got["readme.md"] {
			t.Errorf("expected readme.md to be offered, got %v", got)
		}
		if !got["todo.txt"] {
			t.Errorf("expected todo.txt to be offered, got %v", got)
		}
		if got["ignore.yml"] {
			t.Errorf("ignore.yml must not be offered for extensions {md,txt}, got %v", got)
		}
	})
}

func TestHasTaskFileExtension(t *testing.T) {
	yes := []string{"tasks.yml", "tasks.YAML", "path/to/c.json", "x.json5"}
	no := []string{"notes.txt", "tasks", "archive.tar.gz", ""}
	for _, p := range yes {
		if !hasTaskFileExtension(p) {
			t.Errorf("hasTaskFileExtension(%q) = false, want true", p)
		}
	}
	for _, p := range no {
		if hasTaskFileExtension(p) {
			t.Errorf("hasTaskFileExtension(%q) = true, want false", p)
		}
	}
}

// withCwd chdirs to dir for the duration of body and restores the
// original cwd afterwards. Centralised so the resolveTaskFilePath
// tests do not each handle the t.Cleanup dance.
func withCwd(t *testing.T, dir string, body func()) {
	t.Helper()
	prev, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir %s: %v", dir, err)
	}
	t.Cleanup(func() { _ = os.Chdir(prev) })
	body()
}
