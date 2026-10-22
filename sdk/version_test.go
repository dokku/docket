package sdk_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dokku/docket/sdk"
	"github.com/dokku/docket/sdk/dokkutest"
)

func TestCheckMinimumVersionsWithoutGate(t *testing.T) {
	fake := newFakeDokku(t, "0.38.8")
	task, _ := sdk.NewTask("dokku_app")
	if err := sdk.CheckMinimumVersions(fake.Context(context.Background()), "dokku_app", task); err != nil {
		t.Fatalf("a context with no gate should not refuse: %v", err)
	}
	if fake.Count("version") != 0 {
		t.Errorf("a context with no gate should not read the version")
	}
}

func TestCheckMinimumVersionsRefusesOldDokku(t *testing.T) {
	fake := newFakeDokku(t, "0.38.8")
	ctx := sdk.ContextWithVersionGate(fake.Context(context.Background()))
	task, _ := sdk.NewTask("dokku_app")

	err := sdk.CheckMinimumVersions(ctx, "dokku_app", task)
	var verr *sdk.VersionError
	if !errors.As(err, &verr) {
		t.Fatalf("error = %v (%T), want a *VersionError", err, err)
	}
	if verr.TaskType != "dokku_app" || verr.Plugin != "" || verr.Found != "0.38.8" {
		t.Errorf("version error = %#v", verr)
	}
	if want, ok := sdk.TaskMinimumVersions(task); !ok || verr.Minimum != want.Dokku {
		t.Errorf("Minimum = %q, want the declared %q", verr.Minimum, want.Dokku)
	}
	if strings.Contains(err.Error(), "--ignore-version") {
		t.Errorf("an sdk error should not name a CLI flag: %v", err)
	}

	if err := sdk.CheckMinimumVersions(ctx, "dokku_app", task); err == nil {
		t.Fatal("the second check should refuse too")
	}
	if got := fake.Count("version"); got != 1 {
		t.Errorf("version reads = %d, want 1 per target", got)
	}
}

// TestSessionWithVersionGate is the embedder #617 describes: every call gets a
// context of its own from the session, and the server's version is still read
// once for all of them, with each call's refusal intact.
func TestSessionWithVersionGate(t *testing.T) {
	fake := newFakeDokku(t, "0.38.8")
	session := sdk.NewSession(nil, sdk.WithVersionGate())
	defer func() { _ = session.Close() }()
	task, _ := sdk.NewTask("dokku_app")

	for i := 0; i < 3; i++ {
		reqCtx, cancel := context.WithCancel(fake.Context(context.Background()))
		ctx := session.Context(reqCtx, sdk.Target{})
		var verr *sdk.VersionError
		if err := sdk.CheckMinimumVersions(ctx, "dokku_app", task); !errors.As(err, &verr) {
			t.Fatalf("call %d: error = %v, want a *VersionError", i, err)
		}
		cancel()
	}
	if got := fake.Count("version"); got != 1 {
		t.Errorf("version reads = %d, want 1 per session", got)
	}

	ungated := sdk.NewSession(nil).Context(fake.Context(context.Background()), sdk.Target{})
	if err := sdk.CheckMinimumVersions(ungated, "dokku_app", task); err != nil {
		t.Errorf("a session without the gate refused: %v", err)
	}
}

func TestExportRecipeVersionGate(t *testing.T) {
	fake := exportFake(t)
	res, err := sdk.ExportRecipe(fake.Context(context.Background()), sdk.ExportOptions{Apps: []string{"web"}, Inline: true})
	if err != nil {
		t.Fatal(err)
	}
	if exportedTypes(res)["dokku_config"] {
		t.Fatalf("an old server should not export dokku_config")
	}
	var refused bool
	for _, verr := range res.Report.VersionErrors {
		if verr.TaskType == "dokku_config" {
			refused = true
		}
	}
	if !refused {
		t.Fatalf("VersionErrors = %#v, report = %#v", res.Report.VersionErrors, res.Report)
	}

	fake = exportFake(t)
	res, err = sdk.ExportRecipe(fake.Context(context.Background()), sdk.ExportOptions{Apps: []string{"web"}, Inline: true, IgnoreVersion: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Report.VersionErrors) != 0 {
		t.Fatalf("IgnoreVersion should refuse nothing: %#v", res.Report.VersionErrors)
	}
	if !exportedTypes(res)["dokku_config"] {
		t.Fatalf("IgnoreVersion should export dokku_config: %#v", res.Plays())
	}
	if fake.Count("version") != 0 {
		t.Errorf("IgnoreVersion should not read the version")
	}

	// IgnoreVersion wins over a gate the caller already installed.
	fake = exportFake(t)
	res, err = sdk.ExportRecipe(sdk.ContextWithVersionGate(fake.Context(context.Background())), sdk.ExportOptions{Apps: []string{"web"}, Inline: true, IgnoreVersion: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Report.VersionErrors) != 0 || !exportedTypes(res)["dokku_config"] {
		t.Fatalf("IgnoreVersion should override a caller's gate: %#v", res.Report.VersionErrors)
	}
}

func exportedTypes(res *sdk.ExportResult) map[string]bool {
	types := map[string]bool{}
	for _, play := range res.Plays() {
		for _, task := range play.Tasks {
			types[task.Type] = true
		}
	}
	return types
}

// newFakeDokku is a fake dokku at version holding app web with one config
// key.
func newFakeDokku(t *testing.T, version string) *dokkutest.Server {
	return dokkutest.New(t).SeedVersion(version).SeedConfig("web", map[string]string{"KEY": "value"})
}

// exportFake is newFakeDokku at 0.38.0 that also answers, with an empty
// success, the commands of the exporters dokkutest does not model, which an
// export of every task type sends.
func exportFake(t *testing.T) *dokkutest.Server {
	return newFakeDokku(t, "0.38.0").Fallback(dokkutest.Respond(""))
}
