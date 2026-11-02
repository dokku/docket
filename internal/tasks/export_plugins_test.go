package tasks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

// export_plugins_test.go covers the export engine leaving out the exporters
// whose task needs a plugin the server does not have installed (#672).

// pluginCommandPrefixes are the subcommands of the plugins the export engine
// checks for before running an exporter that reads them.
var pluginCommandPrefixes = []string{"acl:", "http-auth:", "maintenance:", "letsencrypt:", "global-cert:"}

// pluginRunner answers like fakeDokku, records every call, and fails a
// command of a plugin it is not told is installed the way dokku does for an
// unknown command, so an exporter that runs anyway leaves a warning behind.
type pluginRunner struct {
	mu        sync.Mutex
	calls     []string
	responses map[string]string
	installed map[string]bool
	failList  bool
}

func newPluginRunner(responses map[string]string, installed ...string) *pluginRunner {
	r := &pluginRunner{responses: withInstalledPlugins(responses, installed...), installed: map[string]bool{}}
	for _, name := range installed {
		r.installed[name] = true
	}
	return r
}

func (r *pluginRunner) run(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
	args := strings.Join(in.Args, " ")
	r.mu.Lock()
	r.calls = append(r.calls, args)
	r.mu.Unlock()
	if r.failList && args == pluginListKey {
		resp := subprocess.ExecCommandResponse{ExitCode: 1}
		return resp, &subprocess.ExecError{Response: resp, Err: errors.New("plugin:list failed"), Ran: true}
	}
	for _, arg := range in.Args {
		for _, prefix := range pluginCommandPrefixes {
			if strings.HasPrefix(arg, prefix) && !r.installed[strings.TrimSuffix(prefix, ":")] {
				resp := subprocess.ExecCommandResponse{ExitCode: 1, Stderr: fmt.Sprintf(" !     %s is not a dokku command.", arg)}
				return resp, &subprocess.ExecError{Response: resp, Err: fmt.Errorf("%s is not a dokku command", arg), Ran: true}
			}
		}
	}
	return subprocess.ExecCommandResponse{Stdout: r.responses[args]}, nil
}

// pluginCalls returns the recorded commands of the given plugin prefixes.
func (r *pluginRunner) pluginCalls(prefixes ...string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, call := range r.calls {
		for _, prefix := range prefixes {
			if strings.Contains(call, prefix) {
				out = append(out, call)
				break
			}
		}
	}
	return out
}

// count returns how many recorded commands equal args.
func (r *pluginRunner) count(args string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, call := range r.calls {
		if call == args {
			n++
		}
	}
	return n
}

// pluginTaskWarnings returns the warnings raised by a task backed by one of
// pluginCommandPrefixes' plugins. The fake answers every other report with
// nothing, so the rest of the exporters warn about unparseable JSON.
func pluginTaskWarnings(warnings []string) []string {
	var out []string
	for _, w := range warnings {
		for _, typeKey := range []string{"dokku_acl_", "dokku_http_auth", "dokku_maintenance", "dokku_letsencrypt", "dokku_certs"} {
			if strings.Contains(w, ": "+typeKey) {
				out = append(out, w)
				break
			}
		}
	}
	return out
}

func pluginExportFixture() map[string]string {
	return map[string]string{
		"--quiet apps:list":                                         "web",
		"--quiet config:export --format json web":                   `{"LOG_LEVEL":"info"}`,
		"--quiet acl:list web":                                      "alice\n",
		"--quiet certs:report web --ssl-enabled":                    "false",
		"http-auth:report web --format json":                        `{"enabled":"true","users":"","allowed-ips":"","domains":""}`,
		"--quiet global-cert:report --global --global-cert-enabled": "false",
	}
}

func TestExportSkipsExportersForMissingPlugins(t *testing.T) {
	t.Parallel()
	runner := newPluginRunner(pluginExportFixture())
	ctx := subprocess.ContextWithRunner(testCtx(), runner.run)

	res, err := ExportRecipe(ctx, ExportOptions{Inline: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if calls := runner.pluginCalls(pluginCommandPrefixes...); len(calls) != 0 {
		t.Errorf("export ran commands of plugins that are not installed: %v", calls)
	}
	if warnings := pluginTaskWarnings(res.Report.Warnings); len(warnings) != 0 {
		t.Errorf("expected no warnings from plugin-backed tasks, got %v", warnings)
	}
	// dokku_certs' app scope needs only core certs, so it is still read.
	if runner.count("--quiet certs:report web --ssl-enabled") != 1 {
		t.Errorf("the app certs read should still run, calls: %v", runner.calls)
	}
	recipe, _ := res.MarshalRecipe("yaml")
	if !strings.Contains(string(recipe), "dokku_config") {
		t.Errorf("the rest of the app should still export:\n%s", recipe)
	}
}

func TestExportRunsExportersForInstalledPlugins(t *testing.T) {
	t.Parallel()
	runner := newPluginRunner(pluginExportFixture(), "acl", "http-auth")
	ctx := subprocess.ContextWithRunner(testCtx(), runner.run)

	res, err := ExportRecipe(ctx, ExportOptions{Inline: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if runner.count("--quiet acl:list web") != 1 {
		t.Errorf("acl is installed, so acl:list should run once, calls: %v", runner.calls)
	}
	recipe, _ := res.MarshalRecipe("yaml")
	for _, want := range []string{"dokku_acl_app", "alice", "dokku_http_auth:"} {
		if !strings.Contains(string(recipe), want) {
			t.Errorf("recipe missing %q:\n%s", want, recipe)
		}
	}
	// The plugins that are still missing stay unread.
	if calls := runner.pluginCalls("maintenance:", "letsencrypt:", "global-cert:"); len(calls) != 0 {
		t.Errorf("export ran commands of plugins that are not installed: %v", calls)
	}
}

func TestExportGlobalCertNeedsGlobalCertPlugin(t *testing.T) {
	t.Parallel()
	const probe = "--quiet global-cert:report --global --global-cert-enabled"
	for _, tc := range []struct {
		name      string
		installed []string
		want      int
	}{
		{"missing", nil, 0},
		{"installed", []string{"global-cert"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner := newPluginRunner(pluginExportFixture(), tc.installed...)
			ctx := subprocess.ContextWithRunner(testCtx(), runner.run)
			if _, err := ExportRecipe(ctx, ExportOptions{GlobalOnly: true}); err != nil {
				t.Fatalf("ExportRecipe: %v", err)
			}
			if got := runner.count(probe); got != tc.want {
				t.Errorf("%s ran %d times, want %d", probe, got, tc.want)
			}
		})
	}
}

func TestExportReadsThePluginListOncePerRun(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		export func(context.Context, ExportOptions) (*ExportResult, error)
	}{
		{"ungated", ExportRecipe},
		{"gated", GatedExportRecipe},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fixture := pluginExportFixture()
			fixture["version"] = "dokku version 0.38.31"
			fixture["--quiet apps:list"] = "web\nworker"
			runner := newPluginRunner(fixture)
			ctx := subprocess.ContextWithRunner(testCtx(), runner.run)
			if _, err := tc.export(ctx, ExportOptions{Inline: true}); err != nil {
				t.Fatalf("export: %v", err)
			}
			// One read for the installed-plugin check - shared with the
			// version gate when there is one - and one inside dokku_plugin's
			// own exporter, however many apps and exporters there are.
			if got := runner.count(pluginListKey); got != 2 {
				t.Errorf("plugin:list --format json ran %d times, want 2", got)
			}
		})
	}
}

func TestExportRunsExportersWhenThePluginListFails(t *testing.T) {
	t.Parallel()
	runner := newPluginRunner(pluginExportFixture(), "acl")
	runner.failList = true
	ctx := subprocess.ContextWithRunner(testCtx(), runner.run)

	res, err := ExportRecipe(ctx, ExportOptions{Inline: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	// Without a plugin list the exporters run as they did before the check,
	// and each reports what its own read returns.
	if runner.count("--quiet acl:list web") != 1 {
		t.Errorf("acl:list should run when the plugin list cannot be read, calls: %v", runner.calls)
	}
	recipe, _ := res.MarshalRecipe("yaml")
	if !strings.Contains(string(recipe), "dokku_acl_app") {
		t.Errorf("recipe missing dokku_acl_app:\n%s", recipe)
	}
	if !slices.ContainsFunc(res.Report.Warnings, func(w string) bool { return strings.Contains(w, "dokku_http_auth") }) {
		t.Errorf("a missing plugin read anyway should warn as before, got %v", res.Report.Warnings)
	}
}

func TestExportResourceForMissingPluginIsUnmatched(t *testing.T) {
	t.Parallel()
	runner := newPluginRunner(pluginExportFixture())
	ctx := subprocess.ContextWithRunner(testCtx(), runner.run)

	sel, err := ParseResourceSelectors([]string{"dokku_acl_app[app=web]"})
	if err != nil {
		t.Fatalf("ParseResourceSelectors: %v", err)
	}
	res, err := ExportRecipe(ctx, ExportOptions{Inline: true, Resources: sel})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if calls := runner.pluginCalls("acl:"); len(calls) != 0 {
		t.Errorf("export ran acl commands without dokku-acl: %v", calls)
	}
	if want := []string{"dokku_acl_app[app=web]"}; !slices.Equal(res.Report.MissingResources, want) {
		t.Errorf("MissingResources = %v, want %v", res.Report.MissingResources, want)
	}
	if warnings := pluginTaskWarnings(res.Report.Warnings); len(warnings) != 0 {
		t.Errorf("expected no warnings from plugin-backed tasks, got %v", warnings)
	}
}

func TestExportScopePrototypeSetsGlobalOnlyForTheGlobalPlay(t *testing.T) {
	t.Parallel()
	proto, _ := Lookup("dokku_certs")
	if got := exportScopePrototype(proto, true).(*CertsTask); !got.Global {
		t.Errorf("global prototype should have Global set, got %+v", got)
	}
	if proto.(*CertsTask).Global {
		t.Errorf("setting Global must not change the registry prototype")
	}
	if got := exportScopePrototype(proto, false).(*CertsTask); got.Global {
		t.Errorf("app prototype should leave Global unset, got %+v", got)
	}
	// A task without a Global field comes back as it went in.
	app, _ := Lookup("dokku_acl_app")
	if got := exportScopePrototype(app, true); got != app {
		t.Errorf("a task without Global should be returned as-is")
	}
}

// TestExportersReturnPluginFailures covers the exporters that used to swallow
// every non-transport failure so a server without their plugin stayed quiet.
// The engine no longer runs them there, so a failure is the installed
// plugin's own and is returned, for the engine to report as a warning.
func TestExportersReturnPluginFailures(t *testing.T) {
	t.Parallel()
	runner := newPluginRunner(map[string]string{
		"--quiet plugin:trigger service-list": "postgres:db",
	})
	ctx := subprocess.ContextWithRunner(testCtx(), runner.run)

	if _, err := (AclServiceTask{}).ExportGlobal(ctx); err == nil {
		t.Errorf("AclServiceTask.ExportGlobal: want the acl:list-service error, got nil")
	}
	if _, err := (HttpAuthUserTask{}).ExportApp(ctx, "web"); err == nil {
		t.Errorf("HttpAuthUserTask.ExportApp: want the http-auth:export-users error, got nil")
	}
	if _, err := (CertsTask{}).ExportGlobal(ctx); err == nil {
		t.Errorf("CertsTask.ExportGlobal: want the global-cert:report error, got nil")
	}
}

// TestExportAppCertProbeFailureStaysQuiet pins the half of exportCert that
// still swallows: certs is core, and an app whose certs:report fails is
// skipped as before.
func TestExportAppCertProbeFailureStaysQuiet(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		resp := subprocess.ExecCommandResponse{ExitCode: 1}
		return resp, &subprocess.ExecError{Response: resp, Err: errors.New("certs:report failed"), Ran: true}
	})
	bodies, err := (CertsTask{}).ExportApp(ctx, "web")
	if err != nil || len(bodies) != 0 {
		t.Errorf("CertsTask.ExportApp = %v, %v; want no bodies and no error", bodies, err)
	}
}

// TestExportWarnsWhenAnInstalledPluginFails is the engine side of the above:
// with dokku-acl installed, a failing acl:list-service is reported rather than
// exported as an empty access list.
func TestExportWarnsWhenAnInstalledPluginFails(t *testing.T) {
	t.Parallel()
	responses := withInstalledPlugins(map[string]string{
		"--quiet plugin:trigger service-list": "postgres:db",
	}, "acl")
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		args := strings.Join(in.Args, " ")
		if strings.HasPrefix(args, "--quiet acl:list-service") {
			resp := subprocess.ExecCommandResponse{ExitCode: 1}
			return resp, &subprocess.ExecError{Response: resp, Err: errors.New("acl:list-service failed"), Ran: true}
		}
		return subprocess.ExecCommandResponse{Stdout: responses[args]}, nil
	})
	res, err := ExportRecipe(ctx, ExportOptions{GlobalOnly: true})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	if !slices.ContainsFunc(res.Report.Warnings, func(w string) bool { return strings.HasPrefix(w, "global: dokku_acl_service: ") }) {
		t.Errorf("expected a dokku_acl_service warning, got %v", res.Report.Warnings)
	}
}
