package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/mitchellh/cli"
)

// These tests drive the multi-play executor on the plan path so the
// assertions stay deterministic without a Dokku server. Apply-only
// behaviour (--fail-fast vs default abort-current-play) is exercised
// against real Dokku in tests/bats/plays.bats.

func writeMultiPlayTasks(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write tasks.yml: %v", err)
	}
	return path
}

// runPlan stages os.Args because PlanCommand.FlagSet() reads --tasks
// from os.Args (so it can pre-register input flags before flag.Parse
// runs against the per-call args).
func runPlan(t *testing.T, path string, args ...string) (string, string, int) {
	t.Helper()
	origArgs := os.Args
	os.Args = []string{"docket-test", "plan", "--tasks", path}
	t.Cleanup(func() { os.Args = origArgs })

	c := &PlanCommand{}
	c.Meta = command.Meta{Ui: cli.NewMockUi()}
	all := append([]string{"--tasks", path}, args...)
	exit := c.Run(all)
	ui := c.Ui.(*cli.MockUi)
	return ui.OutputWriter.String(), ui.ErrorWriter.String(), exit
}

// TestMultiPlayPlanRunsAllPlaysInOrder is the headline multi-play case:
// two plays both produce a `==> Play:` header in source order.
func TestMultiPlayPlanRunsAllPlaysInOrder(t *testing.T) {
	path := writeMultiPlayTasks(t, `---
- name: api
  tasks:
    - name: noop-api
      dokku_app:
        app: docket-test-noop-api
- name: worker
  tasks:
    - name: noop-worker
      dokku_app:
        app: docket-test-noop-worker
`)
	stdout, _, _ := runPlan(t, path)
	idxAPI := strings.Index(stdout, "==> Play: api")
	idxWorker := strings.Index(stdout, "==> Play: worker")
	if idxAPI < 0 || idxWorker < 0 {
		t.Fatalf("missing play headers; got:\n%s", stdout)
	}
	if idxAPI > idxWorker {
		t.Errorf("plays out of order; api should come before worker; got:\n%s", stdout)
	}
}

// TestMultiPlayPlayFilterRunsOnlyNamedPlay covers --play. Only the
// matched play should produce a header; the other play's tasks should
// not appear.
func TestMultiPlayPlayFilterRunsOnlyNamedPlay(t *testing.T) {
	path := writeMultiPlayTasks(t, `---
- name: api
  tasks:
    - name: noop-api
      dokku_app:
        app: docket-test-noop-api
- name: worker
  tasks:
    - name: noop-worker
      dokku_app:
        app: docket-test-noop-worker
`)
	stdout, _, _ := runPlan(t, path, "--play", "api")
	if !strings.Contains(stdout, "==> Play: api") {
		t.Errorf("api play header missing; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "==> Play: worker") {
		t.Errorf("worker play should be filtered out; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "noop-worker") {
		t.Errorf("worker tasks should not appear; got:\n%s", stdout)
	}
}

// TestMultiPlayPlayFilterUnknownNameErrors verifies the user sees a
// useful diagnostic when --play does not match any play, including the
// available play names so they can correct the typo.
func TestMultiPlayPlayFilterUnknownNameErrors(t *testing.T) {
	path := writeMultiPlayTasks(t, `---
- name: api
  tasks:
    - name: noop
      dokku_app:
        app: docket-test-noop-api
- name: worker
  tasks:
    - name: noop
      dokku_app:
        app: docket-test-noop-worker
`)
	_, stderr, exit := runPlan(t, path, "--play", "missing")
	if exit == 0 {
		t.Error("expected non-zero exit when --play does not match")
	}
	for _, want := range []string{`--play "missing"`, `"api"`, `"worker"`} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q; got:\n%s", want, stderr)
		}
	}
}

// TestMultiPlayPlayFilterComposesWithTags pins the --play + --tags
// composition: --play narrows to one play, --tags then filters tasks
// within that play.
func TestMultiPlayPlayFilterComposesWithTags(t *testing.T) {
	path := writeMultiPlayTasks(t, `---
- name: api
  tasks:
    - name: deploy-api
      tags: [deploy]
      dokku_app:
        app: docket-test-deploy-api
    - name: configure-api
      tags: [configure]
      dokku_app:
        app: docket-test-configure-api
- name: worker
  tasks:
    - name: deploy-worker
      tags: [deploy]
      dokku_app:
        app: docket-test-deploy-worker
`)
	stdout, _, _ := runPlan(t, path, "--play", "api", "--tags", "deploy")
	if !strings.Contains(stdout, "deploy-api") {
		t.Errorf("deploy-api should run; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "configure-api") {
		t.Errorf("configure-api should be filtered by --tags; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "deploy-worker") {
		t.Errorf("worker play tasks should be filtered by --play; got:\n%s", stdout)
	}
}

// TestMultiPlayPlayLevelTagsPropagateToTasks: --tags filters against
// the task's effective tag set, which includes the play's tags. A task
// with no per-task tag still passes a --tags filter that matches the
// play tag.
func TestMultiPlayPlayLevelTagsPropagateToTasks(t *testing.T) {
	path := writeMultiPlayTasks(t, `---
- name: api
  tags: [api]
  tasks:
    - name: deploy-api
      dokku_app:
        app: docket-test-deploy-api
- name: worker
  tags: [worker]
  tasks:
    - name: deploy-worker
      dokku_app:
        app: docket-test-deploy-worker
`)
	stdout, _, _ := runPlan(t, path, "--tags", "api")
	if !strings.Contains(stdout, "deploy-api") {
		t.Errorf("deploy-api should match play tag; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "deploy-worker") {
		t.Errorf("deploy-worker should be filtered by --tags; got:\n%s", stdout)
	}
}

// TestMultiPlayWhenSkippedShowsInSummary: skipped plays count toward
// the new "n play skipped" summary segment.
func TestMultiPlayWhenSkippedShowsInSummary(t *testing.T) {
	path := writeMultiPlayTasks(t, `---
- inputs:
    - name: env
      default: dev
- name: api
  when: 'env == "prod"'
  tasks:
    - name: noop
      dokku_app:
        app: docket-test-noop-api
- name: worker
  tasks:
    - name: noop
      dokku_app:
        app: docket-test-noop-worker
`)
	stdout, _, _ := runPlan(t, path)
	if !strings.Contains(stdout, "1 play skipped") {
		t.Errorf("summary should include `1 play skipped`; got:\n%s", stdout)
	}
}
