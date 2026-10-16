package sdk_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/dokku/docket/sdk"
)

func TestCheckMinimumVersionsWithoutGate(t *testing.T) {
	fake := newFakeDokku("0.38.8")
	task, _ := sdk.NewTask("dokku_app")
	if err := sdk.CheckMinimumVersions(fake.context(), "dokku_app", task); err != nil {
		t.Fatalf("a context with no gate should not refuse: %v", err)
	}
	if fake.count("version") != 0 {
		t.Errorf("a context with no gate should not read the version")
	}
}

func TestCheckMinimumVersionsRefusesOldDokku(t *testing.T) {
	fake := newFakeDokku("0.38.8")
	ctx := sdk.ContextWithVersionGate(fake.context())
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
	if got := fake.count("version"); got != 1 {
		t.Errorf("version reads = %d, want 1 per target", got)
	}
}

func TestExportRecipeVersionGate(t *testing.T) {
	fake := newFakeDokku("0.38.0")
	res, err := sdk.ExportRecipe(fake.context(), sdk.ExportOptions{Apps: []string{"web"}, Inline: true})
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

	fake = newFakeDokku("0.38.0")
	res, err = sdk.ExportRecipe(fake.context(), sdk.ExportOptions{Apps: []string{"web"}, Inline: true, IgnoreVersion: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Report.VersionErrors) != 0 {
		t.Fatalf("IgnoreVersion should refuse nothing: %#v", res.Report.VersionErrors)
	}
	if !exportedTypes(res)["dokku_config"] {
		t.Fatalf("IgnoreVersion should export dokku_config: %#v", res.Plays())
	}
	if fake.count("version") != 0 {
		t.Errorf("IgnoreVersion should not read the version")
	}

	// IgnoreVersion wins over a gate the caller already installed.
	fake = newFakeDokku("0.38.0")
	res, err = sdk.ExportRecipe(sdk.ContextWithVersionGate(fake.context()), sdk.ExportOptions{Apps: []string{"web"}, Inline: true, IgnoreVersion: true})
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

// fakeDokku answers `dokku version` with a fixed version, one app and its config,
// and an empty success for everything else. It counts what it is asked.
type fakeDokku struct {
	version string
	mu      sync.Mutex
	calls   map[string]int
}

func newFakeDokku(version string) *fakeDokku {
	return &fakeDokku{version: version, calls: map[string]int{}}
}

func (f *fakeDokku) context() context.Context {
	return sdk.ContextWithRunner(context.Background(), func(_ context.Context, in sdk.ExecCommandInput) (sdk.ExecCommandResponse, error) {
		key := strings.Join(in.Args, " ")
		f.mu.Lock()
		f.calls[key]++
		f.mu.Unlock()
		switch key {
		case "version":
			return sdk.ExecCommandResponse{Stdout: f.version + "\n"}, nil
		case "--quiet apps:list":
			return sdk.ExecCommandResponse{Stdout: "web"}, nil
		case "--quiet config:export --format json web":
			return sdk.ExecCommandResponse{Stdout: `{"KEY":"value"}`}, nil
		default:
			return sdk.ExecCommandResponse{}, nil
		}
	})
}

func (f *fakeDokku) count(key string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[key]
}
