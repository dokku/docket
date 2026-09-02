package commands

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/mitchellh/cli"
)

// runApplyWithTarget drives apply with a recording runner on the context, so
// the test can see which host each play's tasks were routed to without a
// server. The stub task does not reach subprocess, so this uses a real task
// type and lets the runner answer for it.
func runApplyWithTarget(t *testing.T, path string, args ...string) (map[string][]string, string) {
	t.Helper()
	origArgs := os.Args
	os.Args = []string{"docket-test", "apply", "--tasks", path}
	t.Cleanup(func() { os.Args = origArgs })

	seen := map[string][]string{}
	ctx := subprocess.ContextWithRunner(context.Background(),
		func(ctx context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
			host := subprocess.TargetFromContext(ctx).Host
			seen[host] = append(seen[host], strings.Join(in.Args, " "))
			return subprocess.ExecCommandResponse{}, nil
		})

	ui := cli.NewMockUi()
	c := &ApplyCommand{Meta: command.Meta{Ui: ui}, Ctx: ctx}
	c.Run(append([]string{"--tasks", path}, args...))
	return seen, ui.OutputWriter.String()
}

const twoHostRecipe = `---
- name: local play
  tasks:
    - name: first
      dokku_app: { app: a }
- name: remote play
  host: deploy@remote.example.com
  tasks:
    - name: second
      dokku_app: { app: b }
`

// TestApplyRoutesEachPlayToItsOwnHost is the headline case #500 asks for: one
// recipe, two servers. Before the target was per-invocation this was not
// expressible at all.
func TestApplyRoutesEachPlayToItsOwnHost(t *testing.T) {
	path := writeTasksFile(t, twoHostRecipe)
	seen, stdout := runApplyWithTarget(t, path)

	local, remote := seen[""], seen["deploy@remote.example.com"]
	if len(local) == 0 {
		t.Error("the play declaring no host should have run against the local target")
	}
	if len(remote) == 0 {
		t.Fatal("the play declaring a host should have run against it")
	}
	// The app name is the last argument, so match on that rather than a
	// substring - "--quiet apps:exists" contains " a" all by itself.
	for _, args := range local {
		if strings.HasSuffix(args, " b") {
			t.Errorf("the remote play's app reached the local target: %q", args)
		}
	}
	for _, args := range remote {
		if strings.HasSuffix(args, " a") {
			t.Errorf("the local play's app reached the remote target: %q", args)
		}
	}
	if !strings.Contains(stdout, "(host: deploy@remote.example.com)") {
		t.Errorf("the play header should name the play's own host; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "local play  (host:") {
		t.Errorf("a play with no host of its own should not claim one; got:\n%s", stdout)
	}
}

// TestApplyPlayHostOverridesTheRunWideFlag pins precedence: the play wins over
// --host for its own tasks, and the plays that declare nothing still follow
// the flag.
func TestApplyPlayHostOverridesTheRunWideFlag(t *testing.T) {
	path := writeTasksFile(t, twoHostRecipe)
	seen, _ := runApplyWithTarget(t, path, "--host", "run@cli.example.com")

	if len(seen["run@cli.example.com"]) == 0 {
		t.Error("the play declaring no host should follow --host")
	}
	if len(seen["deploy@remote.example.com"]) == 0 {
		t.Error("the play declaring a host should override --host")
	}
	if _, ok := seen[""]; ok {
		t.Error("nothing should have run against an empty target")
	}
}

// TestApplyPlaySudoInheritsAndDeclines pins the inheritance rule: a play
// overrides only the keys it names, so --sudo follows a play to its own host
// unless the play says otherwise. The alternative - a play's `host:` meaning
// "ignore everything the run was given" - would make a two-server migration
// restate the run's flags on every play.
func TestApplyPlaySudoInheritsAndDeclines(t *testing.T) {
	path := writeTasksFile(t, `---
- name: inherits
  host: deploy@one.example.com
  tasks:
    - name: a
      dokku_app: { app: a }
- name: declines
  host: deploy@two.example.com
  sudo: false
  tasks:
    - name: b
      dokku_app: { app: b }
`)

	origArgs := os.Args
	os.Args = []string{"docket-test", "apply", "--tasks", path}
	t.Cleanup(func() { os.Args = origArgs })

	sudoByHost := map[string]bool{}
	ctx := subprocess.ContextWithRunner(context.Background(),
		func(ctx context.Context, _ subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
			target := subprocess.TargetFromContext(ctx)
			sudoByHost[target.Host] = target.Sudo
			return subprocess.ExecCommandResponse{}, nil
		})

	c := &ApplyCommand{Meta: command.Meta{Ui: cli.NewMockUi()}, Ctx: ctx}
	c.Run([]string{"--tasks", path, "--sudo"})

	if !sudoByHost["deploy@one.example.com"] {
		t.Error("a play that redirects but says nothing about sudo should keep the run's --sudo")
	}
	if sudoByHost["deploy@two.example.com"] {
		t.Error("`sudo: false` should decline the run's --sudo for that play")
	}
}

// TestPlanRoutesEachPlayToItsOwnHost mirrors the apply case: plan probes the
// server for every task, so it needs the same per-play routing or it would
// report drift read off the wrong machine.
func TestPlanRoutesEachPlayToItsOwnHost(t *testing.T) {
	path := writeTasksFile(t, twoHostRecipe)

	origArgs := os.Args
	os.Args = []string{"docket-test", "plan", "--tasks", path}
	t.Cleanup(func() { os.Args = origArgs })

	seen := map[string]bool{}
	ctx := subprocess.ContextWithRunner(context.Background(),
		func(ctx context.Context, _ subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
			seen[subprocess.TargetFromContext(ctx).Host] = true
			return subprocess.ExecCommandResponse{}, nil
		})

	ui := cli.NewMockUi()
	c := &PlanCommand{Meta: command.Meta{Ui: ui}, Ctx: ctx}
	c.Run([]string{"--tasks", path})

	if !seen[""] || !seen["deploy@remote.example.com"] {
		t.Errorf("plan should probe both targets; saw %v", seen)
	}
	if !strings.Contains(ui.OutputWriter.String(), "(host: deploy@remote.example.com)") {
		t.Errorf("the plan play header should name the play's host; got:\n%s", ui.OutputWriter.String())
	}
}
