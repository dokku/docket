package tasks

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

func TestParseVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		raw  string
		want string
		ok   bool
	}{
		{raw: "0.38.31", want: "0.38.31", ok: true},
		{raw: "dokku version 0.38.31", want: "0.38.31", ok: true},
		{raw: "  v0.38.31\n", want: "0.38.31", ok: true},
		{raw: "2.3.1", want: "2.3.1", ok: true},
		{raw: "", ok: false},
		{raw: "0.38", ok: false},
		{raw: "0.38.31-rc1", ok: false},
		{raw: "abc1234", ok: false},
		{raw: "0.38.08", ok: false},
	}
	for _, tc := range cases {
		got, err := parseVersion(tc.raw)
		if tc.ok {
			if err != nil {
				t.Errorf("parseVersion(%q) error = %v", tc.raw, err)
				continue
			}
			if got.String() != tc.want {
				t.Errorf("parseVersion(%q) = %s, want %s", tc.raw, got, tc.want)
			}
			continue
		}
		if err == nil {
			t.Errorf("parseVersion(%q) = %s, want an error", tc.raw, got)
		}
	}
}

func TestVersionOlderThan(t *testing.T) {
	t.Parallel()
	older, err := parseVersion("0.38.8")
	if err != nil {
		t.Fatal(err)
	}
	floor, err := parseVersion("0.38.31")
	if err != nil {
		t.Fatal(err)
	}
	same, err := parseVersion("0.38.31")
	if err != nil {
		t.Fatal(err)
	}
	newer, err := parseVersion("0.39.0")
	if err != nil {
		t.Fatal(err)
	}
	if !older.olderThan(floor) {
		t.Error("0.38.8 should be older than 0.38.31")
	}
	if same.olderThan(floor) {
		t.Error("an equal version should be allowed")
	}
	if newer.olderThan(floor) {
		t.Error("0.39.0 should be allowed against 0.38.31")
	}
}

func TestParsePluginList(t *testing.T) {
	t.Parallel()
	jsonBody := `[{"name":"letsencrypt","version":"0.25.0","enabled":true},{"name":"redis","version":"2.3.1"}]`
	got := parsePluginList(jsonBody)
	if got["letsencrypt"] != "0.25.0" || got["redis"] != "2.3.1" {
		t.Fatalf("json list = %#v", got)
	}

	text := "letsencrypt 0.1.0 enabled\nredis abc1234 enabled\nhttp-auth enabled\n"
	got = parsePluginList(text)
	if got["letsencrypt"] != "0.1.0" {
		t.Errorf("text version = %q", got["letsencrypt"])
	}
	if _, ok := got["redis"]; !ok || got["redis"] != "" {
		t.Errorf("a commit sha should be recorded as installed with an unparseable version, got %#v", got)
	}
	if _, ok := got["http-auth"]; !ok || got["http-auth"] != "" {
		t.Errorf("a row with no version column should be installed, got %#v", got)
	}
}

func TestCheckMinimumVersions(t *testing.T) {
	t.Parallel()

	t.Run("no gate", func(t *testing.T) {
		t.Parallel()
		called := false
		ctx := subprocess.ContextWithRunner(context.Background(), func(context.Context, subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
			called = true
			return subprocess.ExecCommandResponse{}, nil
		})
		if err := CheckMinimumVersions(ctx, "dokku_app", AppTask{App: "a"}); err != nil {
			t.Fatal(err)
		}
		if called {
			t.Error("a context without a version gate should not probe")
		}
	})

	t.Run("no floor", func(t *testing.T) {
		t.Parallel()
		called := false
		ctx := gatedRunner(t, func(context.Context, subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
			called = true
			return subprocess.ExecCommandResponse{}, nil
		})
		if err := CheckMinimumVersions(ctx, "dokku_stub", floorlessTask{}); err != nil {
			t.Fatal(err)
		}
		if called {
			t.Error("a task with no floor should not probe")
		}
	})

	t.Run("old dokku", func(t *testing.T) {
		t.Parallel()
		ctx := gatedRunner(t, versionRunner("0.38.8\n", ""))
		err := CheckMinimumVersions(ctx, "dokku_app", AppTask{App: "a"})
		if err == nil || err.Error() != "dokku_app requires Dokku >= 0.38.31, server has 0.38.8" {
			t.Fatalf("error = %v", err)
		}
		verr := asVersionError(t, err)
		if verr.TaskType != "dokku_app" || verr.Plugin != "" || verr.Minimum != "0.38.31" || verr.Found != "0.38.8" || verr.Err != nil {
			t.Fatalf("version error = %#v", verr)
		}
	})

	t.Run("new enough dokku", func(t *testing.T) {
		t.Parallel()
		ctx := gatedRunner(t, versionRunner("dokku version 0.38.31", ""))
		if err := CheckMinimumVersions(ctx, "dokku_app", AppTask{App: "a"}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("unparseable dokku", func(t *testing.T) {
		t.Parallel()
		ctx := gatedRunner(t, versionRunner("not-a-version\n", ""))
		err := CheckMinimumVersions(ctx, "dokku_app", AppTask{App: "a"})
		if err == nil || err.Error() != `dokku_app requires Dokku >= 0.38.31, but the server version "not-a-version" could not be parsed` {
			t.Fatalf("error = %v", err)
		}
		if verr := asVersionError(t, err); verr.Found != "not-a-version" || verr.Err == nil {
			t.Fatalf("version error = %#v", verr)
		}
	})

	t.Run("probe error", func(t *testing.T) {
		t.Parallel()
		ctx := gatedRunner(t, func(context.Context, subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
			return subprocess.ExecCommandResponse{}, errVersionProbe
		})
		err := CheckMinimumVersions(ctx, "dokku_app", AppTask{App: "a"})
		if err == nil || !strings.Contains(err.Error(), "dokku_app: checking Dokku version: ") || strings.Contains(err.Error(), "--ignore-version") {
			t.Fatalf("error = %v", err)
		}
		if !errors.Is(err, errVersionProbe) {
			t.Fatalf("error %v should wrap the probe failure", err)
		}
		if verr := asVersionError(t, err); verr.Plugin != "" || verr.Found != "" {
			t.Fatalf("version error = %#v", verr)
		}
	})

	t.Run("old plugin", func(t *testing.T) {
		t.Parallel()
		ctx := gatedRunner(t, versionRunner("0.38.31\n", `[{"name":"letsencrypt","version":"0.1.0"}]`))
		err := CheckMinimumVersions(ctx, "dokku_letsencrypt", LetsencryptTask{App: "a"})
		if err == nil || err.Error() != "dokku_letsencrypt requires letsencrypt >= 0.25.2, server has 0.1.0" {
			t.Fatalf("error = %v", err)
		}
		verr := asVersionError(t, err)
		if verr.TaskType != "dokku_letsencrypt" || verr.Plugin != "letsencrypt" || verr.Minimum != "0.25.2" || verr.Found != "0.1.0" || verr.Err != nil {
			t.Fatalf("version error = %#v", verr)
		}
	})

	t.Run("plugin version from text list", func(t *testing.T) {
		t.Parallel()
		ctx := gatedRunner(t, versionRunner("0.38.31\n", "letsencrypt 0.1.0 enabled\n"))
		err := CheckMinimumVersions(ctx, "dokku_letsencrypt", LetsencryptTask{App: "a"})
		if err == nil || !strings.Contains(err.Error(), "server has 0.1.0") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("plugin list error", func(t *testing.T) {
		t.Parallel()
		ctx := gatedRunner(t, func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
			if strings.Join(in.Args, " ") == "version" {
				return subprocess.ExecCommandResponse{Stdout: "0.38.31\n"}, nil
			}
			return subprocess.ExecCommandResponse{}, errVersionProbe
		})
		err := CheckMinimumVersions(ctx, "dokku_letsencrypt", LetsencryptTask{App: "a"})
		if err == nil || !strings.Contains(err.Error(), "dokku_letsencrypt: checking plugin versions: ") || !errors.Is(err, errVersionProbe) {
			t.Fatalf("error = %v", err)
		}
		if verr := asVersionError(t, err); verr.Plugin != "letsencrypt" || verr.Minimum != "0.25.2" || verr.Found != "" {
			t.Fatalf("version error = %#v", verr)
		}
	})

	t.Run("missing plugin", func(t *testing.T) {
		t.Parallel()
		ctx := gatedRunner(t, versionRunner("0.38.31\n", `[{"name":"http-auth","version":"0.14.0"}]`))
		if err := CheckMinimumVersions(ctx, "dokku_letsencrypt", LetsencryptTask{App: "a"}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("unparseable plugin version", func(t *testing.T) {
		t.Parallel()
		ctx := gatedRunner(t, versionRunner("0.38.31\n", `[{"name":"letsencrypt","version":"abc1234"}]`))
		err := CheckMinimumVersions(ctx, "dokku_letsencrypt", LetsencryptTask{App: "a"})
		if err == nil || err.Error() != `dokku_letsencrypt requires letsencrypt >= 0.25.2, but the installed version "abc1234" could not be parsed` {
			t.Fatalf("error = %v", err)
		}
		if verr := asVersionError(t, err); verr.Plugin != "letsencrypt" || verr.Found != "abc1234" || verr.Err == nil {
			t.Fatalf("version error = %#v", verr)
		}
	})

	t.Run("conditional plugin", func(t *testing.T) {
		t.Parallel()
		ctx := gatedRunner(t, versionRunner("0.38.31\n", `[{"name":"global-cert","version":"0.1.0"}]`))
		if err := CheckMinimumVersions(ctx, "dokku_certs", CertsTask{}); err != nil {
			t.Fatalf("a non-global certs task should not demand global-cert: %v", err)
		}
		err := CheckMinimumVersions(ctx, "dokku_certs", CertsTask{Global: true})
		if err == nil || !strings.Contains(err.Error(), "requires global-cert >= 0.7.0") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("service name field", func(t *testing.T) {
		t.Parallel()
		ctx := gatedRunner(t, versionRunner("0.38.31\n", `[{"name":"redis","version":"1.0.0"}]`))
		if err := CheckMinimumVersions(ctx, "dokku_service_create", ServiceCreateTask{}); err != nil {
			t.Fatalf("an empty service type should skip the plugin check: %v", err)
		}
		err := CheckMinimumVersions(ctx, "dokku_service_create", ServiceCreateTask{Service: "redis"})
		if err == nil || !strings.Contains(err.Error(), "requires redis >= 2.3.1, server has 1.0.0") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestVersionCacheIsPerTarget(t *testing.T) {
	t.Parallel()
	calls := map[string]int{}
	base := gatedRunner(t, func(ctx context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		if strings.Join(in.Args, " ") != "version" {
			return subprocess.ExecCommandResponse{}, nil
		}
		host := subprocess.TargetFromContext(ctx).Host
		calls[host]++
		if host == "old.example" {
			return subprocess.ExecCommandResponse{Stdout: "0.38.8\n"}, nil
		}
		return subprocess.ExecCommandResponse{Stdout: "0.38.31\n"}, nil
	})
	local := subprocess.ContextWithTarget(base, subprocess.Target{})
	remote := subprocess.ContextWithTarget(base, subprocess.Target{Host: "old.example"})

	if err := CheckMinimumVersions(local, "dokku_app", AppTask{App: "a"}); err != nil {
		t.Fatal(err)
	}
	err := CheckMinimumVersions(remote, "dokku_app", AppTask{App: "a"})
	if err == nil || !strings.Contains(err.Error(), "server has 0.38.8") {
		t.Fatalf("remote error = %v", err)
	}
	if err := CheckMinimumVersions(local, "dokku_app", AppTask{App: "a"}); err != nil {
		t.Fatal(err)
	}
	if calls[""] != 1 {
		t.Errorf("local version probes = %d, want 1", calls[""])
	}
	if calls["old.example"] != 1 {
		t.Errorf("remote version probes = %d, want 1", calls["old.example"])
	}
}

// TestVersionCacheKeyFollowsTheConnection pins that the identity file and ssh
// options are part of the cache key. An option such as HostName sends the same
// host string to a different server, which may run a different Dokku.
func TestVersionCacheKeyFollowsTheConnection(t *testing.T) {
	t.Parallel()
	targets := []subprocess.Target{
		{Host: "deploy@example.com"},
		{Host: "deploy@example.com", IdentityFile: "/keys/a"},
		{Host: "deploy@example.com", IdentityFile: "/keys/b"},
		{Host: "deploy@example.com", SSHOptions: []string{"HostName=other.example.com"}},
	}
	seen := map[string]int{}
	for i, target := range targets {
		key := targetCacheKey(subprocess.ContextWithTarget(context.Background(), target))
		if other, ok := seen[key]; ok {
			t.Errorf("targets %+v and %+v share version cache key %q", targets[other], target, key)
		}
		seen[key] = i
	}
}

// TestPluginChangeInvalidatesVersionCache checks a plugin change drops the
// cached plugin list. dokku_plugin reads `plugin:list --format json` itself,
// the same command the gate caches, so the fake answers both from one list:
// letsencrypt starts out installed at a version too old for
// dokku_letsencrypt, and the gate stops refusing once dokku_plugin has
// uninstalled it - an uninstalled plugin is not a refusal - which only a fresh
// read can see.
func TestPluginChangeInvalidatesVersionCache(t *testing.T) {
	t.Parallel()
	installed := true
	lists := 0
	ctx := gatedRunner(t, func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		key := strings.Join(in.Args, " ")
		switch key {
		case "version":
			return subprocess.ExecCommandResponse{Stdout: "0.38.31\n"}, nil
		case "--quiet plugin:list --format json":
			lists++
			if !installed {
				return subprocess.ExecCommandResponse{Stdout: `[]`}, nil
			}
			return subprocess.ExecCommandResponse{Stdout: `[{"name":"letsencrypt","version":"0.1.0"}]`}, nil
		case "--quiet plugin:uninstall letsencrypt":
			installed = false
			return subprocess.ExecCommandResponse{}, nil
		default:
			return subprocess.ExecCommandResponse{}, nil
		}
	})

	err := CheckMinimumVersions(ctx, "dokku_letsencrypt", LetsencryptTask{App: "a"})
	if err == nil {
		t.Fatal("expected the old plugin to be refused before the change")
	}
	state := (PluginTask{Name: "letsencrypt", State: StateAbsent}).Execute(ctx)
	if state.Error != nil {
		t.Fatalf("uninstall: %v", state.Error)
	}
	if err := CheckMinimumVersions(ctx, "dokku_letsencrypt", LetsencryptTask{App: "a"}); err != nil {
		t.Fatalf("after uninstall: %v", err)
	}
	if lists != 3 {
		t.Errorf("plugin list reads = %d, want 3 (the gate before the change, the plan, the gate after invalidation)", lists)
	}
}

func TestFailedPluginChangeKeepsVersionCache(t *testing.T) {
	t.Parallel()
	lists := 0
	ctx := gatedRunner(t, func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		key := strings.Join(in.Args, " ")
		switch key {
		case "version":
			return subprocess.ExecCommandResponse{Stdout: "0.38.31\n"}, nil
		case "--quiet plugin:list --format json":
			lists++
			return subprocess.ExecCommandResponse{Stdout: `[{"name":"letsencrypt","version":"0.1.0"}]`}, nil
		case "--quiet plugin:uninstall letsencrypt":
			return subprocess.ExecCommandResponse{}, errVersionProbe
		default:
			return subprocess.ExecCommandResponse{}, nil
		}
	})

	if err := CheckMinimumVersions(ctx, "dokku_letsencrypt", LetsencryptTask{App: "a"}); err == nil {
		t.Fatal("expected the old plugin to be refused")
	}
	state := (PluginTask{Name: "letsencrypt", State: StateAbsent}).Execute(ctx)
	if state.Error == nil {
		t.Fatal("expected the uninstall to fail")
	}
	if err := CheckMinimumVersions(ctx, "dokku_letsencrypt", LetsencryptTask{App: "a"}); err == nil {
		t.Fatal("expected the cached old version to still refuse")
	}
	if lists != 2 {
		t.Errorf("plugin list reads = %d, want 2 (the gate once, the plan once; a failed change should keep the cache)", lists)
	}
}

func TestExportRecipeWarnsOnOldDokku(t *testing.T) {
	t.Parallel()
	responses := exportFixture()
	responses["version"] = "0.38.0\n"
	ctx := ContextWithVersionGate(subprocess.ContextWithRunner(context.Background(), fakeDokku(responses)))
	res, err := ExportRecipe(ctx, ExportOptions{Apps: []string{"app-one"}})
	if err != nil {
		t.Fatal(err)
	}
	var warned bool
	for _, warning := range res.Report.Warnings {
		if strings.Contains(warning, "app-one: dokku_config: dokku_config requires Dokku >= 0.38.31") {
			warned = true
		}
	}
	if !warned {
		t.Fatalf("warnings = %#v", res.Report.Warnings)
	}
	var refusedConfig bool
	for _, verr := range res.Report.VersionErrors {
		if verr.TaskType == "dokku_config" && verr.Found == "0.38.0" {
			refusedConfig = true
		}
	}
	if !refusedConfig {
		t.Fatalf("version errors = %#v", res.Report.VersionErrors)
	}
	for _, play := range res.Plays() {
		for _, task := range play.Tasks {
			if task.Type == "dokku_config" {
				t.Fatalf("an old server should not export dokku_config: %#v", play.Tasks)
			}
		}
	}
}

func TestExportRecipeIgnoreVersionOverridesTheGate(t *testing.T) {
	t.Parallel()
	responses := exportFixture()
	responses["version"] = "0.38.0\n"
	versionReads := 0
	runner := fakeDokku(responses)
	ctx := ContextWithVersionGate(subprocess.ContextWithRunner(context.Background(), func(ctx context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		if strings.Join(in.Args, " ") == "version" {
			versionReads++
		}
		return runner(ctx, in)
	}))
	res, err := ExportRecipe(ctx, ExportOptions{Apps: []string{"app-one"}, IgnoreVersion: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Report.VersionErrors) != 0 {
		t.Fatalf("version errors = %#v", res.Report.VersionErrors)
	}
	for _, warning := range res.Report.Warnings {
		if strings.Contains(warning, "requires Dokku") {
			t.Fatalf("IgnoreVersion should not refuse: %#v", res.Report.Warnings)
		}
	}
	if versionReads != 0 {
		t.Errorf("version reads = %d, want 0", versionReads)
	}
	var exported bool
	for _, play := range res.Plays() {
		for _, task := range play.Tasks {
			if task.Type == "dokku_config" {
				exported = true
			}
		}
	}
	if !exported {
		t.Fatalf("IgnoreVersion should export dokku_config: %#v", res.Plays())
	}
}

func asVersionError(t *testing.T, err error) *VersionError {
	t.Helper()
	var verr *VersionError
	if !errors.As(err, &verr) {
		t.Fatalf("error %v (%T) is not a *VersionError", err, err)
	}
	return verr
}

type floorlessTask struct{}

func (floorlessTask) Plan(context.Context) PlanResult { return PlanResult{} }

func (floorlessTask) Execute(context.Context) TaskOutputState { return TaskOutputState{} }

var errVersionProbe = errString("dokku: not available")

type errString string

func (e errString) Error() string { return string(e) }

func gatedRunner(t *testing.T, runner subprocess.ExecRunner) context.Context {
	t.Helper()
	return ContextWithVersionGate(subprocess.ContextWithRunner(context.Background(), runner))
}

func versionRunner(dokku, plugins string) subprocess.ExecRunner {
	return func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		switch strings.Join(in.Args, " ") {
		case "version":
			return subprocess.ExecCommandResponse{Stdout: dokku}, nil
		case "--quiet plugin:list --format json":
			return subprocess.ExecCommandResponse{Stdout: plugins}, nil
		default:
			return subprocess.ExecCommandResponse{}, nil
		}
	}
}
