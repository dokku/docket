package tasks

import (
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

// exportResource runs ExportRecipe restricted to the given addresses against
// the shared fixture and returns the marshalled recipe plus the report.
func exportResource(t *testing.T, addresses ...string) (string, ExportReport) {
	t.Helper()
	selectors, err := ParseResourceSelectors(addresses)
	if err != nil {
		t.Fatalf("ParseResourceSelectors(%v): %v", addresses, err)
	}
	res, err := ExportRecipe(ExportOptions{Resources: selectors, Inline: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	recipe, err := res.MarshalRecipe("yaml")
	if err != nil {
		t.Fatalf("MarshalRecipe: %v", err)
	}
	return string(recipe), res.Report
}

// TestExportResourceSelectsOneResource covers the headline case the issue asks
// for: reading back a single resource instead of a whole app play.
func TestExportResourceSelectsOneResource(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(exportFixture()))()

	out, report := exportResource(t, "dokku_config[app=app-one]")

	if !strings.Contains(out, "dokku_config") {
		t.Errorf("expected the addressed task in the recipe; got:\n%s", out)
	}
	if strings.Contains(out, "dokku_domains") {
		t.Errorf("expected only the addressed task type; got:\n%s", out)
	}
	if strings.Contains(out, "app-two") {
		t.Errorf("expected only the addressed app; got:\n%s", out)
	}
	if len(report.MissingResources) > 0 {
		t.Errorf("unexpected unmatched addresses: %v", report.MissingResources)
	}
}

// TestExportResourceBareTypeSelectsEveryApp covers the wildcard form: an
// address with no keys means every resource of that type, wherever it lives.
func TestExportResourceBareTypeSelectsEveryApp(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(exportFixture()))()

	out, report := exportResource(t, "dokku_domains")

	if !strings.Contains(out, "dokku_domains") {
		t.Errorf("expected the addressed task type; got:\n%s", out)
	}
	if strings.Contains(out, "dokku_config") {
		t.Errorf("expected only the addressed task type; got:\n%s", out)
	}
	if !strings.Contains(out, "app-one") {
		t.Errorf("expected app-one's domains; got:\n%s", out)
	}
	if len(report.MissingResources) > 0 {
		t.Errorf("unexpected unmatched addresses: %v", report.MissingResources)
	}
}

// TestExportResourceCombinesAddresses covers several addresses in one run,
// including two apps, and asserts each contributes its own play.
func TestExportResourceCombinesAddresses(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(exportFixture()))()

	out, _ := exportResource(t, "dokku_config[app=app-one]", "dokku_domains[app=app-one]")

	for _, want := range []string{"dokku_config", "dokku_domains", "app-one"} {
		if !strings.Contains(out, want) {
			t.Errorf("expected %q in the recipe; got:\n%s", want, out)
		}
	}
	if strings.Contains(out, "app-two") {
		t.Errorf("app-two was not addressed; got:\n%s", out)
	}
}

// TestExportResourceReportsUnmatchedAddress asserts an address the server has
// nothing for is reported rather than silently exporting nothing, the same
// contract --app has for a nonexistent app (#346).
func TestExportResourceReportsUnmatchedAddress(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(exportFixture()))()

	_, report := exportResource(t, "dokku_config[app=app-one]", "dokku_domains[app=app-two]")

	if len(report.MissingResources) != 1 || report.MissingResources[0] != "dokku_domains[app=app-two]" {
		t.Errorf("MissingResources = %v, want [dokku_domains[app=app-two]]", report.MissingResources)
	}
}

// TestExportResourceGlobalScope asserts a global-scoped address emits the
// leading global play and no app plays at all - distinct from "no app
// restriction", which would enumerate every app.
func TestExportResourceGlobalScope(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet apps:list": "app-one",
		"--quiet plugin:list --format json": `[
			{"name":"redis","core":false,"source_url":"https://github.com/dokku/dokku-redis.git","committish":"","branch":""}
		]`,
	}))()

	out, report := exportResource(t, "dokku_plugin[name=redis]")

	if !strings.Contains(out, "dokku_plugin") {
		t.Errorf("expected the global task; got:\n%s", out)
	}
	if strings.Contains(out, "app-one") {
		t.Errorf("a global-scoped address must not emit app plays; got:\n%s", out)
	}
	if len(report.MissingResources) > 0 {
		t.Errorf("unexpected unmatched addresses: %v", report.MissingResources)
	}
}

// TestParseResourceSelectorsRejectsBadAddresses covers the validation that
// runs before the export contacts the server, so a typo fails instantly.
func TestParseResourceSelectorsRejectsBadAddresses(t *testing.T) {
	for _, tt := range []struct {
		name    string
		address string
		wantErr string
	}{
		{
			name:    "unknown task type suggests a near miss",
			address: "dokku_confg[app=api]",
			wantErr: `did you mean "dokku_config"`,
		},
		{
			name:    "a task no exporter reaches",
			address: "dokku_git_auth[host=github.com]",
			wantErr: "cannot be exported",
		},
		{
			name:    "a key the task does not declare",
			address: "dokku_config[application=api]",
			wantErr: "is not an identity key of dokku_config",
		},
		{
			name:    "malformed address",
			address: "dokku_config[app]",
			wantErr: "is not key=value",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseResourceSelectors([]string{tt.address})
			if err == nil {
				t.Fatalf("expected an error containing %q, got none", tt.wantErr)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error = %v, want it to contain %q", err, tt.wantErr)
			}
		})
	}
}

// TestParseResourceSelectorsAcceptsValidAddresses is the positive half: every
// exportable form parses, including a bare type key and a global scope.
func TestParseResourceSelectorsAcceptsValidAddresses(t *testing.T) {
	selectors, err := ParseResourceSelectors([]string{
		"dokku_config",
		"dokku_config[app=api]",
		"dokku_apps_property[global=true,property=disable-autocreation]",
	})
	if err != nil {
		t.Fatalf("ParseResourceSelectors: %v", err)
	}
	if len(selectors) != 3 {
		t.Fatalf("got %d selectors, want 3", len(selectors))
	}
	if len(selectors[0].Keys) != 0 {
		t.Errorf("a bare type key should pin no keys, got %v", selectors[0].Keys)
	}
	if selectors[2].Keys["global"] != "true" {
		t.Errorf("global key = %q, want true", selectors[2].Keys["global"])
	}
}

// TestExportedRecipeLoadsWithoutNameCollisions closes the loop on the change:
// export emits recipes with no `name:` on any task, so every task in one is
// auto-named. If two tasks in a play resolved to the same generated name and
// nothing disambiguated them, the loader would reject its own export.
func TestExportedRecipeLoadsWithoutNameCollisions(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(exportFixture()))()

	res, err := ExportRecipe(ExportOptions{Inline: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	recipe, err := res.MarshalRecipe("yaml")
	if err != nil {
		t.Fatalf("MarshalRecipe: %v", err)
	}

	plays, err := GetPlays(recipe, map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("exported recipe failed to load: %v\n%s", err, recipe)
	}
	for _, play := range plays {
		for _, name := range play.Tasks.Keys() {
			if name == "" {
				t.Errorf("play %q has an unnamed task", play.Name)
			}
		}
	}
}
