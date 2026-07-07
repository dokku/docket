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
