package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"

	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/mitchellh/cli"
)

func fakeExecRunner(responses map[string]string) func(context.Context, subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
	return func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		return subprocess.ExecCommandResponse{Stdout: responses[strings.Join(in.Args, " ")]}, nil
	}
}

func exportCommandFixture() map[string]string {
	return map[string]string{
		"--quiet apps:list":                       "web",
		"--quiet config:export --format json web": `{"API_KEY":"abc123"}`,
		"domains:report web --domains-app-vhosts": "",
	}
}

func newExportCommand() (*ExportCommand, *cli.MockUi) {
	ui := cli.NewMockUi()
	c := &ExportCommand{Meta: command.Meta{Ui: ui}}
	return c, ui
}

func TestExportCommandMetadata(t *testing.T) {
	c := &ExportCommand{}
	if c.Name() != "export" {
		t.Errorf("Name = %q, want export", c.Name())
	}
	if c.Synopsis() == "" {
		t.Error("Synopsis must not be empty")
	}
}

func TestExportCommandWritesRecipeAndVars(t *testing.T) {
	defer subprocess.SetExecRunner(fakeExecRunner(exportCommandFixture()))()

	dir := t.TempDir()
	recipe := filepath.Join(dir, "tasks.yml")
	vars := filepath.Join(dir, "tasks.vars.yml")

	c, _ := newExportCommand()
	if code := c.Run([]string{"--output", recipe}); code != 0 {
		t.Fatalf("Run exit = %d, want 0", code)
	}

	recipeBytes, err := os.ReadFile(recipe)
	if err != nil {
		t.Fatalf("recipe not written: %v", err)
	}
	if !strings.Contains(string(recipeBytes), "{{ .web_API_KEY }}") {
		t.Errorf("recipe should reference the input:\n%s", recipeBytes)
	}
	if strings.Contains(string(recipeBytes), "abc123") {
		t.Errorf("recipe leaked the secret value:\n%s", recipeBytes)
	}

	varsBytes, err := os.ReadFile(vars)
	if err != nil {
		t.Fatalf("vars-file not written (default derived path): %v", err)
	}
	if !strings.Contains(string(varsBytes), "abc123") {
		t.Errorf("vars-file should hold the real value:\n%s", varsBytes)
	}
}

func TestExportCommandOverwritePromptDeclined(t *testing.T) {
	defer subprocess.SetExecRunner(fakeExecRunner(exportCommandFixture()))()

	dir := t.TempDir()
	recipe := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(recipe, []byte("OLD\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c, ui := newExportCommand()
	ui.InputReader = strings.NewReader("n\n")
	if code := c.Run([]string{"--output", recipe}); code != 1 {
		t.Fatalf("declined overwrite should exit 1, got %d", code)
	}
	got, _ := os.ReadFile(recipe)
	if string(got) != "OLD\n" {
		t.Errorf("declined overwrite must not modify the file, got %q", got)
	}
}

func TestExportCommandOverwriteConfirmed(t *testing.T) {
	defer subprocess.SetExecRunner(fakeExecRunner(exportCommandFixture()))()

	dir := t.TempDir()
	recipe := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(recipe, []byte("OLD\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c, ui := newExportCommand()
	ui.InputReader = strings.NewReader("y\n")
	if code := c.Run([]string{"--output", recipe}); code != 0 {
		t.Fatalf("confirmed overwrite should exit 0, got %d", code)
	}
	got, _ := os.ReadFile(recipe)
	if strings.Contains(string(got), "OLD") {
		t.Errorf("confirmed overwrite should replace the file, got %q", got)
	}
}

func TestExportCommandOverwriteFlagSkipsPrompt(t *testing.T) {
	defer subprocess.SetExecRunner(fakeExecRunner(exportCommandFixture()))()

	dir := t.TempDir()
	recipe := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(recipe, []byte("OLD\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	c, _ := newExportCommand()
	// No InputReader set: --overwrite must not prompt.
	if code := c.Run([]string{"--output", recipe, "--overwrite"}); code != 0 {
		t.Fatalf("--overwrite should exit 0 without prompting, got %d", code)
	}
	got, _ := os.ReadFile(recipe)
	if strings.Contains(string(got), "OLD") {
		t.Errorf("--overwrite should replace the file, got %q", got)
	}
}

func TestExportOutputValidates(t *testing.T) {
	defer subprocess.SetExecRunner(fakeExecRunner(exportCommandFixture()))()

	dir := t.TempDir()
	recipe := filepath.Join(dir, "tasks.yml")
	vars := filepath.Join(dir, "tasks.vars.yml")

	c, _ := newExportCommand()
	if code := c.Run([]string{"--output", recipe}); code != 0 {
		t.Fatalf("export exit = %d", code)
	}

	// The exported recipe + vars-file must pass docket's own offline
	// validation: the emitted structure parses, the sigil templates render,
	// and the required inputs resolve from the vars-file. This is the
	// offline stand-in for the apply round-trip contract.
	// ValidateCommand.FlagSet loads the recipe's inputs from the --tasks path
	// in os.Args (before flag parsing), so set os.Args as the real CLI would.
	valArgs := []string{"--tasks", recipe, "--vars-file", vars, "--strict"}
	oldArgs := os.Args
	os.Args = append([]string{"docket", "validate"}, valArgs...)
	defer func() { os.Args = oldArgs }()

	vui := cli.NewMockUi()
	v := &ValidateCommand{Meta: command.Meta{Ui: vui}}
	if code := v.Run(valArgs); code != 0 {
		rb, _ := os.ReadFile(recipe)
		vb, _ := os.ReadFile(vars)
		t.Fatalf("docket validate --strict exit = %d, want 0\n--- validate stderr ---\n%s\n--- recipe ---\n%s\n--- vars ---\n%s",
			code, vui.ErrorWriter.String(), rb, vb)
	}
}

// TestExportOutputValidatesJSON5 is the JSON5 twin of
// TestExportOutputValidates: the pair --format json5 emits must round-trip
// through docket's own offline validation just as the YAML pair does.
func TestExportOutputValidatesJSON5(t *testing.T) {
	defer subprocess.SetExecRunner(fakeExecRunner(exportCommandFixture()))()

	dir := t.TempDir()
	t.Chdir(dir)
	recipe := filepath.Join(dir, "tasks.json")
	vars := filepath.Join(dir, "tasks.vars.json")

	c, ui := newExportCommand()
	if code := c.Run([]string{"--format", "json5"}); code != 0 {
		t.Fatalf("export exit = %d: %s", code, ui.ErrorWriter.String())
	}

	valArgs := []string{"--tasks", recipe, "--vars-file", vars, "--strict"}
	oldArgs := os.Args
	os.Args = append([]string{"docket", "validate"}, valArgs...)
	defer func() { os.Args = oldArgs }()

	vui := cli.NewMockUi()
	v := &ValidateCommand{Meta: command.Meta{Ui: vui}}
	if code := v.Run(valArgs); code != 0 {
		rb, _ := os.ReadFile(recipe)
		vb, _ := os.ReadFile(vars)
		t.Fatalf("docket validate --strict exit = %d, want 0\n--- validate stderr ---\n%s\n--- recipe ---\n%s\n--- vars ---\n%s",
			code, vui.ErrorWriter.String(), rb, vb)
	}
}

// TestExportCommandFormatJSON5WritesJSONPair pins the default-path swap
// for the pair of files export writes: tasks.json plus tasks.vars.json,
// and no .yml left behind.
func TestExportCommandFormatJSON5WritesJSONPair(t *testing.T) {
	defer subprocess.SetExecRunner(fakeExecRunner(exportCommandFixture()))()

	dir := t.TempDir()
	t.Chdir(dir)

	c, ui := newExportCommand()
	if code := c.Run([]string{"--format", "json5"}); code != 0 {
		t.Fatalf("Run exit = %d, want 0: %s", code, ui.ErrorWriter.String())
	}

	for _, name := range []string{"tasks.yml", "tasks.vars.yml"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("--format json5 should not write %s", name)
		}
	}

	recipe, err := os.ReadFile(filepath.Join(dir, "tasks.json"))
	if err != nil {
		t.Fatalf("tasks.json not written: %v", err)
	}
	if !strings.HasPrefix(string(recipe), "[") {
		t.Errorf("recipe should open with a JSON5 array:\n%s", recipe)
	}
	if !strings.Contains(string(recipe), "{{ .web_API_KEY }}") {
		t.Errorf("recipe should reference the input:\n%s", recipe)
	}

	vars, err := os.ReadFile(filepath.Join(dir, "tasks.vars.json"))
	if err != nil {
		t.Fatalf("tasks.vars.json not written: %v", err)
	}
	if !strings.HasPrefix(string(vars), "{") {
		t.Errorf("vars-file should be a JSON object:\n%s", vars)
	}
	if !strings.Contains(string(vars), `"web_API_KEY"`) || !strings.Contains(string(vars), "abc123") {
		t.Errorf("vars-file should hold the real value under the input name:\n%s", vars)
	}
	// export always emits per-task "cannot read this back" warnings, so
	// assert on the absence of the mismatch one specifically.
	if warn := ui.ErrorWriter.String(); strings.Contains(warn, "does not match") {
		t.Errorf("matching extensions should not warn about a mismatch:\n%s", warn)
	}
}

// TestExportCommandFormatOverridesOutputExtension pins that --format wins
// over an explicit --output extension, and that the resulting file - whose
// name now lies about its contents - is warned about.
func TestExportCommandFormatOverridesOutputExtension(t *testing.T) {
	defer subprocess.SetExecRunner(fakeExecRunner(exportCommandFixture()))()

	dir := t.TempDir()
	recipe := filepath.Join(dir, "tasks.yml")

	c, ui := newExportCommand()
	if code := c.Run([]string{"--output", recipe, "--format", "json5"}); code != 0 {
		t.Fatalf("Run exit = %d, want 0: %s", code, ui.ErrorWriter.String())
	}

	body, err := os.ReadFile(recipe)
	if err != nil {
		t.Fatalf("recipe not written: %v", err)
	}
	if !strings.HasPrefix(string(body), "[") {
		t.Errorf("--format json5 should have won over the .yml extension:\n%s", body)
	}
	if warn := ui.ErrorWriter.String(); !strings.Contains(warn, "--tasks-format json5") {
		t.Errorf("a lying extension should warn how to read it back:\n%s", warn)
	}
}

// TestExportCommandFormatGovernsVarsFile pins decision 3 of #410: an
// explicit --format sets the vars-file format too, even when
// --vars-output names a different extension.
func TestExportCommandFormatGovernsVarsFile(t *testing.T) {
	defer subprocess.SetExecRunner(fakeExecRunner(exportCommandFixture()))()

	dir := t.TempDir()
	recipe := filepath.Join(dir, "tasks.json5")
	vars := filepath.Join(dir, "tasks.vars.yml")

	c, ui := newExportCommand()
	if code := c.Run([]string{"--output", recipe, "--vars-output", vars, "--format", "json5"}); code != 0 {
		t.Fatalf("Run exit = %d, want 0: %s", code, ui.ErrorWriter.String())
	}

	body, err := os.ReadFile(vars)
	if err != nil {
		t.Fatalf("vars-file not written: %v", err)
	}
	// A quoted key is the JSON tell; the YAML encoder emits web_API_KEY:
	// unquoted. JSON is valid YAML, so this file still loads under its
	// .yml name - which is why only the recipe earns a warning.
	if !strings.HasPrefix(string(body), "{") || !strings.Contains(string(body), `"web_API_KEY":`) {
		t.Errorf("--format json5 should have won over the .yml vars extension:\n%s", body)
	}
}

// TestExportCommandVarsFileKeepsItsOwnExtensionWithoutFormat is the
// backwards-compatibility half: with no --format, the vars-file still
// follows its own --vars-output extension rather than the recipe's.
func TestExportCommandVarsFileKeepsItsOwnExtensionWithoutFormat(t *testing.T) {
	tests := []struct {
		name       string
		varsName   string
		wantPrefix string
	}{
		{name: "json vars beside a yaml recipe", varsName: "vars.json", wantPrefix: "{"},
		{name: "yaml vars beside a yaml recipe", varsName: "vars.yml", wantPrefix: "web_API_KEY:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer subprocess.SetExecRunner(fakeExecRunner(exportCommandFixture()))()

			dir := t.TempDir()
			vars := filepath.Join(dir, tt.varsName)

			c, ui := newExportCommand()
			code := c.Run([]string{"--output", filepath.Join(dir, "tasks.yml"), "--vars-output", vars})
			if code != 0 {
				t.Fatalf("Run exit = %d, want 0: %s", code, ui.ErrorWriter.String())
			}

			body, err := os.ReadFile(vars)
			if err != nil {
				t.Fatalf("vars-file not written: %v", err)
			}
			if !strings.HasPrefix(string(body), tt.wantPrefix) {
				t.Errorf("vars-file should follow its own extension, want prefix %q:\n%s", tt.wantPrefix, body)
			}
		})
	}
}

// TestExportCommandFormatJSON5ToStdout is the round trip #410 was filed
// for. Streaming still inlines values, so there is no vars-file and
// nothing lands on disk.
func TestExportCommandFormatJSON5ToStdout(t *testing.T) {
	defer subprocess.SetExecRunner(fakeExecRunner(exportCommandFixture()))()

	dir := t.TempDir()
	t.Chdir(dir)

	var ui *cli.MockUi
	captured, exit := captureStdout(t, func() int {
		var c *ExportCommand
		c, ui = newExportCommand()
		return c.Run([]string{"--output", "-", "--format", "json5"})
	})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0: %s", exit, ui.ErrorWriter.String())
	}
	if !strings.HasPrefix(captured, "[") {
		t.Errorf("stdout should open with a JSON5 array:\n%s", captured)
	}
	if !strings.Contains(captured, "abc123") {
		t.Errorf("a streamed recipe inlines its values:\n%s", captured)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("--output - should not write any file, found %d", len(entries))
	}
}

// TestExportCommandFormatJSON5PromptsOnAdjustedPath is the export twin of
// init's --force sequencing test: the overwrite prompt has to name the
// path the swap produced.
func TestExportCommandFormatJSON5PromptsOnAdjustedPath(t *testing.T) {
	defer subprocess.SetExecRunner(fakeExecRunner(exportCommandFixture()))()

	dir := t.TempDir()
	t.Chdir(dir)
	recipe := filepath.Join(dir, "tasks.json")
	if err := os.WriteFile(recipe, []byte("OLD\n"), 0o644); err != nil {
		t.Fatalf("seed tasks.json: %v", err)
	}

	c, ui := newExportCommand()
	ui.InputReader = strings.NewReader("n\n")
	if code := c.Run([]string{"--format", "json5"}); code != 1 {
		t.Fatalf("declined overwrite exit = %d, want 1", code)
	}
	if body, _ := os.ReadFile(recipe); string(body) != "OLD\n" {
		t.Errorf("declined overwrite should leave the file alone: %q", body)
	}
	if out := ui.OutputWriter.String(); !strings.Contains(out, "tasks.json already exists") {
		t.Errorf("prompt should name the adjusted path:\n%s", out)
	}
}

// TestExportCommandRejectsUnknownFormatBeforeReadingServer deliberately
// installs no fake exec runner: passing proves the --format value is
// rejected before tasks.ExportRecipe would have shelled out to dokku.
func TestExportCommandRejectsUnknownFormatBeforeReadingServer(t *testing.T) {
	dir := t.TempDir()
	recipe := filepath.Join(dir, "tasks.yml")

	c, ui := newExportCommand()
	if code := c.Run([]string{"--format", "toml", "--output", recipe}); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	errOut := ui.ErrorWriter.String()
	if !strings.Contains(errOut, "--format") {
		t.Errorf("error should name --format:\n%s", errOut)
	}
	if !strings.Contains(errOut, "yaml, json5") {
		t.Errorf("error should name the valid values:\n%s", errOut)
	}
	if _, err := os.Stat(recipe); err == nil {
		t.Error("a rejected --format should not write the recipe")
	}
}

func TestExportCommandSummaryExcludesGlobalPlay(t *testing.T) {
	// #345: one app plus a global play must report "(1 app)", not "(2 apps)".
	responses := exportCommandFixture()
	responses["--quiet plugin:list --format json"] = `[{"name":"redis","core":false,"source_url":"https://github.com/dokku/dokku-redis.git","committish":"c0ffee","branch":"master"}]`
	defer subprocess.SetExecRunner(fakeExecRunner(responses))()

	dir := t.TempDir()
	recipe := filepath.Join(dir, "tasks.yml")

	c, ui := newExportCommand()
	if code := c.Run([]string{"--output", recipe}); code != 0 {
		t.Fatalf("Run exit = %d, want 0", code)
	}
	out := ui.OutputWriter.String()
	if !strings.Contains(out, "(1 app)") {
		t.Errorf("summary should report 1 app, got:\n%s", out)
	}
	if strings.Contains(out, "(2 apps)") {
		t.Errorf("summary must not count the global play as an app:\n%s", out)
	}
}

func TestExportCommandNonexistentAppExitsNonZero(t *testing.T) {
	// #346: a --app typo must not write an empty recipe and exit 0.
	defer subprocess.SetExecRunner(fakeExecRunner(exportCommandFixture()))()

	dir := t.TempDir()
	recipe := filepath.Join(dir, "tasks.yml")

	c, ui := newExportCommand()
	code := c.Run([]string{"--app", "nope", "--output", recipe})
	if code != 1 {
		t.Fatalf("Run exit = %d, want 1", code)
	}
	if _, err := os.Stat(recipe); !os.IsNotExist(err) {
		t.Errorf("no recipe file should be written for a nonexistent app")
	}
	if !strings.Contains(ui.ErrorWriter.String(), "nope") {
		t.Errorf("error output should name the missing app:\n%s", ui.ErrorWriter.String())
	}
}

func TestExportCommandPartialMissingAppStillWrites(t *testing.T) {
	// #346: existing apps are still exported, but a missing one forces a non-zero
	// exit so the typo is surfaced.
	defer subprocess.SetExecRunner(fakeExecRunner(exportCommandFixture()))()

	dir := t.TempDir()
	recipe := filepath.Join(dir, "tasks.yml")

	c, ui := newExportCommand()
	code := c.Run([]string{"--app", "web", "--app", "nope", "--output", recipe})
	if code != 1 {
		t.Fatalf("Run exit = %d, want 1", code)
	}
	body, err := os.ReadFile(recipe)
	if err != nil {
		t.Fatalf("web should still be exported: %v", err)
	}
	if !strings.Contains(string(body), "web") {
		t.Errorf("recipe should contain the exported app web:\n%s", body)
	}
	if !strings.Contains(ui.ErrorWriter.String(), "nope") {
		t.Errorf("error output should name the missing app:\n%s", ui.ErrorWriter.String())
	}
}

func TestExportCommandDeriveVarsOutput(t *testing.T) {
	cases := map[string]string{
		"tasks.yml":        "tasks.vars.yml",
		"tasks.json":       "tasks.vars.json",
		"deploy/prod.yaml": "deploy/prod.vars.yaml",
		"noext":            "noext.vars",
	}
	for in, want := range cases {
		if got := deriveVarsOutput(in); got != want {
			t.Errorf("deriveVarsOutput(%q) = %q, want %q", in, got, want)
		}
	}
}
