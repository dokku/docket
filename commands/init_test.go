package commands

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dokku/docket/tasks"

	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/mitchellh/cli"
	yaml "gopkg.in/yaml.v3"
)

func TestInitCommandMetadata(t *testing.T) {
	t.Parallel()
	c := &InitCommand{}
	if c.Name() != "init" {
		t.Errorf("Name = %q, want %q", c.Name(), "init")
	}
	if c.Synopsis() == "" {
		t.Error("Synopsis must not be empty")
	}
}

func TestInitCommandExamples(t *testing.T) {
	t.Parallel()
	c := &InitCommand{}
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

func TestInitCommandHelpDoesNotPanic(t *testing.T) {
	t.Parallel()
	c := &InitCommand{}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("FlagSet panicked: %v", r)
		}
	}()
	_ = c.FlagSet()
}

func TestInitRendersDefaultTemplate(t *testing.T) {
	t.Parallel()
	out, err := renderInit(initOptions{Name: "demo"})
	if err != nil {
		t.Fatalf("renderInit: %v", err)
	}
	body := string(out)
	for _, want := range []string{"dokku_app", "dokku_config", "dokku_domains", "dokku_git_sync", "default: demo"} {
		if !strings.Contains(body, want) {
			t.Errorf("output missing %q\n%s", want, body)
		}
	}
	// dokku_git_sync is documented as last so build/deploy follows the
	// app/config/domains setup.
	idxApp := strings.Index(body, "dokku_app")
	idxConfig := strings.Index(body, "dokku_config")
	idxDomains := strings.Index(body, "dokku_domains")
	idxSync := strings.Index(body, "dokku_git_sync")
	if !(idxApp < idxConfig && idxConfig < idxDomains && idxDomains < idxSync) {
		t.Errorf("ordering wrong: app=%d config=%d domains=%d sync=%d", idxApp, idxConfig, idxDomains, idxSync)
	}
}

func TestInitRendersMinimalTemplate(t *testing.T) {
	t.Parallel()
	out, err := renderInit(initOptions{Name: "demo", Minimal: true})
	if err != nil {
		t.Fatalf("renderInit: %v", err)
	}
	body := string(out)
	if !strings.Contains(body, "dokku_app") {
		t.Errorf("minimal output missing dokku_app: %s", body)
	}
	for _, unwanted := range []string{"dokku_config", "dokku_domains", "dokku_git_sync", "inputs:"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("minimal output unexpectedly contains %q\n%s", unwanted, body)
		}
	}
	// Hard-substituted app value, not a sigil expression.
	if !strings.Contains(body, "app: demo") {
		t.Errorf("minimal output missing literal `app: demo` substitution: %s", body)
	}
}

func TestInitRefusesToOverwriteWithoutForce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte("preserved\n"), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	c := newTestInitCommand(dir)
	if exit := c.Run([]string{"--output", path, "--name", "demo"}); exit != 1 {
		t.Errorf("exit = %d, want 1", exit)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "preserved\n" {
		t.Errorf("file was overwritten without --force: %q", got)
	}
}

func TestInitForceOverwrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte("preserved\n"), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	c := newTestInitCommand(dir)
	if exit := c.Run([]string{"--output", path, "--force", "--name", "demo"}); exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(got), "dokku_app") {
		t.Errorf("file was not overwritten: %q", got)
	}
}

func TestInitWritesDefaultTemplateToDisk(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")

	c := newTestInitCommand(dir)
	if exit := c.Run([]string{"--output", path, "--name", "demo"}); exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(got), "dokku_git_sync") {
		t.Errorf("file missing dokku_git_sync:\n%s", got)
	}
}

func TestInitNameFlagSetsAppDefault(t *testing.T) {
	t.Parallel()
	out, err := renderInit(initOptions{Name: "billing"})
	if err != nil {
		t.Fatalf("renderInit: %v", err)
	}
	if !strings.Contains(string(out), "default: billing") {
		t.Errorf("--name not propagated to app input default:\n%s", out)
	}
}

// TestInitNameSetsPlayName: --name names the scaffolded play (not just the
// app input default) so `docket apply --play <name>` resolves after init.
// Every template variant is checked because the play name is what the
// documented `--name` behaviour promises.
func TestInitNameSetsPlayName(t *testing.T) {
	t.Parallel()
	cases := []struct {
		label   string
		opts    initOptions
		format  string
		context map[string]interface{}
	}{
		{"yaml-default", initOptions{Name: "web"}, tasks.FormatYAML, map[string]interface{}{"app": "web", "repo": "https://example.com/r.git"}},
		{"yaml-minimal", initOptions{Name: "web", Minimal: true}, tasks.FormatYAML, nil},
		{"json5-default", initOptions{Name: "web", Format: tasks.FormatNameJSON5}, tasks.FormatNameJSON5, map[string]interface{}{"app": "web", "repo": "https://example.com/r.git"}},
		{"json5-minimal", initOptions{Name: "web", Minimal: true, Format: tasks.FormatNameJSON5}, tasks.FormatNameJSON5, nil},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			out, err := renderInit(tc.opts)
			if err != nil {
				t.Fatalf("renderInit: %v", err)
			}
			plays, err := tasks.GetPlaysWithFormat(out, tc.format, tc.context, nil)
			if err != nil {
				t.Fatalf("GetPlaysWithFormat: %v\n%s", err, out)
			}
			if len(plays) != 1 {
				t.Fatalf("plays = %d, want 1\n%s", len(plays), out)
			}
			if plays[0].Name != "web" {
				t.Errorf("play name = %q, want \"web\"\n%s", plays[0].Name, out)
			}
		})
	}
}

func TestInitRepoFlagSetsRepoDefault(t *testing.T) {
	t.Parallel()
	out, err := renderInit(initOptions{Name: "demo", Repo: "git@github.com:foo/bar.git"})
	if err != nil {
		t.Fatalf("renderInit: %v", err)
	}
	body := string(out)
	if !strings.Contains(body, `default: "git@github.com:foo/bar.git"`) {
		t.Errorf("--repo not propagated to repo input default:\n%s", body)
	}
	if strings.Contains(body, "required: true") {
		t.Errorf("repo input still marked required despite --repo:\n%s", body)
	}
}

func TestInitRepoEmptyKeepsRepoRequired(t *testing.T) {
	t.Parallel()
	out, err := renderInit(initOptions{Name: "demo"})
	if err != nil {
		t.Fatalf("renderInit: %v", err)
	}
	body := string(out)
	if !strings.Contains(body, "required: true") {
		t.Errorf("empty --repo should keep repo input required:\n%s", body)
	}
}

func TestInitDefaultNameFromCwd(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "widget-svc")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	if got := defaultName(dir); got != "widget-svc" {
		t.Errorf("defaultName(dir) = %q, want %q", got, "widget-svc")
	}
}

func TestInitDefaultRepoFromGitConfig(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := "[core]\n" +
		"\trepositoryformatversion = 0\n" +
		"[remote \"origin\"]\n" +
		"\turl = git@example.com:owner/repo.git\n" +
		"\tfetch = +refs/heads/*:refs/remotes/origin/*\n"
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write git config: %v", err)
	}

	if got := defaultRepo(dir); got != "git@example.com:owner/repo.git" {
		t.Errorf("defaultRepo(dir) = %q, want git@example.com:owner/repo.git", got)
	}
}

func TestInitNoGitConfigYieldsEmptyRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if got := defaultRepo(dir); got != "" {
		t.Errorf("defaultRepo(dir) with no .git/config = %q, want empty", got)
	}
}

func TestInitGitConfigWithoutOriginYieldsEmptyRepo(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := "[core]\n\trepositoryformatversion = 0\n[remote \"upstream\"]\n\turl = git@example.com:upstream/repo.git\n"
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write git config: %v", err)
	}

	if got := defaultRepo(dir); got != "" {
		t.Errorf("defaultRepo(dir) with no origin = %q, want empty", got)
	}
}

func TestInitDefaultPassesValidate(t *testing.T) {
	t.Parallel()
	out, err := renderInit(initOptions{Name: "demo"})
	if err != nil {
		t.Fatalf("renderInit: %v", err)
	}
	problems := tasks.Validate(out, tasks.ValidateOptions{})
	if len(problems) != 0 {
		t.Fatalf("default scaffold should pass validate, got %+v", problems)
	}

	out2, err := renderInit(initOptions{Name: "demo", Repo: "git@example.com:foo/bar.git"})
	if err != nil {
		t.Fatalf("renderInit: %v", err)
	}
	if problems := tasks.Validate(out2, tasks.ValidateOptions{}); len(problems) != 0 {
		t.Fatalf("default scaffold with --repo should pass validate, got %+v", problems)
	}
}

func TestInitSpecialCharacterNamesParse(t *testing.T) {
	t.Parallel()
	// A --name with YAML/JSON-special characters used to render a scaffold
	// that would not parse; the escaping helpers now emit a correctly
	// quoted scalar for every format, so init's parse check (which uses
	// UnmarshalRecipe) accepts the scaffold instead of writing a broken
	// file (#355).
	names := []string{"@web", "foo: bar", `has"quote`, "*star", "- dash", "#hash", "key: value: nested"}
	for _, name := range names {
		for _, tc := range []struct {
			label  string
			opts   initOptions
			format string
		}{
			{"yaml", initOptions{Name: name}, tasks.FormatYAML},
			{"yaml-minimal", initOptions{Name: name, Minimal: true}, tasks.FormatYAML},
			{"json5", initOptions{Name: name, Format: tasks.FormatNameJSON5}, tasks.FormatNameJSON5},
			{"json5-minimal", initOptions{Name: name, Minimal: true, Format: tasks.FormatNameJSON5}, tasks.FormatNameJSON5},
		} {
			out, err := renderInit(tc.opts)
			if err != nil {
				t.Fatalf("renderInit(%q, %s): %v", name, tc.label, err)
			}
			if _, err := tasks.UnmarshalRecipe(out, tc.format); err != nil {
				t.Errorf("name %q (%s) scaffold should parse, got: %v\n%s", name, tc.label, err, out)
			}
		}
	}
}

func TestInitYAMLSpecialNamesFullyValidate(t *testing.T) {
	t.Parallel()
	// The names the issue calls out (@web, a `: `-containing name) also
	// round-trip through full validate, including the sigil render that
	// substitutes the name into the task bodies (#355). A name with a double
	// quote (`has"quote`) used to break the rendered body; the scaffold now
	// pipes each interpolation through `dq`, so it validates too (#371).
	names := []string{"@web", "foo: bar", "*star", "- dash", `has"quote`}
	for _, name := range names {
		out, err := renderInit(initOptions{Name: name})
		if err != nil {
			t.Fatalf("renderInit(%q): %v", name, err)
		}
		if problems := tasks.Validate(out, tasks.ValidateOptions{}); len(problems) != 0 {
			t.Errorf("name %q scaffold should validate, got %+v\n%s", name, problems, out)
		}
	}
}

func TestInitSimpleNameStaysUnquoted(t *testing.T) {
	t.Parallel()
	// The escaping helper must not quote ordinary names, keeping the
	// scaffold output stable for the common case.
	out, err := renderInit(initOptions{Name: "billing"})
	if err != nil {
		t.Fatalf("renderInit: %v", err)
	}
	if !strings.Contains(string(out), "default: billing") {
		t.Errorf("simple name should stay unquoted:\n%s", out)
	}
}

func TestInitSpecialCharacterNameWritesValidFile(t *testing.T) {
	t.Parallel()
	// End-to-end: a special-character --name writes a file that round-trips
	// through validate, and the parse check (which now runs before the
	// write) does not reject it (#355).
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	c := newTestInitCommand(dir)
	if exit := c.Run([]string{"--output", path, "--name", "@web", "--repo", "https://example.com/r.git"}); exit != 0 {
		t.Fatalf("init --name @web exit = %d, want 0", exit)
	}
	out, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if problems := tasks.Validate(out, tasks.ValidateOptions{}); len(problems) != 0 {
		t.Errorf("written scaffold should validate, got %+v\n%s", problems, out)
	}
}

func TestInitMinimalPassesValidate(t *testing.T) {
	t.Parallel()
	out, err := renderInit(initOptions{Name: "demo", Minimal: true})
	if err != nil {
		t.Fatalf("renderInit: %v", err)
	}
	if problems := tasks.Validate(out, tasks.ValidateOptions{}); len(problems) != 0 {
		t.Fatalf("minimal scaffold should pass validate, got %+v", problems)
	}
}

func TestInitDefaultJSON5PassesValidate(t *testing.T) {
	t.Parallel()
	out, err := renderInit(initOptions{Name: "demo", Format: tasks.FormatNameJSON5})
	if err != nil {
		t.Fatalf("renderInit (json5): %v", err)
	}
	if problems := tasks.Validate(out, tasks.ValidateOptions{Format: tasks.FormatNameJSON5}); len(problems) != 0 {
		t.Fatalf("JSON5 default scaffold should pass validate, got %+v", problems)
	}
	out2, err := renderInit(initOptions{Name: "demo", Repo: "git@example.com:foo/bar.git", Format: tasks.FormatNameJSON5})
	if err != nil {
		t.Fatalf("renderInit (json5 with repo): %v", err)
	}
	if problems := tasks.Validate(out2, tasks.ValidateOptions{Format: tasks.FormatNameJSON5}); len(problems) != 0 {
		t.Fatalf("JSON5 scaffold with --repo should pass validate, got %+v", problems)
	}
}

func TestInitMinimalJSON5PassesValidate(t *testing.T) {
	t.Parallel()
	out, err := renderInit(initOptions{Name: "demo", Minimal: true, Format: tasks.FormatNameJSON5})
	if err != nil {
		t.Fatalf("renderInit (minimal json5): %v", err)
	}
	if problems := tasks.Validate(out, tasks.ValidateOptions{Format: tasks.FormatNameJSON5}); len(problems) != 0 {
		t.Fatalf("minimal JSON5 scaffold should pass validate, got %+v", problems)
	}
}

func TestInitJSON5HasNoYAMLDocumentMarker(t *testing.T) {
	t.Parallel()
	out, err := renderInit(initOptions{Name: "demo", Format: tasks.FormatNameJSON5})
	if err != nil {
		t.Fatalf("renderInit: %v", err)
	}
	if strings.HasPrefix(string(out), "---") {
		t.Errorf("JSON5 scaffold should not start with YAML document marker:\n%s", out)
	}
	if !strings.HasPrefix(strings.TrimSpace(string(out)), "[") {
		t.Errorf("JSON5 scaffold should start with [:\n%s", out)
	}
}

func TestInitJSON5RoundTripsThroughGetPlays(t *testing.T) {
	t.Parallel()
	out, err := renderInit(initOptions{Name: "api", Format: tasks.FormatNameJSON5})
	if err != nil {
		t.Fatalf("renderInit: %v", err)
	}
	plays, err := tasks.GetPlaysWithFormat(out, tasks.FormatNameJSON5, map[string]interface{}{
		"app":  "api",
		"repo": "https://example.com/repo.git",
	}, nil)
	if err != nil {
		t.Fatalf("GetPlaysWithFormat: %v", err)
	}
	if len(plays) != 1 {
		t.Fatalf("plays = %d, want 1", len(plays))
	}
	if got := len(plays[0].Tasks.Keys()); got != 4 {
		t.Errorf("default JSON5 scaffold = %d tasks, want 4", got)
	}
}

func TestSelectInitTemplate(t *testing.T) {
	t.Parallel()
	cases := map[[2]string]string{
		{tasks.FormatYAML, "false"}:      "default.yml.tmpl",
		{tasks.FormatYAML, "true"}:       "minimal.yml.tmpl",
		{tasks.FormatNameJSON5, "false"}: "default.json5.tmpl",
		{tasks.FormatNameJSON5, "true"}:  "minimal.json5.tmpl",
		{"", "false"}:                    "default.yml.tmpl",
	}
	for key, want := range cases {
		minimal := key[1] == "true"
		if got := selectInitTemplate(key[0], minimal); got != want {
			t.Errorf("selectInitTemplate(%q, %v) = %q, want %q", key[0], minimal, got, want)
		}
	}
}

func TestInitDefaultParsesAsRecipe(t *testing.T) {
	t.Parallel()
	out, err := renderInit(initOptions{Name: "api"})
	if err != nil {
		t.Fatalf("renderInit: %v", err)
	}
	taskList, err := tasks.GetTasks(out, map[string]interface{}{
		"app":  "api",
		"repo": "https://example.com/repo.git",
	})
	if err != nil {
		t.Fatalf("tasks.GetTasks: %v", err)
	}
	if got := len(taskList.Keys()); got != 4 {
		t.Errorf("default scaffold = %d tasks, want 4", got)
	}
}

func TestInitDefaultRecipeShape(t *testing.T) {
	t.Parallel()
	out, err := renderInit(initOptions{Name: "billing", Repo: "git@example.com:foo/bar.git"})
	if err != nil {
		t.Fatalf("renderInit: %v", err)
	}
	var recipe tasks.Recipe
	if err := yaml.Unmarshal(out, &recipe); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	if len(recipe) != 1 {
		t.Fatalf("recipe has %d plays, want 1", len(recipe))
	}
	play := recipe[0]
	if len(play.Inputs) != 2 {
		t.Errorf("play has %d inputs, want 2", len(play.Inputs))
	}
	var appInput, repoInput *tasks.Input
	for i := range play.Inputs {
		switch play.Inputs[i].Name {
		case "app":
			appInput = &play.Inputs[i]
		case "repo":
			repoInput = &play.Inputs[i]
		}
	}
	if appInput == nil || appInput.Default != "billing" {
		t.Errorf("app input default = %+v, want billing", appInput)
	}
	if repoInput == nil || repoInput.Default != "git@example.com:foo/bar.git" {
		t.Errorf("repo input default = %+v, want git@example.com:foo/bar.git", repoInput)
	}
	if repoInput != nil && repoInput.Required {
		t.Errorf("repo input should not be required when --repo is set")
	}
}

func TestInitOutputDashWritesToStdout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	captured, exit := captureStdout(t, func(out io.Writer) int {
		c := newTestInitCommand(dir)
		c.Stdout = out
		return c.Run([]string{"--output", "-", "--name", "demo"})
	})
	if exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}

	if !strings.HasPrefix(captured, "---\n") {
		t.Errorf("stdout missing leading ---:\n%s", captured)
	}
	if !strings.Contains(captured, "dokku_app") {
		t.Errorf("stdout missing dokku_app:\n%s", captured)
	}
	if strings.Contains(captured, "==> Created") {
		t.Errorf("stdout contains success block (should be suppressed):\n%s", captured)
	}
	if _, err := os.Stat(filepath.Join(dir, "tasks.yml")); err == nil {
		t.Errorf("--output - should not create tasks.yml on disk")
	}
}

// TestInitRejectsForceWithStdout is init's half of #419: the exists check
// --force governs is skipped entirely when streaming, so the flag would
// otherwise be read off the flag set and dropped.
func TestInitRejectsForceWithStdout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	c, ui := newTestInitCommandUi(dir)
	if exit := c.Run([]string{"--output", "-", "--force", "--name", "demo"}); exit != 1 {
		t.Fatalf("exit = %d, want 1", exit)
	}
	errOut := ui.ErrorWriter.String()
	if !strings.Contains(errOut, "--force") {
		t.Errorf("error should name --force:\n%s", errOut)
	}
	if !strings.Contains(errOut, "--output -") {
		t.Errorf("error should name --output -:\n%s", errOut)
	}
	for _, name := range []string{"tasks.yml", "tasks.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("a rejected init should not write %s", name)
		}
	}
}

// TestInitFormatJSON5WritesTasksJSON covers the headline of #410: asking
// for JSON5 by name, with no --output, writes tasks.json rather than a
// JSON5 document under a .yml name.
func TestInitFormatJSON5WritesTasksJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	c, ui := newTestInitCommandUi(dir)
	if exit := c.Run([]string{"--format", "json5", "--name", "demo"}); exit != 0 {
		t.Fatalf("exit = %d, want 0: %s", exit, ui.ErrorWriter.String())
	}

	if _, err := os.Stat(filepath.Join(dir, "tasks.yml")); err == nil {
		t.Error("--format json5 should not write tasks.yml")
	}
	body, err := os.ReadFile(filepath.Join(dir, "tasks.json"))
	if err != nil {
		t.Fatalf("tasks.json not written: %v", err)
	}
	if !strings.HasPrefix(string(body), "[") {
		t.Errorf("scaffold should open with a JSON5 array:\n%s", body)
	}
	if strings.HasPrefix(string(body), "---") {
		t.Errorf("JSON5 scaffold should not carry the YAML document marker:\n%s", body)
	}
	if problems := tasks.Validate(body, tasks.ValidateOptions{Format: tasks.FormatNameJSON5}); len(problems) > 0 {
		t.Errorf("scaffold did not validate: %+v", problems)
	}
	if out := ui.OutputWriter.String(); !strings.Contains(out, "Created tasks.json") {
		t.Errorf("summary should name tasks.json:\n%s", out)
	}
	if warn := ui.ErrorWriter.String(); warn != "" {
		t.Errorf("a matching extension should not warn:\n%s", warn)
	}
}

// TestInitFormatJSONAliasWritesTasksJSON pins the consequence of sharing
// one normaliser with --tasks-format: the json alias resolves to json5,
// so it drives the default-path swap too.
func TestInitFormatJSONAliasWritesTasksJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if exit := newTestInitCommand(dir).Run([]string{"--format", "json"}); exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	if _, err := os.Stat(filepath.Join(dir, "tasks.json")); err != nil {
		t.Errorf("--format json should write tasks.json: %v", err)
	}
}

// TestInitFormatYAMLKeepsDefaultPath guards the untouched default: only
// json5 moves the path.
func TestInitFormatYAMLKeepsDefaultPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	if exit := newTestInitCommand(dir).Run([]string{"--format", "yaml"}); exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	if _, err := os.Stat(filepath.Join(dir, "tasks.json")); err == nil {
		t.Error("--format yaml should not write tasks.json")
	}
	body, err := os.ReadFile(filepath.Join(dir, "tasks.yml"))
	if err != nil {
		t.Fatalf("tasks.yml not written: %v", err)
	}
	if !strings.HasPrefix(string(body), "---\n") {
		t.Errorf("YAML scaffold should keep its document marker:\n%s", body)
	}
}

// TestInitExplicitOutputWinsOverFormatDefault pins the rule that a path
// the user typed is never rewritten, even when --format would otherwise
// have moved the default. The extension then disagrees with the bytes,
// which is legal and warned about.
func TestInitExplicitOutputWinsOverFormatDefault(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	c, ui := newTestInitCommandUi(dir)
	if exit := c.Run([]string{"--output", "recipe.yml", "--format", "json5"}); exit != 0 {
		t.Fatalf("exit = %d, want 0: %s", exit, ui.ErrorWriter.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "tasks.json")); err == nil {
		t.Error("an explicit --output should not be replaced by the json5 default")
	}
	body, err := os.ReadFile(filepath.Join(dir, "recipe.yml"))
	if err != nil {
		t.Fatalf("recipe.yml not written: %v", err)
	}
	if !strings.HasPrefix(string(body), "[") {
		t.Errorf("--format json5 should have won over the .yml extension:\n%s", body)
	}
	if warn := ui.ErrorWriter.String(); !strings.Contains(warn, "--tasks-format json5") {
		t.Errorf("a lying extension should warn how to read it back:\n%s", warn)
	}
}

// TestInitFormatOverridesOutputExtension is the mirror case: --format
// yaml beats a .json extension.
func TestInitFormatOverridesOutputExtension(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")

	c, ui := newTestInitCommandUi(dir)
	if exit := c.Run([]string{"--output", path, "--format", "yaml"}); exit != 0 {
		t.Fatalf("exit = %d, want 0: %s", exit, ui.ErrorWriter.String())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("tasks.json not written: %v", err)
	}
	if !strings.HasPrefix(string(body), "---\n") {
		t.Errorf("--format yaml should have won over the .json extension:\n%s", body)
	}
	if problems := tasks.Validate(body, tasks.ValidateOptions{}); len(problems) > 0 {
		t.Errorf("scaffold did not validate as YAML: %+v", problems)
	}
	if warn := ui.ErrorWriter.String(); !strings.Contains(warn, "--tasks-format yaml") {
		t.Errorf("a lying extension should warn how to read it back:\n%s", warn)
	}
}

// TestInitFormatJSON5ChecksForceOnAdjustedPath is the sequencing test:
// the default-path swap has to happen before the exists / --force check,
// or init would stat tasks.yml while writing tasks.json - refusing over
// an unrelated file, then clobbering the relevant one.
func TestInitFormatJSON5ChecksForceOnAdjustedPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	jsonPath := filepath.Join(dir, "tasks.json")
	yamlPath := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(jsonPath, []byte("preserved\n"), 0o644); err != nil {
		t.Fatalf("seed tasks.json: %v", err)
	}
	if err := os.WriteFile(yamlPath, []byte("yaml-preserved\n"), 0o644); err != nil {
		t.Fatalf("seed tasks.yml: %v", err)
	}

	c, ui := newTestInitCommandUi(dir)
	if exit := c.Run([]string{"--format", "json5"}); exit != 1 {
		t.Fatalf("exit = %d, want 1", exit)
	}
	if errOut := ui.ErrorWriter.String(); !strings.Contains(errOut, "tasks.json already exists") {
		t.Errorf("error should name the adjusted path:\n%s", errOut)
	}
	if body, _ := os.ReadFile(jsonPath); string(body) != "preserved\n" {
		t.Errorf("tasks.json was overwritten without --force: %q", body)
	}

	if exit := newTestInitCommand(dir).Run([]string{"--format", "json5", "--force"}); exit != 0 {
		t.Fatalf("--force exit = %d, want 0", exit)
	}
	body, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("read tasks.json: %v", err)
	}
	if !strings.Contains(string(body), "dokku_app") {
		t.Errorf("--force should have rewritten tasks.json:\n%s", body)
	}
	if yaml, _ := os.ReadFile(yamlPath); string(yaml) != "yaml-preserved\n" {
		t.Errorf("tasks.yml is not the target and must be left alone: %q", yaml)
	}
}

// TestInitFormatJSON5ToStdout is the case #410 was filed for: before
// --format, `--output -` could only ever emit YAML.
func TestInitFormatJSON5ToStdout(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	captured, exit := captureStdout(t, func(out io.Writer) int {
		c := newTestInitCommand(dir)
		c.Stdout = out
		return c.Run([]string{"--output", "-", "--format", "json5", "--name", "demo"})
	})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	if strings.HasPrefix(captured, "---") {
		t.Errorf("JSON5 on stdout should not carry the YAML document marker:\n%s", captured)
	}
	if !strings.HasPrefix(captured, "[") {
		t.Errorf("stdout should open with a JSON5 array:\n%s", captured)
	}
	if !strings.Contains(captured, "dokku_app") {
		t.Errorf("stdout missing dokku_app:\n%s", captured)
	}
	if strings.Contains(captured, "==> Created") {
		t.Errorf("stdout contains the success block (should be suppressed):\n%s", captured)
	}
	if _, err := tasks.UnmarshalRecipe([]byte(captured), tasks.FormatNameJSON5); err != nil {
		t.Errorf("streamed scaffold did not parse as JSON5: %v", err)
	}
	for _, name := range []string{"tasks.yml", "tasks.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("--output - should not create %s on disk", name)
		}
	}
}

// TestInitRejectsUnknownFormat checks the value error names the flag the
// user actually typed, not the --tasks-format it shares a parser with.
func TestInitRejectsUnknownFormat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	c, ui := newTestInitCommandUi(dir)
	if exit := c.Run([]string{"--format", "toml"}); exit != 1 {
		t.Fatalf("exit = %d, want 1", exit)
	}
	errOut := ui.ErrorWriter.String()
	if !strings.Contains(errOut, "--format") {
		t.Errorf("error should name --format:\n%s", errOut)
	}
	if !strings.Contains(errOut, "yaml, json5") {
		t.Errorf("error should name the valid values:\n%s", errOut)
	}
	for _, name := range []string{"tasks.yml", "tasks.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			t.Errorf("a rejected --format should not create %s", name)
		}
	}
}

// TestInitNextStepsDefaultOutputStaysBare pins the common case: the
// scaffold lands on defaultTaskFileCandidates[0], so the bare commands
// the block prints probe their way straight to it and naming the file
// would be noise. The literal lines are asserted because the padding is
// what keeps the three comments in one column.
func TestInitNextStepsDefaultOutputStaysBare(t *testing.T) {
	t.Parallel()
	c, ui := newTestInitCommandUi(t.TempDir())
	if exit := c.Run(nil); exit != 0 {
		t.Fatalf("exit = %d, want 0: %s", exit, ui.ErrorWriter.String())
	}

	out := ui.OutputWriter.String()
	for _, want := range []string{
		"  $ docket validate          # offline check",
		"  $ docket plan              # preview against the server",
		"  $ docket apply             # apply",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("next steps missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "--tasks") {
		t.Errorf("tasks.yml is the first probe candidate, so no --tasks is needed:\n%s", out)
	}
}

// TestInitNextStepsNameNonDefaultOutput is the #420 regression: a
// scaffold written anywhere but the first probe candidate has to be named
// in the commands, or `docket validate` silently reports on a stale
// tasks.yml sitting next to it.
func TestInitNextStepsNameNonDefaultOutput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		args []string
		want string
	}{
		{name: "format json5 moves the default path", args: []string{"--format", "json5"}, want: "--tasks tasks.json"},
		{name: "explicit json output", args: []string{"--output", "recipe.json"}, want: "--tasks recipe.json"},
		{name: "another yaml spelling", args: []string{"--output", "tasks.yaml"}, want: "--tasks tasks.yaml"},
		{name: "subdirectory", args: []string{"--output", "staging/tasks.yml"}, want: "--tasks staging/tasks.yml"},
		{name: "path needing shell quotes", args: []string{"--output", "my recipes.yml"}, want: `--tasks 'my recipes.yml'`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, "staging"), 0o755); err != nil {
				t.Fatalf("mkdir staging: %v", err)
			}

			c, ui := newTestInitCommandUi(dir)
			if exit := c.Run(tc.args); exit != 0 {
				t.Fatalf("exit = %d, want 0: %s", exit, ui.ErrorWriter.String())
			}

			out := ui.OutputWriter.String()
			for _, verb := range []string{"validate", "plan", "apply"} {
				line := "  $ docket " + verb + " " + tc.want
				if !strings.Contains(out, line) {
					t.Errorf("next steps missing %q:\n%s", line, out)
				}
			}
		})
	}
}

// TestInitNextStepsCleansRelativeDefault: "./tasks.yml" is the first
// probe candidate spelled differently, so it stays on the bare path.
func TestInitNextStepsCleansRelativeDefault(t *testing.T) {
	t.Parallel()
	c, ui := newTestInitCommandUi(t.TempDir())
	if exit := c.Run([]string{"--output", "./tasks.yml"}); exit != 0 {
		t.Fatalf("exit = %d, want 0: %s", exit, ui.ErrorWriter.String())
	}
	if out := ui.OutputWriter.String(); strings.Contains(out, "--tasks") {
		t.Errorf("./tasks.yml is the default candidate; no --tasks needed:\n%s", out)
	}
}

// TestInitNextStepsCommentsStayAligned: the block is only readable while
// the three comments share a column, and the --tasks suffix must not
// break that.
func TestInitNextStepsCommentsStayAligned(t *testing.T) {
	t.Parallel()
	c, ui := newTestInitCommandUi(t.TempDir())
	if exit := c.Run([]string{"--format", "json5"}); exit != 0 {
		t.Fatalf("exit = %d, want 0: %s", exit, ui.ErrorWriter.String())
	}

	column := -1
	for _, line := range strings.Split(ui.OutputWriter.String(), "\n") {
		if !strings.HasPrefix(line, "  $ ") {
			continue
		}
		at := strings.Index(line, "#")
		if at < 0 {
			t.Fatalf("next-steps line has no comment: %q", line)
		}
		if column == -1 {
			column = at
			continue
		}
		if at != column {
			t.Errorf("comment column = %d, want %d (line %q)", at, column, line)
		}
	}
	if column == -1 {
		t.Fatal("no next-steps lines were printed")
	}
}

// newTestInitCommand wires up a Meta backed by cli.MockUi so c.Ui.* calls
// don't nil-panic during Run. Tests assert via the file system or stdout
// capture; MockUi's buffers are ignored.
func newTestInitCommand(baseDir ...string) *InitCommand {
	c, _ := newTestInitCommandUi(baseDir...)
	return c
}

// newTestInitCommandUi is newTestInitCommand for tests that also need to
// read what the command said - the mismatch warning lands on the UI's
// error buffer, not on stdout.
func newTestInitCommandUi(baseDir ...string) (*InitCommand, *cli.MockUi) {
	ui := cli.NewMockUi()
	c := &InitCommand{}
	c.Meta = command.Meta{Ui: ui}
	if len(baseDir) > 0 {
		c.BaseDir = baseDir[0]
	}
	return c, ui
}

func readAllString(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	return string(b), err
}
