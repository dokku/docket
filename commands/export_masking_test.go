package commands

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

// failingExecRunner answers from responses like fakeExecRunner, except that
// one command fails with err. fakeExecRunner can only return canned stdout, and
// the diagnostics this file is about are built from an exporter's error.
func failingExecRunner(responses map[string]string, failing string, err error) func(context.Context, subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
	return func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		key := strings.Join(in.Args, " ")
		if key == failing {
			return subprocess.ExecCommandResponse{}, err
		}
		return subprocess.ExecCommandResponse{Stdout: responses[key]}, nil
	}
}

// TestExportCommandWarningMasksAConfigValue is the point of #488: the mask on
// the warning loop could never fire, because nothing on the export path ever
// populated the registry.
//
// dokku_app_lock sits directly ahead of dokku_config in appExportOrder, so the
// warning is appended before the value it quotes has been read at all. That is
// deliberate: it pins that masking happens when the warning is printed, after
// the whole read, rather than when it is appended.
func TestExportCommandWarningMasksAConfigValue(t *testing.T) {
	defer subprocess.SetExecRunner(failingExecRunner(
		exportCommandFixture(),
		"--quiet apps:locked web",
		errors.New(`apps:locked: unreadable lock state "abc123"`),
	))()
	c, ui := newExportCommand(t.TempDir())
	if code := c.Run(nil); code != 0 {
		t.Fatalf("Run exit = %d, want 0: %s", code, ui.ErrorWriter.String())
	}

	warnings := ui.ErrorWriter.String()
	var line string
	for _, l := range strings.Split(warnings, "\n") {
		if strings.Contains(l, "web: dokku_app_lock") {
			line = l
			break
		}
	}
	if line == "" {
		t.Fatalf("expected the app_lock export warning, got: %s", warnings)
	}
	if strings.Contains(line, "abc123") {
		t.Errorf("warning leaked the config value: %s", line)
	}
	if !strings.Contains(line, `unreadable lock state "***"`) {
		t.Errorf("warning should carry the masked value, got: %s", line)
	}
}

// TestExportCommandRedactWarningMasksAConfigValue is the case that rules out
// registering from res.Vars once the export is over: --redact writes a
// placeholder there, so the vars map holds nothing to mask with - while the
// real value was still read off the server and is still in the warning.
func TestExportCommandRedactWarningMasksAConfigValue(t *testing.T) {
	defer subprocess.SetExecRunner(failingExecRunner(
		exportCommandFixture(),
		"--quiet apps:locked web",
		errors.New(`apps:locked: unreadable lock state "abc123"`),
	))()
	dir := t.TempDir()

	c, ui := newExportCommand(dir)
	if code := c.Run([]string{"--redact"}); code != 0 {
		t.Fatalf("Run exit = %d, want 0: %s", code, ui.ErrorWriter.String())
	}

	vars, err := os.ReadFile(filepath.Join(dir, "tasks.vars.yml"))
	if err != nil {
		t.Fatalf("read vars: %v", err)
	}
	if strings.Contains(string(vars), "abc123") {
		t.Fatalf("--redact must not write the value anywhere:\n%s", vars)
	}
	if warnings := ui.ErrorWriter.String(); strings.Contains(warnings, "abc123") {
		t.Errorf("warning leaked a value --redact wrote nowhere: %s", warnings)
	}
}

// TestExportCommandFailureMasksASecretReadBeforeTheAppList pins why
// ExportRecipe hands back its partial result on error. The global play is
// exported before apps:list runs, so by the time the failure is printed the
// export is already holding the cluster token it read.
func TestExportCommandFailureMasksASecretReadBeforeTheAppList(t *testing.T) {
	defer subprocess.SetExecRunner(failingExecRunner(
		map[string]string{
			"--quiet scheduler-k3s:report --global --format json": `{"global-token":"s3cr3ttoken"}`,
		},
		"--quiet apps:list",
		errors.New("apps:list: server rejected cluster token s3cr3ttoken"),
	))()
	c, ui := newExportCommand(t.TempDir())
	if code := c.Run(nil); code != 1 {
		t.Fatalf("Run exit = %d, want 1: %s", code, ui.ErrorWriter.String())
	}

	out := ui.ErrorWriter.String()
	if !strings.Contains(out, "export failed:") {
		t.Fatalf("expected the export failure, got: %s", out)
	}
	if strings.Contains(out, "s3cr3ttoken") {
		t.Errorf("failure leaked the cluster token read by the global play: %s", out)
	}
	if !strings.Contains(out, "***") {
		t.Errorf("failure should carry the mask placeholder, got: %s", out)
	}
}

// TestExportCommandMaskingLeavesTheVarsFileInTheClear pins the invariant that
// makes registering these values safe at all: masking is display-only. The
// recipe and the vars-file are written straight to disk, never through the
// masked Ui, so an export whose every config value is registered still writes
// a vars-file the operator can apply.
func TestExportCommandMaskingLeavesTheVarsFileInTheClear(t *testing.T) {
	defer subprocess.SetExecRunner(fakeExecRunner(exportCommandFixture()))()
	dir := t.TempDir()

	c, ui := newExportCommand(dir)
	if code := c.Run(nil); code != 0 {
		t.Fatalf("Run exit = %d, want 0: %s", code, ui.ErrorWriter.String())
	}

	vars, err := os.ReadFile(filepath.Join(dir, "tasks.vars.yml"))
	if err != nil {
		t.Fatalf("read vars: %v", err)
	}
	if !strings.Contains(string(vars), "abc123") {
		t.Errorf("vars-file must hold the real value, got:\n%s", vars)
	}
	if strings.Contains(string(vars), "***") {
		t.Errorf("vars-file must never be masked, got:\n%s", vars)
	}
}

// TestExportCommandMissingAppNameStaysReadable is the deliberate exception. An
// --app name is the user's own argument echoed back, and the message exists to
// point at the typo - which "*** not found on server" would not do. It stays
// unmasked even when it collides with a value the export registered.
func TestExportCommandMissingAppNameStaysReadable(t *testing.T) {
	responses := exportCommandFixture()
	responses["--quiet config:export --format json web"] = `{"API_KEY":"nope-app"}`
	defer subprocess.SetExecRunner(fakeExecRunner(responses))()
	c, ui := newExportCommand(t.TempDir())
	if code := c.Run([]string{"--app", "web", "--app", "nope-app"}); code != 1 {
		t.Fatalf("Run exit = %d, want 1: %s", code, ui.ErrorWriter.String())
	}

	out := ui.ErrorWriter.String()
	if !strings.Contains(out, "nope-app not found on server") {
		t.Errorf("the missing --app name must stay readable, got: %s", out)
	}
}
