package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/dokku/docket/subprocess"
)

// exportResource runs ExportRecipe restricted to the given addresses against
// the shared fixture and returns the marshalled recipe plus the report.
func exportResource(t *testing.T, ctx context.Context, addresses ...string) (string, ExportReport) {
	t.Helper()
	selectors, err := ParseResourceSelectors(addresses)
	if err != nil {
		t.Fatalf("ParseResourceSelectors(%v): %v", addresses, err)
	}
	res, err := ExportRecipe(ctx, ExportOptions{Resources: selectors, Inline: true})
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
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(exportFixture()))

	out, report := exportResource(t, ctx, "dokku_config[app=app-one]")

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
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(exportFixture()))

	out, report := exportResource(t, ctx, "dokku_domains")

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
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(exportFixture()))

	out, _ := exportResource(t, ctx, "dokku_config[app=app-one]", "dokku_domains[app=app-one]")

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
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(exportFixture()))

	_, report := exportResource(t, ctx, "dokku_config[app=app-one]", "dokku_domains[app=app-two]")

	if len(report.MissingResources) != 1 || report.MissingResources[0] != "dokku_domains[app=app-two]" {
		t.Errorf("MissingResources = %v, want [dokku_domains[app=app-two]]", report.MissingResources)
	}
}

// TestExportResourceGlobalScope asserts a global-scoped address emits the
// leading global play and no app plays at all - distinct from "no app
// restriction", which would enumerate every app.
func TestExportResourceGlobalScope(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet apps:list": "app-one",
		"--quiet plugin:list --format json": `[
			{"name":"redis","core":false,"source_url":"https://github.com/dokku/dokku-redis.git","committish":"","branch":""}
		]`,
	}))

	out, report := exportResource(t, ctx, "dokku_plugin[name=redis]")

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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(exportFixture()))

	res, err := ExportRecipe(ctx, ExportOptions{Inline: true})
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

// TestExportResourceGlobalAddressSurvivesAppNarrowing is the regression lock on
// #518. `--app foo` alone means "this app, not the server", so the global play
// is skipped - but an address that names a global resource is asking for it by
// name, and the global play is the only place it can come from. The two
// together used to export nothing and report the address as missing from a
// server that had it.
func TestExportResourceGlobalAddressSurvivesAppNarrowing(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet apps:list": "app-one",
		"--quiet plugin:list --format json": `[
			{"name":"redis","core":false,"source_url":"https://github.com/dokku/dokku-redis.git","committish":"","branch":""}
		]`,
	}))
	selectors, err := ParseResourceSelectors([]string{"dokku_plugin[name=redis]"})
	if err != nil {
		t.Fatalf("ParseResourceSelectors: %v", err)
	}

	res, err := ExportRecipe(ctx, ExportOptions{Apps: []string{"app-one"}, Resources: selectors, Inline: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	recipe, err := res.MarshalRecipe("yaml")
	if err != nil {
		t.Fatalf("MarshalRecipe: %v", err)
	}

	if !strings.Contains(string(recipe), "dokku_plugin") {
		t.Errorf("the addressed global resource is missing from the recipe; got:\n%s", recipe)
	}
	if len(res.Report.MissingResources) > 0 {
		t.Errorf("address reported as unmatched against a server that has it: %v", res.Report.MissingResources)
	}
	// --app still keeps the run off other apps: the address named no app-scoped
	// resource, so no app play should be emitted at all.
	if strings.Contains(string(recipe), "name: app-one") {
		t.Errorf("a global-only address must not emit app plays; got:\n%s", recipe)
	}
}

// pinnedRunner answers from responses, except that any command naming one of
// the missing apps fails the way dokku does for an app that does not exist - a
// completed command with a non-zero exit - including `apps:exists`. It records
// every command it sees so a test can assert which round trips were made.
type pinnedRunner struct {
	responses map[string]string
	missing   map[string]bool

	mu    sync.Mutex
	calls []string
}

func (r *pinnedRunner) run(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
	args := strings.Join(in.Args, " ")
	r.mu.Lock()
	r.calls = append(r.calls, args)
	r.mu.Unlock()
	for _, arg := range in.Args {
		if r.missing[arg] {
			resp := subprocess.ExecCommandResponse{ExitCode: 1, Stderr: fmt.Sprintf(" !     App %s does not exist", arg)}
			return resp, &subprocess.ExecError{Response: resp, Err: fmt.Errorf("App %s does not exist", arg), Ran: true}
		}
	}
	return subprocess.ExecCommandResponse{Stdout: r.responses[args]}, nil
}

// count returns how many recorded commands contain substr.
func (r *pinnedRunner) count(substr string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, call := range r.calls {
		if strings.Contains(call, substr) {
			n++
		}
	}
	return n
}

// exportPinned runs ExportRecipe restricted to addresses against runner and
// returns the result.
func exportPinned(t *testing.T, runner *pinnedRunner, addresses ...string) *ExportResult {
	t.Helper()
	selectors, err := ParseResourceSelectors(addresses)
	if err != nil {
		t.Fatalf("ParseResourceSelectors(%v): %v", addresses, err)
	}
	ctx := subprocess.ContextWithRunner(testCtx(), runner.run)
	res, err := ExportRecipe(ctx, ExportOptions{Resources: selectors, Inline: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	return res
}

// TestExportResourcePinnedSkipsAppsList covers #567: an address that pins its
// app already says which app to read, so the export neither lists every app on
// the server nor probes the one it was given when its read succeeds.
func TestExportResourcePinnedSkipsAppsList(t *testing.T) {
	t.Parallel()
	runner := &pinnedRunner{responses: exportFixture()}

	res := exportPinned(t, runner, "dokku_config[app=app-one]")

	if n := runner.count("apps:list"); n != 0 {
		t.Errorf("apps:list ran %d times, want 0", n)
	}
	if n := runner.count("apps:exists"); n != 0 {
		t.Errorf("apps:exists ran %d times, want 0", n)
	}
	if len(res.Plays()) != 1 || res.Plays()[0].Name != "app-one" {
		t.Errorf("plays = %+v, want one app-one play", res.Plays())
	}
	if len(res.Report.MissingResources) > 0 {
		t.Errorf("unexpected unmatched addresses: %v", res.Report.MissingResources)
	}
}

// TestExportResourcePinnedMissingAppReportsAddress asserts a pinned app that
// does not exist comes back the way it did when apps:list filtered it out: as
// the address the user typed, with no play and none of the warnings its failed
// reads raised.
func TestExportResourcePinnedMissingAppReportsAddress(t *testing.T) {
	t.Parallel()
	runner := &pinnedRunner{responses: exportFixture(), missing: map[string]bool{"ghost": true}}

	res := exportPinned(t, runner, "dokku_config[app=ghost]")

	if got := res.Report.MissingResources; len(got) != 1 || got[0] != "dokku_config[app=ghost]" {
		t.Errorf("MissingResources = %v, want [dokku_config[app=ghost]]", got)
	}
	if len(res.Report.MissingApps) > 0 {
		t.Errorf("MissingApps = %v, want none: the user named an address, not an app", res.Report.MissingApps)
	}
	if len(res.Report.Warnings) > 0 {
		t.Errorf("Warnings = %v, want none for an app that does not exist", res.Report.Warnings)
	}
	if len(res.Plays()) > 0 {
		t.Errorf("plays = %+v, want none", res.Plays())
	}
	if n := runner.count("apps:list"); n != 0 {
		t.Errorf("apps:list ran %d times, want 0", n)
	}
	if n := runner.count("apps:exists ghost"); n != 1 {
		t.Errorf("apps:exists ghost ran %d times, want 1", n)
	}
}

// TestExportResourcePinnedAppAddressProbesExistence covers dokku_app, whose
// exporter reads nothing and always returns a body: apps:exists is its read, so
// a missing app is not exported as though it existed.
func TestExportResourcePinnedAppAddressProbesExistence(t *testing.T) {
	t.Parallel()
	runner := &pinnedRunner{responses: exportFixture(), missing: map[string]bool{"ghost": true}}

	res := exportPinned(t, runner, "dokku_app[app=app-one]", "dokku_app[app=ghost]")

	if len(res.Plays()) != 1 || res.Plays()[0].Name != "app-one" {
		t.Fatalf("plays = %+v, want one app-one play", res.Plays())
	}
	if _, ok := As[AppTask](res.Plays()[0].Tasks[0]); !ok {
		t.Errorf("app-one play task = %+v, want a dokku_app body", res.Plays()[0].Tasks[0])
	}
	if got := res.Report.MissingResources; len(got) != 1 || got[0] != "dokku_app[app=ghost]" {
		t.Errorf("MissingResources = %v, want [dokku_app[app=ghost]]", got)
	}
	if n := runner.count("apps:list"); n != 0 {
		t.Errorf("apps:list ran %d times, want 0", n)
	}
	for _, app := range []string{"app-one", "ghost"} {
		if n := runner.count("apps:exists " + app); n != 1 {
			t.Errorf("apps:exists %s ran %d times, want 1", app, n)
		}
	}
}

// TestExportResourcePinnedKeepsWarningsForExistingApp asserts a read that fails
// on an app that does exist still surfaces its warning: only a missing app's
// failures are dropped.
func TestExportResourcePinnedKeepsWarningsForExistingApp(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		if strings.Join(in.Args, " ") == "--quiet config:export --format json app-one" {
			resp := subprocess.ExecCommandResponse{ExitCode: 1}
			return resp, &subprocess.ExecError{Response: resp, Err: errors.New("config plugin broke"), Ran: true}
		}
		return subprocess.ExecCommandResponse{}, nil
	})
	selectors, err := ParseResourceSelectors([]string{"dokku_config[app=app-one]"})
	if err != nil {
		t.Fatalf("ParseResourceSelectors: %v", err)
	}

	res, err := ExportRecipe(ctx, ExportOptions{Resources: selectors, Inline: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}

	if len(res.Report.Warnings) != 1 || !strings.Contains(res.Report.Warnings[0], "config plugin broke") {
		t.Errorf("Warnings = %v, want the config:export failure", res.Report.Warnings)
	}
	if got := res.Report.MissingResources; len(got) != 1 || got[0] != "dokku_config[app=app-one]" {
		t.Errorf("MissingResources = %v, want [dokku_config[app=app-one]]", got)
	}
}

// TestExportResourcePinnedProbeFailureIsFatal asserts an apps:exists probe that
// could not run fails the export, the way an apps:list that could not run
// does, rather than reading as "the app does not exist".
func TestExportResourcePinnedProbeFailureIsFatal(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		if strings.Contains(strings.Join(in.Args, " "), "apps:exists") {
			return subprocess.ExecCommandResponse{}, &subprocess.ExecError{Err: errors.New("dokku: not found")}
		}
		return subprocess.ExecCommandResponse{}, nil
	})
	selectors, err := ParseResourceSelectors([]string{"dokku_app[app=app-one]"})
	if err != nil {
		t.Fatalf("ParseResourceSelectors: %v", err)
	}

	res, err := ExportRecipe(ctx, ExportOptions{Resources: selectors, Inline: true})
	if err == nil {
		t.Fatalf("ExportRecipe succeeded, want the probe failure")
	}
	if !strings.Contains(err.Error(), `checking app "app-one" exists`) {
		t.Errorf("error = %q, want it to name the probe", err)
	}
	if res == nil {
		t.Errorf("result is nil on error; the global play and its secrets must survive (#488)")
	}
}

// TestExportResourceBareTypeStillListsApps asserts an address that pins no app
// still enumerates every app: only pinned addresses skip apps:list.
func TestExportResourceBareTypeStillListsApps(t *testing.T) {
	t.Parallel()
	runner := &pinnedRunner{responses: exportFixture()}

	exportPinned(t, runner, "dokku_domains")

	if n := runner.count("apps:list"); n != 1 {
		t.Errorf("apps:list ran %d times, want 1", n)
	}
}

// TestExportResourceMixedGlobalAndPinnedSkipsAppsList asserts a global address
// beside a pinned one exports both without listing apps: a global address
// never decides which apps are read.
func TestExportResourceMixedGlobalAndPinnedSkipsAppsList(t *testing.T) {
	t.Parallel()
	responses := exportFixture()
	responses["--quiet plugin:list --format json"] = `[
		{"name":"redis","core":false,"source_url":"https://github.com/dokku/dokku-redis.git","committish":"","branch":""}
	]`
	runner := &pinnedRunner{responses: responses}

	res := exportPinned(t, runner, "dokku_plugin[name=redis]", "dokku_config[app=app-one]")

	var names []string
	for _, play := range res.Plays() {
		names = append(names, play.Name)
	}
	if strings.Join(names, ",") != "global,app-one" {
		t.Errorf("plays = %v, want [global app-one]", names)
	}
	if n := runner.count("apps:list"); n != 0 {
		t.Errorf("apps:list ran %d times, want 0", n)
	}
	if len(res.Report.MissingResources) > 0 {
		t.Errorf("unexpected unmatched addresses: %v", res.Report.MissingResources)
	}
}
