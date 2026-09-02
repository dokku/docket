package subprocess

import (
	"context"
	"fmt"
	"sync"
	"testing"
)

func TestContextWithTargetRoundTrip(t *testing.T) {
	t.Parallel()

	want := Target{Host: "deploy@dokku.example.com:2222", Sudo: true, AcceptNewHostKeys: true}
	if got := TargetFromContext(ContextWithTarget(context.Background(), want)); got != want {
		t.Errorf("TargetFromContext = %+v, want %+v", got, want)
	}
}

// TestTargetFromContextDefaultsToLocal pins the zero value. A context that
// carries no target - a library caller who never set one, or docket run
// without --host - runs dokku locally without elevation, which is the
// behaviour the CLI has when neither flag nor env var is set.
func TestTargetFromContextDefaultsToLocal(t *testing.T) {
	t.Parallel()

	if got := TargetFromContext(context.Background()); got != (Target{}) {
		t.Errorf("TargetFromContext = %+v, want the zero Target", got)
	}
	//nolint:staticcheck // deliberately passing nil: a caller not yet threaded
	// a context should run locally rather than panic.
	if got := TargetFromContext(nil); got != (Target{}) {
		t.Errorf("TargetFromContext(nil) = %+v, want the zero Target", got)
	}
}

// TestChildContextOverridesTheTarget pins how a single call is sent somewhere
// else: by deriving a context, not by setting a field on the input. That is
// what keeps "which server" a question with exactly one answer per call, and
// it is the mechanism a per-play host would use.
func TestChildContextOverridesTheTarget(t *testing.T) {
	t.Parallel()

	run := ContextWithTarget(context.Background(), Target{Host: "a@one"})
	child := ContextWithTarget(run, Target{Host: "b@two"})

	if got := TargetFromContext(child).Host; got != "b@two" {
		t.Errorf("child target host = %q, want %q", got, "b@two")
	}
	if got := TargetFromContext(run).Host; got != "a@one" {
		t.Errorf("deriving a child must not disturb the parent; parent host = %q", got)
	}
}

// TestConcurrentTargetsDoNotInterfere is the acceptance test for #423: two
// targets in flight at once inside one process, each command reaching the
// server its own caller asked for. While the host lived in a package variable
// this was not expressible at all - the second caller to set it silently
// redirected the first one's commands.
func TestConcurrentTargetsDoNotInterfere(t *testing.T) {
	var mu sync.Mutex
	seen := map[string][]string{}

	defer SetExecRunner(func(ctx context.Context, input ExecCommandInput) (ExecCommandResponse, error) {
		host := TargetFromContext(ctx).Host
		mu.Lock()
		defer mu.Unlock()
		seen[host] = append(seen[host], input.Args[len(input.Args)-1])
		return ExecCommandResponse{}, nil
	})()

	const perHost = 25
	hosts := []string{"alice@one", "bob@two"}

	var wg sync.WaitGroup
	for _, host := range hosts {
		for i := 0; i < perHost; i++ {
			wg.Add(1)
			go func(host string, i int) {
				defer wg.Done()
				ctx := ContextWithTarget(context.Background(), Target{Host: host})
				_, err := CallExecCommand(ctx, ExecCommandInput{
					Command: "dokku",
					Args:    []string{"apps:create", fmt.Sprintf("%s-%d", host, i)},
				})
				if err != nil {
					t.Errorf("CallExecCommand: %v", err)
				}
			}(host, i)
		}
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != len(hosts) {
		t.Fatalf("commands reached %d hosts, want %d: %v", len(seen), len(hosts), seen)
	}
	for _, host := range hosts {
		args := seen[host]
		if len(args) != perHost {
			t.Errorf("%s saw %d commands, want %d", host, len(args), perHost)
		}
		// Every app name is prefixed with the host its goroutine targeted, so a
		// command that reached the wrong host is visible in the payload rather
		// than only in the count.
		for _, arg := range args {
			if len(arg) < len(host) || arg[:len(host)] != host {
				t.Errorf("%s received a command meant for another host: %q", host, arg)
			}
		}
	}
}

// TestContextRunnerIsPerInvocation pins the seam #423 names as the reason no
// test in this repo could call t.Parallel(): with the executor on the context
// rather than in a package variable, two tests can install different fakes at
// the same time.
func TestContextRunnerIsPerInvocation(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"first", "second"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := ContextWithRunner(context.Background(), func(context.Context, ExecCommandInput) (ExecCommandResponse, error) {
				return ExecCommandResponse{Stdout: name}, nil
			})
			resp, err := CallExecCommand(ctx, ExecCommandInput{Command: "dokku", Args: []string{"version"}})
			if err != nil {
				t.Fatalf("CallExecCommand: %v", err)
			}
			if resp.Stdout != name {
				t.Errorf("stdout = %q, want %q; the other subtest's runner answered", resp.Stdout, name)
			}
		})
	}
}

// TestContextRunnerBeatsThePackageRunner pins the precedence the two seams
// have while both exist, so the tests still using SetExecRunner keep working
// and a context-scoped one is never quietly ignored.
func TestContextRunnerBeatsThePackageRunner(t *testing.T) {
	defer SetExecRunner(func(context.Context, ExecCommandInput) (ExecCommandResponse, error) {
		return ExecCommandResponse{Stdout: "package"}, nil
	})()

	ctx := ContextWithRunner(context.Background(), func(context.Context, ExecCommandInput) (ExecCommandResponse, error) {
		return ExecCommandResponse{Stdout: "context"}, nil
	})
	resp, err := CallExecCommand(ctx, ExecCommandInput{Command: "dokku"})
	if err != nil {
		t.Fatalf("CallExecCommand: %v", err)
	}
	if resp.Stdout != "context" {
		t.Errorf("stdout = %q, want the context runner to win", resp.Stdout)
	}

	// And a context without one still falls back to the package runner.
	resp, err = CallExecCommand(context.Background(), ExecCommandInput{Command: "dokku"})
	if err != nil {
		t.Fatalf("CallExecCommand: %v", err)
	}
	if resp.Stdout != "package" {
		t.Errorf("stdout = %q, want the package runner as the fallback", resp.Stdout)
	}
}

func TestValidateHost(t *testing.T) {
	t.Parallel()

	valid := []string{
		"example.com",
		"deploy@example.com",
		"deploy@example.com:2222",
		"[2001:db8::1]:2222",
	}
	for _, raw := range valid {
		t.Run("valid/"+raw, func(t *testing.T) {
			t.Parallel()
			if err := ValidateHost(raw); err != nil {
				t.Errorf("ValidateHost(%q) = %v, want nil", raw, err)
			}
		})
	}

	// An empty host is rejected rather than treated as "run locally": reaching
	// this function at all means something named a host, and `host: ""` in a
	// recipe is a mistake rather than a way to opt out.
	invalid := []string{"", "   ", "deploy@", "ssh://example.com"}
	for _, raw := range invalid {
		t.Run("invalid/"+raw, func(t *testing.T) {
			t.Parallel()
			if err := ValidateHost(raw); err == nil {
				t.Errorf("ValidateHost(%q) = nil, want an error", raw)
			}
		})
	}
}
