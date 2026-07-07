package tasks

import (
	"context"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

// fakeDokku returns an exec runner that answers dokku invocations from a map
// keyed by the space-joined args, so export tests never spawn a process or
// contact a server. Unlisted commands return empty stdout.
func fakeDokku(responses map[string]string) func(context.Context, subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
	return func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		return subprocess.ExecCommandResponse{Stdout: responses[strings.Join(in.Args, " ")]}, nil
	}
}

func exportFixture() map[string]string {
	return map[string]string{
		"--quiet apps:list":                           "app-one\napp-two",
		"--quiet config:export --format json app-one": `{"SECRET_KEY":"s3cr3t","LOG_LEVEL":"info"}`,
		"domains:report app-one --domains-app-vhosts": "app-one.example.com www.example.com",
		"--quiet config:export --format json app-two": `{}`,
		"domains:report app-two --domains-app-vhosts": "",
	}
}

func TestAppExportOrderIsValid(t *testing.T) {
	for _, key := range appExportOrder {
		proto, ok := RegisteredTasks[key]
		if !ok {
			t.Errorf("appExportOrder has unknown task key %q", key)
			continue
		}
		if _, ok := proto.(AppExporter); !ok {
			t.Errorf("task %q in appExportOrder does not implement AppExporter", key)
		}
	}
}

func TestExportRecipeFileMode(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(exportFixture()))()

	res, err := ExportRecipe(ExportOptions{})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}

	// Two apps -> two plays.
	if len(res.plays) != 2 {
		t.Fatalf("expected 2 plays, got %d", len(res.plays))
	}

	// Config values are lifted into the vars map, not left in the recipe.
	if got := res.Vars["app_one_SECRET_KEY"]; got != "s3cr3t" {
		t.Errorf("vars[app_one_SECRET_KEY] = %q, want s3cr3t", got)
	}
	if got := res.Vars["app_one_LOG_LEVEL"]; got != "info" {
		t.Errorf("vars[app_one_LOG_LEVEL] = %q, want info", got)
	}

	recipe, err := res.MarshalRecipe("yaml")
	if err != nil {
		t.Fatalf("MarshalRecipe: %v", err)
	}
	out := string(recipe)

	// The recipe references the lifted values via inputs, and never contains
	// the raw secret.
	if strings.Contains(out, "s3cr3t") {
		t.Errorf("recipe leaked a secret value:\n%s", out)
	}
	for _, want := range []string{
		"{{ .app_one_SECRET_KEY }}",
		"name: app-one",
		"dokku_app",
		"dokku_config",
		"dokku_domains",
		"app-one.example.com",
		"name: app_one_SECRET_KEY",
		"name: app-two",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("recipe missing %q:\n%s", want, out)
		}
	}

	// Both apps get a dokku_app task (the ":" avoids matching dokku_app_lock).
	if strings.Count(out, "dokku_app:") != 2 {
		t.Errorf("expected dokku_app in both plays:\n%s", out)
	}
}

func TestExportRecipeRedactBlanksVars(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(exportFixture()))()

	res, err := ExportRecipe(ExportOptions{Redact: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if v, ok := res.Vars["app_one_SECRET_KEY"]; !ok || v != "" {
		t.Errorf("redacted vars[app_one_SECRET_KEY] = %q (ok=%v), want empty and present", v, ok)
	}

	recipe, _ := res.MarshalRecipe("yaml")
	if !strings.Contains(string(recipe), "{{ .app_one_SECRET_KEY }}") {
		t.Errorf("redacted recipe should still reference the input:\n%s", recipe)
	}
}

func TestExportRecipeInlineKeepsValues(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(exportFixture()))()

	res, err := ExportRecipe(ExportOptions{Inline: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if res.HasVars() {
		t.Errorf("inline mode should not lift any vars, got %v", res.Vars)
	}
	recipe, _ := res.MarshalRecipe("yaml")
	out := string(recipe)
	if !strings.Contains(out, "s3cr3t") {
		t.Errorf("inline recipe should contain the real value:\n%s", out)
	}
	if strings.Contains(out, "{{ .") {
		t.Errorf("inline recipe should not use input templates:\n%s", out)
	}
}

func TestExportRecipeAppFilter(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(exportFixture()))()

	res, err := ExportRecipe(ExportOptions{Apps: []string{"app-two"}})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if len(res.plays) != 1 {
		t.Fatalf("expected 1 play with --app filter, got %d", len(res.plays))
	}
	recipe, _ := res.MarshalRecipe("yaml")
	if strings.Contains(string(recipe), "app-one") {
		t.Errorf("filtered export should not include app-one:\n%s", recipe)
	}
}
