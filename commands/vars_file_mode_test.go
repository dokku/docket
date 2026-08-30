package commands

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	flag "github.com/spf13/pflag"
)

// sensitiveArg is argFor plus the declaration that makes a vars-file holding
// this input a secrets file rather than a settings file.
func sensitiveArg(t *testing.T, def string) *Argument {
	t.Helper()
	arg := argFor(t, "string", def)
	arg.Sensitive = true
	return arg
}

// chmodTempFile writes a vars file and pins its mode. writeTempFile passes
// 0o644 to os.WriteFile, which the umask can tighten, so these tests set the
// mode explicitly rather than inheriting whoever is running them.
func chmodTempFile(t *testing.T, dir, name, content string, mode os.FileMode) string {
	t.Helper()
	path := writeTempFile(t, dir, name, content)
	if err := os.Chmod(path, mode); err != nil {
		t.Fatalf("chmod %s: %v", path, err)
	}
	return path
}

// TestApplyVarsFilesWarnsOnPermissiveSensitiveFile is the reading end of #489.
func TestApplyVarsFilesWarnsOnPermissiveSensitiveFile(t *testing.T) {
	dir := t.TempDir()
	path := chmodTempFile(t, dir, "vars.yml", "api_key: s3cret\n", 0o644)

	args := map[string]*Argument{"api_key": sensitiveArg(t, "")}
	flags := flag.NewFlagSet("t", flag.ContinueOnError)

	_, warnings, err := applyVarsFiles(args, flags, []string{path})
	if err != nil {
		t.Fatalf("applyVarsFiles failed: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}
	for _, want := range []string{path, "sensitive", "0644", "chmod 600"} {
		if !strings.Contains(warnings[0], want) {
			t.Errorf("warning missing %q\nfull: %s", want, warnings[0])
		}
	}
}

// TestApplyVarsFilesQuietForPrivateSensitiveFile: the file docket export
// itself writes must not warn about the mode docket export gave it.
func TestApplyVarsFilesQuietForPrivateSensitiveFile(t *testing.T) {
	dir := t.TempDir()
	path := chmodTempFile(t, dir, "vars.yml", "api_key: s3cret\n", varsFileMode)

	args := map[string]*Argument{"api_key": sensitiveArg(t, "")}
	flags := flag.NewFlagSet("t", flag.ContinueOnError)

	_, warnings, err := applyVarsFiles(args, flags, []string{path})
	if err != nil {
		t.Fatalf("applyVarsFiles failed: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("a 0600 vars-file must not warn, got %v", warnings)
	}
}

// TestApplyVarsFilesQuietWithoutASensitiveInput keeps the warning off the
// ordinary per-environment file docs/inputs.md recommends. Its mode is the
// user's business; nothing in it is a secret.
func TestApplyVarsFilesQuietWithoutASensitiveInput(t *testing.T) {
	dir := t.TempDir()
	path := chmodTempFile(t, dir, "prod.yml", "app: api\nreplicas: 3\n", 0o644)

	args := map[string]*Argument{
		"app":      argFor(t, "string", ""),
		"replicas": argFor(t, "int", 1),
	}
	flags := flag.NewFlagSet("t", flag.ContinueOnError)

	_, warnings, err := applyVarsFiles(args, flags, []string{path})
	if err != nil {
		t.Fatalf("applyVarsFiles failed: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("a vars-file of ordinary settings must not warn, got %v", warnings)
	}
}

// TestApplyVarsFilesWarnsOnOverriddenSensitiveKey is why loadVarsFiles tracks
// keys per file rather than only last-writer-wins: base.yml still holds the
// secret on disk even though prod.yml's value is the one that gets used.
func TestApplyVarsFilesWarnsOnOverriddenSensitiveKey(t *testing.T) {
	dir := t.TempDir()
	base := chmodTempFile(t, dir, "base.yml", "api_key: from-base\n", 0o644)
	prod := chmodTempFile(t, dir, "prod.yml", "api_key: from-prod\n", varsFileMode)

	args := map[string]*Argument{"api_key": sensitiveArg(t, "")}
	flags := flag.NewFlagSet("t", flag.ContinueOnError)

	_, warnings, err := applyVarsFiles(args, flags, []string{base, prod})
	if err != nil {
		t.Fatalf("applyVarsFiles failed: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want exactly one", warnings)
	}
	if !strings.Contains(warnings[0], base) {
		t.Errorf("warning should name the overridden base file\nfull: %s", warnings[0])
	}
	if strings.Contains(warnings[0], prod) {
		t.Errorf("the 0600 file must not be warned about\nfull: %s", warnings[0])
	}
}

// TestApplyVarsFilesRepeatedPathWarnsOnce: passing the same file twice is
// legal and must not double the warning.
func TestApplyVarsFilesRepeatedPathWarnsOnce(t *testing.T) {
	dir := t.TempDir()
	path := chmodTempFile(t, dir, "vars.yml", "api_key: s3cret\n", 0o644)

	args := map[string]*Argument{"api_key": sensitiveArg(t, "")}
	flags := flag.NewFlagSet("t", flag.ContinueOnError)

	_, warnings, err := applyVarsFiles(args, flags, []string{path, path})
	if err != nil {
		t.Fatalf("applyVarsFiles failed: %v", err)
	}
	if len(warnings) != 1 {
		t.Errorf("warnings = %v, want exactly one", warnings)
	}
}

// permissiveVarsRecipe is a recipe with one sensitive input and one stub task,
// paired with a 0644 vars-file supplying that input - the shape that has to
// produce a warning end to end.
func permissiveVarsRecipe(t *testing.T) (recipe string, vars string) {
	t.Helper()
	recipe = writeTasksFile(t, `---
- inputs:
    - name: api_key
      sensitive: true
      default: ""
  tasks:
    - name: noop
      dokku_stub: { key: a }
`)
	vars = chmodTempFile(t, t.TempDir(), "vars.yml", "api_key: s3cret\n", 0o644)
	return recipe, vars
}

// TestApplyWarnsOnPermissiveVarsFile pins where the warning comes out: stderr,
// through the same Ui.Warn the ambiguous-task-file notice uses.
func TestApplyWarnsOnPermissiveVarsFile(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: false})

	recipe, vars := permissiveVarsRecipe(t)
	stdout, stderr, exit := runApply(t, recipe, "--vars-file", vars)
	if exit != 0 {
		t.Fatalf("apply exit = %d, want 0\nstdout: %s\nstderr: %s", exit, stdout, stderr)
	}
	if !strings.Contains(stderr, "readable by other users") {
		t.Errorf("stderr missing the vars-file warning:\n%s", stderr)
	}
	if strings.Contains(stdout, "readable by other users") {
		t.Errorf("the warning belongs on stderr, not stdout:\n%s", stdout)
	}
}

// TestApplyJSONKeepsVarsFileWarningOffTheStream is the reason the warning is
// not an EventEmitter.TaskWarning: it has no task to name, and the event
// schema's reason field is a closed enum. Routed to stderr it cannot reach the
// stream at all, so every stdout line still parses.
func TestApplyJSONKeepsVarsFileWarningOffTheStream(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: false})

	recipe, vars := permissiveVarsRecipe(t)
	stdout, stderr, exit := runApply(t, recipe, "--json", "--vars-file", vars)
	if exit != 0 {
		t.Fatalf("apply exit = %d, want 0\nstdout: %s\nstderr: %s", exit, stdout, stderr)
	}
	if !strings.Contains(stderr, "readable by other users") {
		t.Errorf("stderr missing the vars-file warning:\n%s", stderr)
	}
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if line == "" {
			continue
		}
		var event map[string]interface{}
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("stdout line is not JSON: %q (%v)", line, err)
		}
	}
}
