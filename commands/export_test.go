package commands

import (
	"context"
	"io"
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

// newExportCommand builds an ExportCommand writing into baseDir. Passing one
// is what replaced chdir'ing the process: export's --output default is
// relative, and several tests are precisely about what that default resolves
// to.
func newExportCommand(baseDir ...string) (*ExportCommand, *cli.MockUi) {
	ui := cli.NewMockUi()
	c := &ExportCommand{Meta: command.Meta{Ui: ui}}
	if len(baseDir) > 0 {
		c.BaseDir = baseDir[0]
	}
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

	c, _ := newExportCommand(dir)
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

	c, ui := newExportCommand(dir)
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

	c, ui := newExportCommand(dir)
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

	c, _ := newExportCommand(dir)
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

	c, _ := newExportCommand(dir)
	if code := c.Run([]string{"--output", recipe}); code != 0 {
		t.Fatalf("export exit = %d", code)
	}

	// The exported recipe + vars-file must pass docket's own offline
	// validation: the emitted structure parses, the sigil templates render,
	// and the required inputs resolve from the vars-file. This is the
	// offline stand-in for the apply round-trip contract.
	// ValidateCommand.FlagSet loads the recipe's inputs from the --tasks path
	// in its argv (before flag parsing), so hand it one as the real CLI would.
	valArgs := []string{"--tasks", recipe, "--vars-file", vars, "--strict"}

	vui := cli.NewMockUi()
	v := &ValidateCommand{Meta: command.Meta{Ui: vui}, Argv: append([]string{"docket", "validate"}, valArgs...)}
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
	recipe := filepath.Join(dir, "tasks.json")
	vars := filepath.Join(dir, "tasks.vars.json")

	c, ui := newExportCommand(dir)
	if code := c.Run([]string{"--format", "json5"}); code != 0 {
		t.Fatalf("export exit = %d: %s", code, ui.ErrorWriter.String())
	}

	valArgs := []string{"--tasks", recipe, "--vars-file", vars, "--strict"}

	vui := cli.NewMockUi()
	v := &ValidateCommand{Meta: command.Meta{Ui: vui}, Argv: append([]string{"docket", "validate"}, valArgs...)}
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

	c, ui := newExportCommand(dir)
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

	c, ui := newExportCommand(dir)
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

	c, ui := newExportCommand(dir)
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

			c, ui := newExportCommand(dir)
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

	var ui *cli.MockUi
	captured, exit := captureStdout(t, func(out io.Writer) int {
		var c *ExportCommand
		c, ui = newExportCommand(dir)
		c.Stdout = out
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
	recipe := filepath.Join(dir, "tasks.json")
	if err := os.WriteFile(recipe, []byte("OLD\n"), 0o644); err != nil {
		t.Fatalf("seed tasks.json: %v", err)
	}

	c, ui := newExportCommand(dir)
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

	c, ui := newExportCommand(dir)
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

// TestExportCommandRejectsStdoutInertFlags is the #419 regression. Like
// TestExportCommandRejectsUnknownFormatBeforeReadingServer it installs no
// fake exec runner, so passing proves the rejection lands before
// tasks.ExportRecipe would have shelled out to dokku.
func TestExportCommandRejectsStdoutInertFlags(t *testing.T) {
	tests := []struct {
		name     string
		extra    []string
		wantFlag string
	}{
		{name: "vars-output", extra: []string{"--vars-output", "VARS"}, wantFlag: "--vars-output"},
		{name: "overwrite", extra: []string{"--overwrite"}, wantFlag: "--overwrite"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			vars := filepath.Join(dir, "secrets.yml")

			args := []string{"--output", "-"}
			for _, a := range tt.extra {
				if a == "VARS" {
					a = vars
				}
				args = append(args, a)
			}

			c, ui := newExportCommand(dir)
			if code := c.Run(args); code != 1 {
				t.Fatalf("exit = %d, want 1", code)
			}
			errOut := ui.ErrorWriter.String()
			if !strings.Contains(errOut, tt.wantFlag) {
				t.Errorf("error should name %s:\n%s", tt.wantFlag, errOut)
			}
			if !strings.Contains(errOut, "--output -") {
				t.Errorf("error should name --output -:\n%s", errOut)
			}
			if _, err := os.Stat(vars); err == nil {
				t.Error("a rejected export should not write the vars-file")
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				t.Fatalf("read dir: %v", err)
			}
			if len(entries) != 0 {
				t.Errorf("a rejected export should write nothing, found %d entries", len(entries))
			}
		})
	}
}

// TestExportCommandReportsUnusedVarsOutput covers the other half of #419:
// an explicit --vars-output that goes unwritten because the server held
// nothing sensitive is reported rather than passed over. The derived
// default stays silent.
func TestExportCommandReportsUnusedVarsOutput(t *testing.T) {
	// The fixture's only sensitive value is the config entry; an empty
	// config export leaves ExportResult.Vars empty and writeVars false.
	fixture := exportCommandFixture()
	fixture["--quiet config:export --format json web"] = "{}"

	t.Run("explicit --vars-output is reported", func(t *testing.T) {
		defer subprocess.SetExecRunner(fakeExecRunner(fixture))()

		dir := t.TempDir()
		recipe := filepath.Join(dir, "tasks.yml")
		vars := filepath.Join(dir, "secrets.yml")

		c, ui := newExportCommand(dir)
		if code := c.Run([]string{"--output", recipe, "--vars-output", vars}); code != 0 {
			t.Fatalf("Run exit = %d, want 0: %s", code, ui.ErrorWriter.String())
		}
		if _, err := os.Stat(vars); !os.IsNotExist(err) {
			t.Errorf("no sensitive values means no vars-file: %v", err)
		}
		out := ui.OutputWriter.String()
		if !strings.Contains(out, "no sensitive values") {
			t.Errorf("summary should say why the vars-file is missing:\n%s", out)
		}
		if !strings.Contains(out, vars) {
			t.Errorf("summary should name the path that was asked for:\n%s", out)
		}
		// apply needs no --vars-file when there is no vars-file.
		if strings.Contains(out, "--vars-file") {
			t.Errorf("next steps should not offer a vars-file that was never written:\n%s", out)
		}
	})

	t.Run("derived default stays quiet", func(t *testing.T) {
		defer subprocess.SetExecRunner(fakeExecRunner(fixture))()

		dir := t.TempDir()
		recipe := filepath.Join(dir, "tasks.yml")

		c, ui := newExportCommand(dir)
		if code := c.Run([]string{"--output", recipe}); code != 0 {
			t.Fatalf("Run exit = %d, want 0: %s", code, ui.ErrorWriter.String())
		}
		if out := ui.OutputWriter.String(); strings.Contains(out, "no sensitive values") {
			t.Errorf("a path the user did not choose should not be reported:\n%s", out)
		}
	})
}

func TestExportCommandSummaryExcludesGlobalPlay(t *testing.T) {
	// #345: one app plus a global play must report "(1 app)", not "(2 apps)".
	responses := exportCommandFixture()
	responses["--quiet plugin:list --format json"] = `[{"name":"redis","core":false,"source_url":"https://github.com/dokku/dokku-redis.git","committish":"c0ffee","branch":"master"}]`
	defer subprocess.SetExecRunner(fakeExecRunner(responses))()

	dir := t.TempDir()
	recipe := filepath.Join(dir, "tasks.yml")

	c, ui := newExportCommand(dir)
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

	c, ui := newExportCommand(dir)
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

	c, ui := newExportCommand(dir)
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

// TestExportCommandResourceSelectsOneResource covers the --resource flag end
// to end: only the addressed task reaches the recipe.
func TestExportCommandResourceSelectsOneResource(t *testing.T) {
	defer subprocess.SetExecRunner(fakeExecRunner(map[string]string{
		"--quiet apps:list":                       "web",
		"--quiet config:export --format json web": `{"LOG_LEVEL":"info"}`,
		"domains:report web --domains-app-vhosts": "web.example.com",
	}))()

	dir := t.TempDir()
	recipe := filepath.Join(dir, "tasks.yml")

	c, ui := newExportCommand(dir)
	if code := c.Run([]string{"--output", recipe, "--resource", "dokku_config[app=web]"}); code != 0 {
		t.Fatalf("Run exit = %d, want 0; stderr=%s", code, ui.ErrorWriter.String())
	}

	out, err := os.ReadFile(recipe)
	if err != nil {
		t.Fatalf("recipe not written: %v", err)
	}
	if !strings.Contains(string(out), "dokku_config") {
		t.Errorf("recipe should hold the addressed task:\n%s", out)
	}
	if strings.Contains(string(out), "dokku_domains") {
		t.Errorf("recipe should hold only the addressed task type:\n%s", out)
	}
}

// TestExportCommandResourceRejectsAppCombination pins the mutual exclusion: an
// address already names its app, so combining the two filters can only express
// a contradiction or a redundancy.
func TestExportCommandResourceRejectsAppCombination(t *testing.T) {
	c, ui := newExportCommand()
	code := c.Run([]string{"--output", "-", "--resource", "dokku_config[app=web]", "--app", "web"})
	if code == 0 {
		t.Fatal("expected a non-zero exit when --resource and --app are combined")
	}
	if !strings.Contains(ui.ErrorWriter.String(), "cannot be combined") {
		t.Errorf("expected a clear rejection; got %q", ui.ErrorWriter.String())
	}
}

// TestExportCommandResourceRejectsBadAddressBeforeReading asserts the address
// is validated before the server is contacted: the fake runner is left unset,
// so any read would answer empty and the run would fail later with a different
// message.
func TestExportCommandResourceRejectsBadAddressBeforeReading(t *testing.T) {
	c, ui := newExportCommand()
	code := c.Run([]string{"--output", "-", "--resource", "dokku_confg[app=web]"})
	if code == 0 {
		t.Fatal("expected a non-zero exit for an unknown task type")
	}
	if !strings.Contains(ui.ErrorWriter.String(), `did you mean "dokku_config"`) {
		t.Errorf("expected a did-you-mean hint; got %q", ui.ErrorWriter.String())
	}
}

// TestExportCommandResourceUnmatchedExitsNonZero asserts an address the server
// has nothing for is surfaced rather than silently exporting nothing, the same
// contract a nonexistent --app has.
func TestExportCommandResourceUnmatchedExitsNonZero(t *testing.T) {
	defer subprocess.SetExecRunner(fakeExecRunner(exportCommandFixture()))()

	c, ui := newExportCommand()
	code := c.Run([]string{"--output", "-", "--resource", "dokku_config[app=missing]"})
	if code == 0 {
		t.Fatal("expected a non-zero exit for an address that matched nothing")
	}
	if !strings.Contains(ui.ErrorWriter.String(), "dokku_config[app=missing]") {
		t.Errorf("expected the unmatched address to be named; got %q", ui.ErrorWriter.String())
	}
}

// TestExportOutputValidatesWithUnappliableK3sProfile is #483 as a regression:
// a server carrying a scheduler-k3s profile whose name dokku accepted but
// docket refuses for state 'present' used to export into a recipe that
// `docket validate` rejected outright - and it rejects the whole file, so the
// one bad profile made the entire export unusable. The profile is now reported
// and left out, the profile that can be applied still comes back, and the pair
// validates.
func TestExportOutputValidatesWithUnappliableK3sProfile(t *testing.T) {
	fixture := exportCommandFixture()
	fixture["--quiet scheduler-k3s:profiles:list --format json"] = `[
		{"name":"edge-pool","role":"worker"},
		{"name":"EdgePool","role":"worker"}
	]`
	defer subprocess.SetExecRunner(fakeExecRunner(fixture))()

	dir := t.TempDir()
	recipe := filepath.Join(dir, "tasks.yml")
	vars := filepath.Join(dir, "tasks.vars.yml")

	c, ui := newExportCommand(dir)
	if code := c.Run([]string{"--output", recipe}); code != 0 {
		t.Fatalf("export exit = %d: %s", code, ui.ErrorWriter.String())
	}

	// The operator is told which profile is missing and why, rather than being
	// handed a recipe that fails validation with no explanation.
	out := ui.OutputWriter.String() + ui.ErrorWriter.String()
	for _, want := range []string{"dokku_scheduler_k3s_profile", "EdgePool", "state 'absent'"} {
		if !strings.Contains(out, want) {
			t.Errorf("export output missing %q:\n%s", want, out)
		}
	}

	recipeBytes, err := os.ReadFile(recipe)
	if err != nil {
		t.Fatalf("recipe not written: %v", err)
	}
	if !strings.Contains(string(recipeBytes), "edge-pool") {
		t.Errorf("recipe dropped the appliable profile:\n%s", recipeBytes)
	}
	if strings.Contains(string(recipeBytes), "EdgePool") {
		t.Errorf("recipe carries the unappliable profile:\n%s", recipeBytes)
	}

	valArgs := []string{"--tasks", recipe, "--vars-file", vars, "--strict"}

	vui := cli.NewMockUi()
	v := &ValidateCommand{Meta: command.Meta{Ui: vui}, Argv: append([]string{"docket", "validate"}, valArgs...)}
	if code := v.Run(valArgs); code != 0 {
		t.Fatalf("docket validate --strict exit = %d, want 0\n--- validate stderr ---\n%s\n--- recipe ---\n%s",
			code, vui.ErrorWriter.String(), recipeBytes)
	}
}
