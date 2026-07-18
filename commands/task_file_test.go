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
	}
	for path, want := range cases {
		if got := detectTaskFileFormat(path); got != want {
			t.Errorf("detectTaskFileFormat(%q) = %q, want %q", path, got, want)
		}
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
