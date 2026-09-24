package commands

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dokku/docket/subprocess"
	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/mitchellh/cli"
)

// testDocketVersion is the version both halves of a saved-plan round trip
// run as, unless a test is about the two disagreeing.
const testDocketVersion = "0.0.0-test"

// savedPlanResult is what one plan or apply invocation produced.
type savedPlanResult struct {
	stdout string
	stderr string
	exit   int
}

// runPlanSaving drives `docket plan` in dir with fixtures on the context and
// the given version. Relative --output paths land in dir.
func runPlanSaving(t *testing.T, ctx context.Context, dir, version string, args ...string) savedPlanResult {
	t.Helper()
	ui := cli.NewMockUi()
	c := &PlanCommand{
		Meta:    command.Meta{Ui: ui},
		Argv:    append([]string{"docket-test", "plan"}, args...),
		Ctx:     ctx,
		BaseDir: dir,
		Version: version,
	}
	exit := c.Run(args)
	return savedPlanResult{ui.OutputWriter.String(), ui.ErrorWriter.String(), exit}
}

// runApplySaved drives `docket apply` in dir with fixtures on the context and
// the given version. Relative --plan paths resolve against dir.
func runApplySaved(t *testing.T, ctx context.Context, dir, version string, args ...string) savedPlanResult {
	t.Helper()
	ui := cli.NewMockUi()
	c := &ApplyCommand{
		Meta:    command.Meta{Ui: ui},
		Argv:    append([]string{"docket-test", "apply"}, args...),
		Ctx:     ctx,
		BaseDir: dir,
		Version: version,
	}
	exit := c.Run(args)
	return savedPlanResult{ui.OutputWriter.String(), ui.ErrorWriter.String(), exit}
}

// stubCtx carries fixtures for one run. Plan and apply get separate ones so a
// test can change what the "server" answers between them.
func stubCtx(f stubFixtures) context.Context {
	return withStubFixtures(context.Background(), f)
}

// writeRecipeIn writes body as tasks.yml in a fresh temp dir and returns the
// dir and the recipe path.
func writeRecipeIn(t *testing.T, body string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write tasks.yml: %v", err)
	}
	return dir, path
}

// readSavedPlanFile loads and schema-checks a saved plan written by a test.
func readSavedPlanFile(t *testing.T, path string) (savedPlan, string) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved plan: %v", err)
	}
	assertMatchesSchema(t, planSchemaPath, string(raw))
	var p savedPlan
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatalf("decode saved plan: %v", err)
	}
	return p, string(raw)
}

const appKeyedRecipe = `---
- inputs:
    - name: app
      required: true
    - name: token
      sensitive: true
    - name: replicas
      type: int
      default: "2"
  tasks:
    - name: "deploy {{ .token }}"
      dokku_stub: { key: "{{ .app }}" }
`

// TestPlanOutputWritesSavedPlan pins the file itself: its mode, its schema,
// what it records about each input, and that a sensitive value appears only
// where apply needs it back - in the inputs, never in the masked events.
func TestPlanOutputWritesSavedPlan(t *testing.T) {
	t.Parallel()
	dir, path := writeRecipeIn(t, appKeyedRecipe)

	res := runPlanSaving(t, stubCtx(stubFixtures{"api": {Changed: true}}), dir, testDocketVersion,
		"--tasks", path, "--app", "api", "--token", "s3cret", "--output", "plan.json")
	if res.exit != 0 {
		t.Fatalf("plan exit = %d, stderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "Saved plan to plan.json") {
		t.Errorf("stderr should confirm the write; got:\n%s", res.stderr)
	}
	if strings.Contains(res.stdout, "s3cret") {
		t.Errorf("plan output leaked the sensitive input:\n%s", res.stdout)
	}

	planPath := filepath.Join(dir, "plan.json")
	if runtime.GOOS != "windows" {
		info, err := os.Stat(planPath)
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("saved plan mode = %04o, want 0600", got)
		}
	}

	p, raw := readSavedPlanFile(t, planPath)
	if p.Format != savedPlanFormat || p.Version != savedPlanVersion || p.DocketVersion != testDocketVersion {
		t.Errorf("header = %q/%d/%q", p.Format, p.Version, p.DocketVersion)
	}
	if p.Recipe.Format != "yaml" || p.Recipe.Data != appKeyedRecipe {
		t.Errorf("recipe not stored verbatim: format %q, data %q", p.Recipe.Format, p.Recipe.Data)
	}
	if got := p.Inputs["token"]; got != (savedPlanInput{Type: "string", Value: "s3cret", Sensitive: true, UserSet: true}) {
		t.Errorf("token input = %+v", got)
	}
	if got := p.Inputs["replicas"]; got != (savedPlanInput{Type: "int", Value: "2", HasDefault: true}) {
		t.Errorf("replicas input = %+v", got)
	}
	if n := strings.Count(raw, "s3cret"); n != 1 {
		t.Errorf("the secret should appear once, as the input value; found %d times:\n%s", n, raw)
	}
	if len(p.Events) != 2 || p.Events[0].Type != "play_start" || p.Events[1].Type != "task" {
		t.Fatalf("events = %+v", p.Events)
	}
	if got := p.Events[1]; got.Name != "deploy ***" || got.Status != "~" {
		t.Errorf("task event = %+v", got)
	}
}

// TestApplyPlanAppliesSavedPlan is the round trip: apply --plan takes the
// recipe and the inputs from the file, with none of them on its command
// line. The stub's key comes from the --app the plan was given, so a fixture
// answering under "api" proves the input travelled.
func TestApplyPlanAppliesSavedPlan(t *testing.T) {
	t.Parallel()
	dir, path := writeRecipeIn(t, appKeyedRecipe)
	fixtures := stubFixtures{"api": {Changed: true}}

	if res := runPlanSaving(t, stubCtx(fixtures), dir, testDocketVersion,
		"--tasks", path, "--app", "api", "--token", "s3cret", "--output", "plan.json"); res.exit != 0 {
		t.Fatalf("plan exit = %d, stderr:\n%s", res.exit, res.stderr)
	}
	// The recipe is gone by apply time: the plan carries its own copy.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	res := runApplySaved(t, stubCtx(fixtures), dir, testDocketVersion, "--plan", "plan.json", "--detailed-exitcode")
	if res.exit != 2 {
		t.Fatalf("apply exit = %d, want 2; stdout:\n%s\nstderr:\n%s", res.exit, res.stdout, res.stderr)
	}
	if !strings.Contains(res.stdout, "[changed] deploy ***") {
		t.Errorf("apply should run the saved task, masked; got:\n%s", res.stdout)
	}
	if strings.Contains(res.stdout+res.stderr, "s3cret") {
		t.Errorf("apply leaked the saved sensitive input:\nstdout:\n%s\nstderr:\n%s", res.stdout, res.stderr)
	}
}

// TestApplyPlanRefusesStalePlan: the server moved between plan and apply, so
// the fresh probe no longer matches and nothing runs.
func TestApplyPlanRefusesStalePlan(t *testing.T) {
	t.Parallel()
	dir, path := writeRecipeIn(t, `---
- tasks:
    - name: configure
      dokku_stub: { key: a }
`)
	if res := runPlanSaving(t, stubCtx(stubFixtures{"a": {Changed: true}}), dir, testDocketVersion,
		"--tasks", path, "--output", "plan.json"); res.exit != 0 {
		t.Fatalf("plan exit = %d, stderr:\n%s", res.exit, res.stderr)
	}

	var calls atomic.Int32
	res := runApplySaved(t, stubCtx(stubFixtures{"a": {Changed: false, Hook: func() { calls.Add(1) }}}),
		dir, testDocketVersion, "--plan", "plan.json")
	if res.exit != 1 {
		t.Fatalf("apply exit = %d, want 1", res.exit)
	}
	if !strings.Contains(res.stderr, "saved plan is stale: plan.json no longer matches") {
		t.Errorf("stderr should say the plan is stale; got:\n%s", res.stderr)
	}
	if !strings.Contains(res.stderr, `tasks/configure: planned "~", now "ok"`) {
		t.Errorf("stderr should name the task that moved; got:\n%s", res.stderr)
	}
	// One call is the check's Plan(); a second would be Execute().
	if got := calls.Load(); got != 1 {
		t.Errorf("stub reached %d times, want 1 (the check only)", got)
	}
	if res.stdout != "" {
		t.Errorf("a stale plan should print no run output; got:\n%s", res.stdout)
	}
}

// TestApplyPlanReportsProbeErrorAsStale: a probe that fails during the check
// is not hidden behind the silent emitter - the stale report carries it.
func TestApplyPlanReportsProbeErrorAsStale(t *testing.T) {
	t.Parallel()
	dir, path := writeRecipeIn(t, `---
- tasks:
    - name: configure
      dokku_stub: { key: a }
`)
	if res := runPlanSaving(t, stubCtx(stubFixtures{"a": {Changed: true}}), dir, testDocketVersion,
		"--tasks", path, "--output", "plan.json"); res.exit != 0 {
		t.Fatalf("plan exit = %d, stderr:\n%s", res.exit, res.stderr)
	}

	res := runApplySaved(t, stubCtx(stubFixtures{"a": {PlanError: errors.New("connection refused")}}),
		dir, testDocketVersion, "--plan", "plan.json")
	if res.exit != 1 {
		t.Fatalf("apply exit = %d, want 1", res.exit)
	}
	if !strings.Contains(res.stderr, `now "error: dokku: connection refused"`) {
		t.Errorf("stderr should carry the probe error; got:\n%s", res.stderr)
	}
}

// TestApplyPlanAbsorbsChangeAfterCheck: once the check passes, apply runs as
// it always does, each task re-reading the server. A task someone else
// reconciled in the meantime simply reports ok - that is not a stale plan.
func TestApplyPlanAbsorbsChangeAfterCheck(t *testing.T) {
	t.Parallel()
	dir, path := writeRecipeIn(t, `---
- tasks:
    - name: configure
      dokku_stub: { key: a }
`)
	if res := runPlanSaving(t, stubCtx(stubFixtures{"a": {Changed: true}}), dir, testDocketVersion,
		"--tasks", path, "--output", "plan.json"); res.exit != 0 {
		t.Fatalf("plan exit = %d, stderr:\n%s", res.exit, res.stderr)
	}

	res := runApplySaved(t, stubCtx(stubFixtures{"a": {Changed: true, ExecuteInSync: true}}),
		dir, testDocketVersion, "--plan", "plan.json", "--detailed-exitcode")
	if res.exit != 0 {
		t.Fatalf("apply exit = %d, want 0; stderr:\n%s", res.exit, res.stderr)
	}
	if strings.Contains(res.stderr, "stale") {
		t.Errorf("a change after the check is not staleness; got:\n%s", res.stderr)
	}
	if !strings.Contains(res.stdout, "[ok]      configure") {
		t.Errorf("the task should report ok; got:\n%s", res.stdout)
	}
}

// TestPlanOutputNotWrittenOnError: a plan that could not read every task is
// not saved.
func TestPlanOutputNotWrittenOnError(t *testing.T) {
	t.Parallel()
	dir, path := writeRecipeIn(t, `---
- tasks:
    - name: configure
      dokku_stub: { key: a }
`)
	res := runPlanSaving(t, stubCtx(stubFixtures{"a": {PlanError: errors.New("boom")}}), dir, testDocketVersion,
		"--tasks", path, "--output", "plan.json")
	if res.exit != 1 {
		t.Fatalf("plan exit = %d, want 1", res.exit)
	}
	if !strings.Contains(res.stderr, "plan has errors; not writing plan.json") {
		t.Errorf("stderr should say why nothing was written; got:\n%s", res.stderr)
	}
	if _, err := os.Stat(filepath.Join(dir, "plan.json")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("no plan should be written; stat err = %v", err)
	}
}

// TestPlanOutputFlagCombinations pins every --output / --force misuse, each
// refused before anything is probed.
func TestPlanOutputFlagCombinations(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		args []string
		want string
	}{
		"force without output": {[]string{"--force"}, "--force only applies to --output"},
		"stdout":               {[]string{"--output", "-"}, "--output cannot be -"},
		"empty":                {[]string{"--output="}, "--output needs a path"},
		"list-tasks":           {[]string{"--output", "plan.json", "--list-tasks"}, "--output cannot be used with --list-tasks"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir, path := writeRecipeIn(t, `---
- tasks:
    - name: configure
      dokku_stub: { key: a }
`)
			var calls atomic.Int32
			args := append([]string{"--tasks", path}, tc.args...)
			res := runPlanSaving(t, stubCtx(stubFixtures{"a": {Hook: func() { calls.Add(1) }}}), dir, testDocketVersion, args...)
			if res.exit != 1 {
				t.Fatalf("exit = %d, want 1", res.exit)
			}
			if !strings.Contains(res.stderr, tc.want) {
				t.Errorf("stderr should contain %q; got:\n%s", tc.want, res.stderr)
			}
			if calls.Load() != 0 {
				t.Error("a rejected flag combination should probe nothing")
			}
		})
	}
}

// TestPlanOutputOverwriteNeedsForce: an existing file is replaced only with
// --force, the refusal comes before any probe, and a forced overwrite of a
// world-readable file still lands at 0600.
func TestPlanOutputOverwriteNeedsForce(t *testing.T) {
	t.Parallel()
	dir, path := writeRecipeIn(t, `---
- tasks:
    - name: configure
      dokku_stub: { key: a }
`)
	planPath := filepath.Join(dir, "plan.json")
	if err := os.WriteFile(planPath, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	ctx := stubCtx(stubFixtures{"a": {Changed: true, Hook: func() { calls.Add(1) }}})
	res := runPlanSaving(t, ctx, dir, testDocketVersion, "--tasks", path, "--output", "plan.json")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1", res.exit)
	}
	if !strings.Contains(res.stderr, "file plan.json already exists; pass --force to overwrite") {
		t.Errorf("stderr = %s", res.stderr)
	}
	if calls.Load() != 0 {
		t.Error("the overwrite refusal should come before any probe")
	}
	if raw, _ := os.ReadFile(planPath); string(raw) != "keep" {
		t.Errorf("the existing file was touched: %q", raw)
	}

	res = runPlanSaving(t, ctx, dir, testDocketVersion, "--tasks", path, "--output", "plan.json", "--force")
	if res.exit != 0 {
		t.Fatalf("forced exit = %d, stderr:\n%s", res.exit, res.stderr)
	}
	readSavedPlanFile(t, planPath)
	if runtime.GOOS != "windows" {
		info, err := os.Stat(planPath)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o600 {
			t.Errorf("forced overwrite left mode %04o, want 0600", got)
		}
	}
}

// TestApplyPlanFlagCombinations pins every flag --plan refuses. Each one is
// refused before the plan file is read - the path here does not exist - and
// a --vars-file in particular is refused rather than read.
func TestApplyPlanFlagCombinations(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		args []string
		want string
	}{
		"tasks":                {[]string{"--tasks", "tasks.yml"}, "--plan cannot be used with --tasks"},
		"tasks-format":         {[]string{"--tasks-format", "yaml"}, "--plan cannot be used with --tasks-format"},
		"vars-file":            {[]string{"--vars-file", "missing.yml"}, "--plan cannot be used with --vars-file"},
		"play":                 {[]string{"--play", "p"}, "--plan cannot be used with --play"},
		"tags":                 {[]string{"--tags", "t"}, "--plan cannot be used with --tags"},
		"skip-tags":            {[]string{"--skip-tags", "t"}, "--plan cannot be used with --skip-tags"},
		"host":                 {[]string{"--host", "h"}, "--plan cannot be used with --host"},
		"sudo":                 {[]string{"--sudo"}, "--plan cannot be used with --sudo"},
		"accept-new-host-keys": {[]string{"--accept-new-host-keys"}, "--plan cannot be used with --accept-new-host-keys"},
		"start-at-task":        {[]string{"--start-at-task", "x"}, "--plan cannot be used with --start-at-task"},
		"list-tasks":           {[]string{"--list-tasks"}, "--plan cannot be used with --list-tasks"},
		"positional recipe":    {[]string{"tasks.yml"}, "--plan cannot be used with a recipe argument"},
		"input flag":           {[]string{"--app", "api"}, "unknown flag: --app"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			args := append([]string{"--plan", "missing-plan.json"}, tc.args...)
			res := runApplySaved(t, context.Background(), t.TempDir(), testDocketVersion, args...)
			if res.exit != 1 {
				t.Fatalf("exit = %d, want 1", res.exit)
			}
			if !strings.Contains(res.stderr, tc.want) {
				t.Errorf("stderr should contain %q; got:\n%s", tc.want, res.stderr)
			}
		})
	}

	t.Run("empty path", func(t *testing.T) {
		t.Parallel()
		res := runApplySaved(t, context.Background(), t.TempDir(), testDocketVersion, "--plan=")
		if res.exit != 1 || !strings.Contains(res.stderr, "--plan needs a path") {
			t.Errorf("exit = %d, stderr:\n%s", res.exit, res.stderr)
		}
	})
}

// savePlainPlan writes a one-task saved plan in a fresh dir and returns the
// dir, for tests about reading the file back.
func savePlainPlan(t *testing.T, version string) string {
	t.Helper()
	dir, path := writeRecipeIn(t, `---
- tasks:
    - name: configure
      dokku_stub: { key: a }
`)
	if res := runPlanSaving(t, stubCtx(nil), dir, version, "--tasks", path, "--output", "plan.json"); res.exit != 0 {
		t.Fatalf("plan exit = %d, stderr:\n%s", res.exit, res.stderr)
	}
	return dir
}

// TestApplyPlanRefusesOtherDocketVersion: a plan is applied only by the
// build that wrote it.
func TestApplyPlanRefusesOtherDocketVersion(t *testing.T) {
	t.Parallel()
	dir := savePlainPlan(t, "1.0.0")
	res := runApplySaved(t, context.Background(), dir, "2.0.0", "--plan", "plan.json")
	if res.exit != 1 {
		t.Fatalf("exit = %d, want 1", res.exit)
	}
	if !strings.Contains(res.stderr, "was saved by docket 1.0.0, but this is docket 2.0.0") {
		t.Errorf("stderr = %s", res.stderr)
	}
}

// TestApplyPlanRejectsForeignDocuments: format and version are checked
// before anything in the file is trusted.
func TestApplyPlanRejectsForeignDocuments(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		edit func(map[string]interface{})
		want string
	}{
		"wrong format":   {func(m map[string]interface{}) { m["format"] = "terraform" }, `is not a docket saved plan (format "terraform"`},
		"future version": {func(m map[string]interface{}) { m["version"] = 2 }, "is saved plan version 2; this docket reads version 1"},
		"unknown field":  {func(m map[string]interface{}) { m["extra"] = true }, "is not a docket saved plan"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			dir := savePlainPlan(t, testDocketVersion)
			planPath := filepath.Join(dir, "plan.json")
			raw, err := os.ReadFile(planPath)
			if err != nil {
				t.Fatal(err)
			}
			var m map[string]interface{}
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			tc.edit(m)
			raw, _ = json.Marshal(m)
			if err := os.WriteFile(planPath, raw, 0o600); err != nil {
				t.Fatal(err)
			}
			res := runApplySaved(t, context.Background(), dir, testDocketVersion, "--plan", "plan.json")
			if res.exit != 1 || !strings.Contains(res.stderr, tc.want) {
				t.Errorf("exit = %d, want 1 with %q; stderr:\n%s", res.exit, tc.want, res.stderr)
			}
		})
	}
}

// TestApplyPlanWarnsOnReadableFile: the plan holds secrets, so a mode that
// lets other users read it is called out - and the run still goes ahead.
func TestApplyPlanWarnsOnReadableFile(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("no mode bits to read")
	}
	dir := savePlainPlan(t, testDocketVersion)
	if err := os.Chmod(filepath.Join(dir, "plan.json"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := runApplySaved(t, context.Background(), dir, testDocketVersion, "--plan", "plan.json")
	if res.exit != 0 {
		t.Fatalf("exit = %d, stderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stderr, "warning: saved plan plan.json holds secrets and is readable by other users (mode 0644); chmod 600 plan.json") {
		t.Errorf("stderr = %s", res.stderr)
	}
}

// TestApplyPlanUsesSavedTarget: the target is frozen with the plan, so the
// DOKKU_HOST of the apply environment does not redirect it. Not parallel:
// it sets a process environment variable.
func TestApplyPlanUsesSavedTarget(t *testing.T) {
	t.Setenv("DOKKU_HOST", "env@other.example.com")
	dir, path := writeRecipeIn(t, `---
- tasks:
    - name: app
      dokku_app: { app: a }
`)
	seen := map[string]int{}
	ctx := subprocess.ContextWithRunner(context.Background(),
		func(ctx context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
			seen[subprocess.TargetFromContext(ctx).Host]++
			return subprocess.ExecCommandResponse{}, nil
		})

	if res := runPlanSaving(t, ctx, dir, testDocketVersion, "--tasks", path, "--host", "saved@example.com", "--output", "plan.json"); res.exit != 0 {
		t.Fatalf("plan exit = %d, stderr:\n%s", res.exit, res.stderr)
	}
	p, _ := readSavedPlanFile(t, filepath.Join(dir, "plan.json"))
	if p.Target.Host != "saved@example.com" {
		t.Errorf("saved target = %+v", p.Target)
	}

	clear(seen)
	res := runApplySaved(t, ctx, dir, testDocketVersion, "--plan", "plan.json")
	if res.exit != 0 {
		t.Fatalf("apply exit = %d, stderr:\n%s", res.exit, res.stderr)
	}
	if seen["env@other.example.com"] != 0 {
		t.Errorf("apply --plan followed DOKKU_HOST: %v", seen)
	}
	if seen["saved@example.com"] == 0 {
		t.Errorf("apply --plan should run against the saved target: %v", seen)
	}
}

// TestApplyPlanKeepsSavedFilters: --play and --tags are frozen with the plan
// and narrow the apply exactly as they narrowed the plan.
func TestApplyPlanKeepsSavedFilters(t *testing.T) {
	t.Parallel()
	dir, path := writeRecipeIn(t, `---
- name: one
  tasks:
    - name: tagged
      tags: [deploy]
      dokku_stub: { key: a }
    - name: untagged
      dokku_stub: { key: b }
- name: two
  tasks:
    - name: other-play
      tags: [deploy]
      dokku_stub: { key: c }
`)
	fixtures := stubFixtures{"a": {Changed: true}, "b": {Changed: true}, "c": {Changed: true}}
	if res := runPlanSaving(t, stubCtx(fixtures), dir, testDocketVersion,
		"--tasks", path, "--play", "one", "--tags", "deploy", "--output", "plan.json"); res.exit != 0 {
		t.Fatalf("plan exit = %d, stderr:\n%s", res.exit, res.stderr)
	}
	p, _ := readSavedPlanFile(t, filepath.Join(dir, "plan.json"))
	if p.Play != "one" || len(p.Tags) != 1 || p.Tags[0] != "deploy" {
		t.Errorf("saved filters = play %q tags %v", p.Play, p.Tags)
	}

	res := runApplySaved(t, stubCtx(fixtures), dir, testDocketVersion, "--plan", "plan.json")
	if res.exit != 0 {
		t.Fatalf("apply exit = %d, stderr:\n%s", res.exit, res.stderr)
	}
	if !strings.Contains(res.stdout, "[changed] tagged") {
		t.Errorf("the tagged task in the saved play should run; got:\n%s", res.stdout)
	}
	for _, name := range []string{"untagged", "other-play"} {
		if strings.Contains(res.stdout, "[changed] "+name) {
			t.Errorf("%s is outside the saved filters but ran; got:\n%s", name, res.stdout)
		}
	}
}

// TestApplyPlanJSONMatchesEventsSchema: the stream apply --plan emits is the
// ordinary apply stream - the check itself emits nothing into it.
func TestApplyPlanJSONMatchesEventsSchema(t *testing.T) {
	t.Parallel()
	dir, path := writeRecipeIn(t, `---
- tasks:
    - name: configure
      dokku_stub: { key: a }
`)
	fixtures := stubFixtures{"a": {Changed: true}}
	if res := runPlanSaving(t, stubCtx(fixtures), dir, testDocketVersion, "--tasks", path, "--json", "--output", "plan.json"); res.exit != 0 {
		t.Fatalf("plan exit = %d, stderr:\n%s", res.exit, res.stderr)
	} else {
		assertLinesMatchSchema(t, eventsSchemaPath, res.stdout)
	}

	res := runApplySaved(t, stubCtx(fixtures), dir, testDocketVersion, "--plan", "plan.json", "--json")
	if res.exit != 0 {
		t.Fatalf("apply exit = %d, stderr:\n%s", res.exit, res.stderr)
	}
	assertLinesMatchSchema(t, eventsSchemaPath, res.stdout)
	var types []string
	for _, line := range jsonLines(res.stdout) {
		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatal(err)
		}
		types = append(types, ev["type"].(string))
	}
	if got := strings.Join(types, ","); got != "play_start,task,summary" {
		t.Errorf("event types = %s, want one apply run's worth", got)
	}
}

// TestStaleSavedPlanDiff pins the comparison and its report.
func TestStaleSavedPlanDiff(t *testing.T) {
	t.Parallel()
	start := savedPlanEvent{Type: "play_start", Name: "tasks"}
	drift := savedPlanEvent{Type: "task", Play: "tasks", Name: "a", Status: "~", Reason: "1 key(s) to set", Commands: []string{"dokku config:set"}}

	if got := staleSavedPlanDiff([]savedPlanEvent{start, drift}, []savedPlanEvent{start, drift}); len(got) != 0 {
		t.Errorf("identical events should not differ: %v", got)
	}

	otherCmd := drift
	otherCmd.Commands = []string{"dokku config:unset"}
	got := staleSavedPlanDiff([]savedPlanEvent{drift}, []savedPlanEvent{otherCmd})
	if len(got) != 1 || got[0] != `tasks/a: "~ 1 key(s) to set", but its mutations or commands differ` {
		t.Errorf("same verdict, other commands: %v", got)
	}

	got = staleSavedPlanDiff([]savedPlanEvent{start, drift}, []savedPlanEvent{start})
	if len(got) != 1 || got[0] != `tasks/a: planned "~ 1 key(s) to set", now "absent"` {
		t.Errorf("missing event: %v", got)
	}

	child := savedPlanEvent{Type: "task", Play: "tasks", Name: "c", Phase: "rescue", Status: "ok"}
	got = staleSavedPlanDiff(nil, []savedPlanEvent{child})
	if len(got) != 1 || got[0] != `tasks/[rescue] c: planned "absent", now "ok"` {
		t.Errorf("extra event: %v", got)
	}

	var saved, now []savedPlanEvent
	for i := 0; i < savedPlanStaleLimit+3; i++ {
		saved = append(saved, drift)
		now = append(now, savedPlanEvent{Type: "task", Play: "tasks", Name: "a", Status: "ok"})
	}
	got = staleSavedPlanDiff(saved, now)
	if len(got) != savedPlanStaleLimit+1 || got[len(got)-1] != "... and 3 more" {
		t.Errorf("limit: %v", got)
	}
}

// TestSavedPlanArgumentsRoundTrip: every input type comes back as the Go type
// the render context holds, so int stays int rather than JSON's float64.
func TestSavedPlanArgumentsRoundTrip(t *testing.T) {
	t.Parallel()
	p := &savedPlan{Inputs: map[string]savedPlanInput{
		"s": {Type: "string", Value: "x", UserSet: true},
		"i": {Type: "int", Value: "3", HasDefault: true},
		"f": {Type: "float", Value: "1.5", HasDefault: true},
		"b": {Type: "bool", Value: "true", HasDefault: true},
	}}
	args, userSet, err := p.arguments()
	if err != nil {
		t.Fatal(err)
	}
	ctx, _, err := buildInputContext(args, userSet)
	if err != nil {
		t.Fatal(err)
	}
	if ctx["s"] != "x" || ctx["i"] != 3 || ctx["f"] != 1.5 || ctx["b"] != true {
		t.Errorf("context = %#v", ctx)
	}
	if !userSet["s"] || userSet["i"] {
		t.Errorf("userSet = %v", userSet)
	}

	bad := &savedPlan{Inputs: map[string]savedPlanInput{"i": {Type: "int", Value: "three"}}}
	if _, _, err := bad.arguments(); err == nil || !strings.Contains(err.Error(), `"three" is not an int`) {
		t.Errorf("bad int error = %v", err)
	}
}

// TestPlanFlagFromArgs pins the pre-parse sniff apply uses to skip reading a
// recipe for input flags.
func TestPlanFlagFromArgs(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"docket apply --plan p.json":    true,
		"docket apply --plan=p.json":    true,
		"docket apply --tasks t.yml":    false,
		"docket apply --planet x":       false,
		"docket apply -- --plan p.json": false,
	}
	for argv, want := range cases {
		if got := planFlagFromArgs(strings.Fields(argv)); got != want {
			t.Errorf("%q: got %v, want %v", argv, got, want)
		}
	}
}
