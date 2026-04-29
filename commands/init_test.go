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
	c := &InitCommand{}
	if c.Name() != "init" {
		t.Errorf("Name = %q, want %q", c.Name(), "init")
	}
	if c.Synopsis() == "" {
		t.Error("Synopsis must not be empty")
	}
}

func TestInitCommandExamples(t *testing.T) {
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
	c := &InitCommand{}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("FlagSet panicked: %v", r)
		}
	}()
	_ = c.FlagSet()
}

func TestInitRendersDefaultTemplate(t *testing.T) {
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
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte("preserved\n"), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	c := newTestInitCommand()
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
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte("preserved\n"), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	c := newTestInitCommand()
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
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")

	c := newTestInitCommand()
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
	out, err := renderInit(initOptions{Name: "billing"})
	if err != nil {
		t.Fatalf("renderInit: %v", err)
	}
	if !strings.Contains(string(out), "default: billing") {
		t.Errorf("--name not propagated to app input default:\n%s", out)
	}
}

func TestInitRepoFlagSetsRepoDefault(t *testing.T) {
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
	dir := filepath.Join(t.TempDir(), "widget-svc")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Chdir(dir)

	if got := defaultName(); got != "widget-svc" {
		t.Errorf("defaultName() = %q, want %q", got, "widget-svc")
	}
}

func TestInitDefaultRepoFromGitConfig(t *testing.T) {
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
	t.Chdir(dir)

	if got := defaultRepo(); got != "git@example.com:owner/repo.git" {
		t.Errorf("defaultRepo() = %q, want git@example.com:owner/repo.git", got)
	}
}

func TestInitNoGitConfigYieldsEmptyRepo(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if got := defaultRepo(); got != "" {
		t.Errorf("defaultRepo() with no .git/config = %q, want empty", got)
	}
}

func TestInitGitConfigWithoutOriginYieldsEmptyRepo(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := "[core]\n\trepositoryformatversion = 0\n[remote \"upstream\"]\n\turl = git@example.com:upstream/repo.git\n"
	if err := os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(cfg), 0o644); err != nil {
		t.Fatalf("write git config: %v", err)
	}
	t.Chdir(dir)

	if got := defaultRepo(); got != "" {
		t.Errorf("defaultRepo() with no origin = %q, want empty", got)
	}
}

func TestInitDefaultPassesValidate(t *testing.T) {
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

func TestInitMinimalPassesValidate(t *testing.T) {
	out, err := renderInit(initOptions{Name: "demo", Minimal: true})
	if err != nil {
		t.Fatalf("renderInit: %v", err)
	}
	if problems := tasks.Validate(out, tasks.ValidateOptions{}); len(problems) != 0 {
		t.Fatalf("minimal scaffold should pass validate, got %+v", problems)
	}
}

func TestInitDefaultJSON5PassesValidate(t *testing.T) {
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
	out, err := renderInit(initOptions{Name: "demo", Minimal: true, Format: tasks.FormatNameJSON5})
	if err != nil {
		t.Fatalf("renderInit (minimal json5): %v", err)
	}
	if problems := tasks.Validate(out, tasks.ValidateOptions{Format: tasks.FormatNameJSON5}); len(problems) != 0 {
		t.Fatalf("minimal JSON5 scaffold should pass validate, got %+v", problems)
	}
}

func TestInitJSON5HasNoYAMLDocumentMarker(t *testing.T) {
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
	dir := t.TempDir()
	t.Chdir(dir)

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	c := newTestInitCommand()
	exitCh := make(chan int, 1)
	go func() {
		exitCh <- c.Run([]string{"--output", "-", "--name", "demo"})
		w.Close()
	}()

	captured, err := readAllString(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	if exit := <-exitCh; exit != 0 {
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

// newTestInitCommand wires up a Meta backed by cli.MockUi so c.Ui.* calls
// don't nil-panic during Run. Tests assert via the file system or stdout
// capture; MockUi's buffers are ignored.
func newTestInitCommand() *InitCommand {
	c := &InitCommand{}
	c.Meta = command.Meta{Ui: cli.NewMockUi()}
	return c
}

func readAllString(r io.Reader) (string, error) {
	b, err := io.ReadAll(r)
	return string(b), err
}
