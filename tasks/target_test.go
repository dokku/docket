package tasks

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/dokku/docket/subprocess"
)

// recordingRunner answers `apps:exists` differently per host and records which
// host each command was routed to. The shared fakeDokku keys only on the joined
// args and drops the target entirely, which is exactly the blindness #423
// describes - so host-aware tests need their own fake rather than a change to
// the one every other test depends on.
func recordingRunner(existsOn map[string]bool, seen *sync.Map) subprocess.ExecRunner {
	return func(ctx context.Context, input subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		host := subprocess.TargetFromContext(ctx).Host
		seen.Store(host, true)
		if strings.Contains(strings.Join(input.Args, " "), "apps:exists") && !existsOn[host] {
			return subprocess.ExecCommandResponse{ExitCode: 1}, &subprocess.ExecError{Response: subprocess.ExecCommandResponse{ExitCode: 1}, Ran: true}
		}
		return subprocess.ExecCommandResponse{}, nil
	}
}

// TestPlanRoutesToTheContextTarget pins that the target reaches a task at all.
// No task has ever set a host on the command it builds, so before the target
// travelled on the context the only thing deciding where a dokku command went
// was a package variable the task could not see.
func TestPlanRoutesToTheContextTarget(t *testing.T) {
	t.Parallel()

	var seen sync.Map
	ctx := subprocess.ContextWithRunner(context.Background(), recordingRunner(nil, &seen))
	ctx = subprocess.ContextWithTarget(ctx, subprocess.Target{Host: "deploy@one"})

	AppTask{App: "demo", State: StatePresent}.Plan(ctx)

	if _, ok := seen.Load("deploy@one"); !ok {
		var hosts []string
		seen.Range(func(k, _ any) bool { hosts = append(hosts, k.(string)); return true })
		t.Errorf("the probe did not reach the context's target; hosts seen: %v", hosts)
	}
}

// TestPlanAgainstTwoTargetsConcurrently is the tasks-layer acceptance test for
// #423: the same task planned against two servers at once, each answer coming
// from the server that goroutine addressed. This is the case the process-global
// host made impossible - and it runs in parallel, which nothing in this repo
// could do while the executor was a package variable too.
func TestPlanAgainstTwoTargetsConcurrently(t *testing.T) {
	t.Parallel()

	// The app exists on one host and not the other, so a plan that routed to
	// the wrong server reports the wrong verdict rather than merely the wrong
	// host in a log line.
	existsOn := map[string]bool{"deploy@has-it": true, "deploy@lacks-it": false}

	var seen sync.Map
	runner := recordingRunner(existsOn, &seen)

	results := make(map[string]PlanResult, len(existsOn))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for host := range existsOn {
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			ctx := subprocess.ContextWithRunner(context.Background(), runner)
			ctx = subprocess.ContextWithTarget(ctx, subprocess.Target{Host: host})
			plan := AppTask{App: "demo", State: StatePresent}.Plan(ctx)
			mu.Lock()
			defer mu.Unlock()
			results[host] = plan
		}(host)
	}
	wg.Wait()

	if got := results["deploy@has-it"]; !got.InSync {
		t.Errorf("the host that has the app should plan in sync; got status %q reason %q", got.Status, got.Reason)
	}
	if got := results["deploy@lacks-it"]; got.InSync {
		t.Error("the host that lacks the app should plan a create, not in sync")
	}
}
