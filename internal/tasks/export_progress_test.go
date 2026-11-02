package tasks

import (
	"reflect"
	"slices"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

// export_progress_test.go covers ExportOptions.Progress, the hook that lets a
// caller say what an export is doing while it reads the server (#670).

// recordProgress runs an export with a Progress hook and returns every event
// it received, in order.
func recordProgress(t *testing.T, responses map[string]string, opts ExportOptions) []ExportProgress {
	t.Helper()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(responses))
	var events []ExportProgress
	opts.Progress = func(ev ExportProgress) { events = append(events, ev) }
	if _, err := ExportRecipe(ctx, opts); err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	return events
}

// scopesOf returns the scope-opening events, and checks every exporter event
// sits under the scope opened before it.
func scopesOf(t *testing.T, events []ExportProgress) []ExportProgress {
	t.Helper()
	var scopes []ExportProgress
	for i, ev := range events {
		if ev.Type == "" {
			scopes = append(scopes, ev)
			continue
		}
		if len(scopes) == 0 {
			t.Fatalf("event %d (%+v) arrived before any scope was opened", i, ev)
		}
		open := scopes[len(scopes)-1]
		if ev.Scope != open.Scope || ev.Index != open.Index || ev.Total != open.Total {
			t.Errorf("event %d (%+v) does not belong to the open scope %+v", i, ev, open)
		}
	}
	return scopes
}

func TestExportProgressReportsEachScopeInOrder(t *testing.T) {
	t.Parallel()
	events := recordProgress(t, exportFixture(), ExportOptions{Inline: true})

	want := []ExportProgress{
		{Scope: "global"},
		{Scope: "app-one", Index: 1, Total: 2},
		{Scope: "app-two", Index: 2, Total: 2},
	}
	if got := scopesOf(t, events); !reflect.DeepEqual(got, want) {
		t.Errorf("scopes = %+v, want %+v", got, want)
	}

	// Every exporter in an app play is reported, in appExportOrder.
	var appOneTypes []string
	for _, ev := range events {
		if ev.Scope == "app-one" && ev.Type != "" {
			appOneTypes = append(appOneTypes, ev.Type)
		}
	}
	if len(appOneTypes) == 0 || appOneTypes[0] != "dokku_app" {
		t.Errorf("app-one exporters should start with dokku_app, got %v", appOneTypes)
	}
	if !slices.Contains(appOneTypes, "dokku_config") {
		t.Errorf("app-one exporters should include dokku_config, got %v", appOneTypes)
	}
}

func TestExportProgressCountsOnlyTheSelectedApps(t *testing.T) {
	t.Parallel()
	events := recordProgress(t, exportFixture(), ExportOptions{Inline: true, Apps: []string{"app-two"}})

	want := []ExportProgress{{Scope: "app-two", Index: 1, Total: 1}}
	if got := scopesOf(t, events); !reflect.DeepEqual(got, want) {
		t.Errorf("scopes = %+v, want %+v (--app skips the global play)", got, want)
	}
}

func TestExportProgressFollowsAResourceFilter(t *testing.T) {
	t.Parallel()
	sel, err := ParseResourceSelectors([]string{"dokku_config[app=app-one]"})
	if err != nil {
		t.Fatalf("ParseResourceSelectors: %v", err)
	}
	events := recordProgress(t, exportFixture(), ExportOptions{Inline: true, Resources: sel})

	want := []ExportProgress{
		{Scope: "app-one", Index: 1, Total: 1},
		{Scope: "app-one", Index: 1, Total: 1, Type: "dokku_config"},
	}
	if !reflect.DeepEqual(events, want) {
		t.Errorf("events = %+v, want %+v", events, want)
	}
}

func TestExportProgressLeavesOutExportersForMissingPlugins(t *testing.T) {
	t.Parallel()
	events := recordProgress(t, withInstalledPlugins(exportFixture(), "acl"), ExportOptions{Inline: true, Apps: []string{"app-one"}})

	var types []string
	for _, ev := range events {
		if ev.Type != "" {
			types = append(types, ev.Type)
		}
	}
	// dokku_acl_app's plugin is installed, so it is read and reported; the
	// http-auth, maintenance and letsencrypt exporters are not run (#672), so
	// nothing says they are about to read the server.
	if !slices.Contains(types, "dokku_acl_app") {
		t.Errorf("progress should report dokku_acl_app, got %v", types)
	}
	for _, skipped := range []string{"dokku_http_auth", "dokku_http_auth_user", "dokku_maintenance", "dokku_maintenance_custom_page", "dokku_letsencrypt", "dokku_letsencrypt_property"} {
		if slices.Contains(types, skipped) {
			t.Errorf("progress reported %s, whose plugin is not installed: %v", skipped, types)
		}
	}
}
