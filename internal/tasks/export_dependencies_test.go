package tasks

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

// dependencyFixture is a server with two apps that share nothing: api links
// postgres db, which attaches to svc-net, and attaches to app-net and
// shared-net itself; worker links postgres other and redis cache. Every global
// type an --app export never needs is answered too, so a test can tell an
// exporter that was skipped from one that read nothing.
func dependencyFixture() map[string]string {
	return mergeFixtures(
		map[string]string{
			"--quiet apps:list":                        "api\nworker",
			"--quiet plugin:trigger service-list":      "postgres:db\npostgres:other\nredis:cache",
			"--quiet postgres:links db":                "api",
			"--quiet postgres:links other":             "worker",
			"--quiet redis:links cache":                "worker",
			"--quiet postgres:info db --exposed-ports": "5432->5433",
			"--quiet acl:list-service postgres db":     "alice",
			"--quiet acl:list-service postgres other":  "bob",
			"--quiet network:report api --format json": `{"attach-post-deploy":"app-net,shared-net"}`,
			"--quiet network:list --format json": `[
				{"Name":"app-net","DokkuManaged":true},
				{"Name":"shared-net","DokkuManaged":true},
				{"Name":"svc-net","DokkuManaged":true},
				{"Name":"unrelated-net","DokkuManaged":true}
			]`,
			"--quiet plugin:list --format json": `[
				{"name":"postgres","core":false,"source_url":"https://github.com/dokku/dokku-postgres.git","committish":"abc123","branch":""},
				{"name":"redis","core":false,"source_url":"https://github.com/dokku/dokku-redis.git","committish":"def456","branch":""},
				{"name":"acl","core":false,"source_url":"https://github.com/dokku-community/dokku-acl.git","committish":"789abc","branch":""},
				{"name":"unrelated","core":false,"source_url":"https://example.com/unrelated.git","committish":"","branch":""}
			]`,
			"--quiet git:report --global --format json": `{"global-deploy-branch":"main"}`,
		},
		infoFixture("postgres", "db", map[string]string{
			"version":             "postgres:16",
			"post-create-network": "svc-net",
			"restart-policy":      "always",
			"backup-schedule":     "0 3 * * *",
			"backup-bucket":       "db-backups",
		}),
		infoFixture("postgres", "other", map[string]string{
			"version":        "postgres:16",
			"custom-env":     "OTHER_SECRET=hunter2",
			"restart-policy": "always",
		}),
		infoFixture("redis", "cache", map[string]string{"version": "redis:7"}),
	)
}

// globalAddresses returns the identity address of every task in the export's
// global play, sorted, or nil when there is no global play.
func globalAddresses(t *testing.T, res *ExportResult) []string {
	t.Helper()
	plays := res.Plays()
	if len(plays) == 0 || plays[0].Name != "global" {
		return nil
	}
	var out []string
	for _, task := range plays[0].Tasks {
		out = append(out, IdentityAddress(task.Type, task.Body.(Task)))
	}
	sort.Strings(out)
	return out
}

func TestExportAppCarriesItsDependencies(t *testing.T) {
	t.Parallel()
	var calls []string
	ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(dependencyFixture(), &calls))

	res, err := ExportRecipe(ctx, ExportOptions{Apps: []string{"api"}})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}

	got := globalAddresses(t, res)
	want := []string{
		"dokku_acl_service[service=db,type=postgres]",
		"dokku_network[name=app-net]",
		"dokku_network[name=shared-net]",
		// named by the linked service, not by the app
		"dokku_network[name=svc-net]",
		"dokku_plugin[name=acl]",
		"dokku_plugin[name=postgres]",
		"dokku_service_backup[service=postgres,name=db]",
		"dokku_service_create[service=postgres,name=db]",
		"dokku_service_expose[service=postgres,name=db]",
		"dokku_service_property[service=postgres,name=db,property=restart-policy]",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("global play = %v\nwant %v", got, want)
	}
	if res.AppCount() != 1 || res.Plays()[1].Name != "api" {
		t.Errorf("app plays = %v, want just api after the global play", res.Plays())
	}

	// Exporters no dependency can reach are never run.
	for _, call := range calls {
		for _, skipped := range []string{"--global", "ssh-keys", "storage:list-entries"} {
			if strings.Contains(call, skipped) {
				t.Errorf("an --app export ran %q, which no dependency needs", call)
			}
		}
	}

	// The other service was read and dropped, but its secret is still
	// registered, since the warnings beside the export print after it.
	if !containsString(res.SensitiveValues(), "hunter2") {
		t.Errorf("SensitiveValues() = %v, want the dropped service's custom-env", res.SensitiveValues())
	}
	for name := range res.Vars {
		if strings.Contains(name, "OTHER_SECRET") {
			t.Errorf("vars carry %q from a service the app does not link", name)
		}
	}
}

func TestExportAppDependenciesFollowEachLink(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(dependencyFixture()))

	res, err := ExportRecipe(ctx, ExportOptions{Apps: []string{"worker"}})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	got := globalAddresses(t, res)
	want := []string{
		"dokku_acl_service[service=other,type=postgres]",
		"dokku_plugin[name=acl]",
		"dokku_plugin[name=postgres]",
		"dokku_plugin[name=redis]",
		"dokku_service_create[service=postgres,name=other]",
		"dokku_service_create[service=redis,name=cache]",
		"dokku_service_property[service=postgres,name=other,property=restart-policy]",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("global play = %v\nwant %v", got, want)
	}
}

func TestExportAppSkipDependencies(t *testing.T) {
	t.Parallel()
	var calls []string
	ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(dependencyFixture(), &calls))

	res, err := ExportRecipe(ctx, ExportOptions{Apps: []string{"api"}, SkipDependencies: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if got := globalAddresses(t, res); got != nil {
		t.Errorf("SkipDependencies exported a global play: %v", got)
	}
	assertNoGlobalReads(t, calls, "SkipDependencies still read")
}

func TestExportAppWithoutDependenciesReadsNoGlobals(t *testing.T) {
	t.Parallel()
	var calls []string
	ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(exportFixture(), &calls))

	res, err := ExportRecipe(ctx, ExportOptions{Apps: []string{"app-one"}})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if got := globalAddresses(t, res); got != nil {
		t.Errorf("an app with no dependencies exported a global play: %v", got)
	}
	assertNoGlobalReads(t, calls, "an app with no dependencies read")
}

// assertNoGlobalReads fails when calls show a global exporter ran. network:list
// is dokku_network's read. plugin:list --format json is read once by the
// installed-plugin check the app play makes before an exporter that needs a
// plugin (#672), so a second read is dokku_plugin's exporter.
func assertNoGlobalReads(t *testing.T, calls []string, prefix string) {
	t.Helper()
	pluginLists := 0
	for _, call := range calls {
		if strings.Contains(call, "network:list") {
			t.Errorf("%s %q", prefix, call)
		}
		if strings.Contains(call, "plugin:list --format json") {
			pluginLists++
		}
	}
	if pluginLists > 1 {
		t.Errorf("%s plugin:list --format json %d times, want at most once for the installed-plugin check", prefix, pluginLists)
	}
}

func TestExportMissingAppHasNoDependencies(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(dependencyFixture()))

	res, err := ExportRecipe(ctx, ExportOptions{Apps: []string{"nope"}})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if res.PlayCount() != 0 {
		t.Errorf("PlayCount = %d, want 0 for a missing app", res.PlayCount())
	}
	if !reflect.DeepEqual(res.Report.MissingApps, []string{"nope"}) {
		t.Errorf("MissingApps = %v, want [nope]", res.Report.MissingApps)
	}
}

func TestExportAppDependenciesProgressOrder(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(dependencyFixture()))

	var scopes []string
	_, err := ExportRecipe(ctx, ExportOptions{Apps: []string{"api"}, Progress: func(ev ExportProgress) {
		if ev.Type == "" {
			scopes = append(scopes, ev.Scope)
		}
	}})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	// The dependencies are only known once the app is read.
	if want := []string{"api", "global"}; !reflect.DeepEqual(scopes, want) {
		t.Errorf("progress scopes = %v, want %v", scopes, want)
	}
}

func TestExportDependenciesFromAppTasks(t *testing.T) {
	t.Parallel()
	deps := newExportDependencies([]ExportedPlay{{Name: "api", Tasks: []ExportedTask{
		{Type: "dokku_storage_mount", Body: StorageMountTask{App: "api", EntryName: "single"}},
		{Type: "dokku_storage_mount", Body: StorageMountTask{App: "api", Mounts: []StorageMount{
			{EntryName: "listed", ContainerDir: "/a"},
			{HostDir: "/var/legacy", ContainerDir: "/b"},
		}}},
		{Type: "dokku_network_property", Body: NetworkPropertyTask{App: "api", Property: "initial-network", Value: "first"}},
		// not a network name
		{Type: "dokku_network_property", Body: NetworkPropertyTask{App: "api", Property: "tld", Value: "svc.cluster.local"}},
		{Type: "dokku_letsencrypt", Body: LetsencryptTask{App: "api"}},
	}}})

	if want := map[string]bool{"single": true, "listed": true}; !reflect.DeepEqual(deps.entries, want) {
		t.Errorf("storage entries = %v, want %v", deps.entries, want)
	}
	if want := map[string]bool{"first": true}; !reflect.DeepEqual(deps.networks, want) {
		t.Errorf("networks = %v, want %v", deps.networks, want)
	}
	if !deps.plugins["letsencrypt"] {
		t.Errorf("plugins = %v, want letsencrypt from the app's letsencrypt task", deps.plugins)
	}
	if !deps.keeps("dokku_storage_entry", StorageEntryTask{Name: "listed"}) || deps.keeps("dokku_storage_entry", StorageEntryTask{Name: "other"}) {
		t.Errorf("storage entries are not kept by name")
	}
	if !deps.wantsType("dokku_storage_entry") || deps.wantsType("dokku_service_create") || deps.wantsType("dokku_ssh_key") {
		t.Errorf("wantsType does not follow the edges found")
	}
}

func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

func TestExportGlobalOnly(t *testing.T) {
	t.Parallel()
	var calls []string
	ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(dependencyFixture(), &calls))

	var scopes []string
	res, err := ExportRecipe(ctx, ExportOptions{GlobalOnly: true, Progress: func(ev ExportProgress) {
		if ev.Type == "" {
			scopes = append(scopes, ev.Scope)
		}
	}})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if res.PlayCount() != 1 || res.AppCount() != 0 {
		t.Fatalf("plays = %v, want the global play alone", res.Plays())
	}
	// Every server-wide resource, not only the ones some app depends on.
	got := globalAddresses(t, res)
	for _, want := range []string{
		"dokku_network[name=unrelated-net]",
		"dokku_plugin[name=unrelated]",
		"dokku_service_create[service=redis,name=cache]",
		"dokku_git_property[global=true,property=deploy-branch]",
	} {
		if !containsString(got, want) {
			t.Errorf("global play missing %s: %v", want, got)
		}
	}
	for _, call := range calls {
		if call == "--quiet apps:list" || strings.Contains(call, " api ") || strings.HasSuffix(call, " api") {
			t.Errorf("a global-only export read an app: %q", call)
		}
	}
	if want := []string{"global"}; !reflect.DeepEqual(scopes, want) {
		t.Errorf("progress scopes = %v, want %v", scopes, want)
	}
}

func TestExportGlobalOnlyRejectsNarrowing(t *testing.T) {
	t.Parallel()
	sel, err := ParseResourceSelectors([]string{"dokku_plugin[name=redis]"})
	if err != nil {
		t.Fatalf("ParseResourceSelectors: %v", err)
	}
	for name, opts := range map[string]ExportOptions{
		"apps":      {GlobalOnly: true, Apps: []string{"api"}},
		"resources": {GlobalOnly: true, Resources: sel},
	} {
		ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(dependencyFixture()))
		res, err := ExportRecipe(ctx, opts)
		if err == nil {
			t.Errorf("%s: GlobalOnly was combined without an error", name)
		}
		if res == nil {
			t.Errorf("%s: the result must never be nil", name)
		}
	}
}
