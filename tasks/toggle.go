package tasks

import (
	"errors"
	"fmt"

	"github.com/dokku/docket/subprocess"
)

// ToggleContext represents the context for a toggle operation
type ToggleContext struct {
	// App is the name of the app
	App string
}

// ToggleProbe returns whether the toggle is currently in the "enabled"
// (state: present) position. nil from a probe (or a non-nil error) is
// treated as "drift, must mutate" so we still run the underlying command,
// except an SSH transport failure, which is surfaced as a plan error.
type ToggleProbe func(ctx ToggleContext) (enabled bool, err error)

// planToggle is the shared Plan() implementation for toggle tasks. The
// probe reports whether the underlying plugin is currently in the
// "enabled" position; when probe is nil or fails with a non-transport
// error, planToggle reports drift and the apply closure runs the
// underlying enable/disable command. An SSH transport failure short-
// circuits to a plan error so an unreachable host is not mistaken for
// drift.
func planToggle(state State, app string, enableCmd, disableCmd string, probe ToggleProbe) PlanResult {
	ctx := ToggleContext{App: app}

	return DispatchPlan(state, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			if probe != nil {
				enabled, err := probe(ctx)
				if err != nil {
					var sshErr *subprocess.SSHError
					if errors.As(err, &sshErr) {
						return PlanResult{Status: PlanStatusError, Error: err}
					}
					// non-SSH probe error: treat as drift, must mutate
				} else if enabled {
					return PlanResult{InSync: true, Status: PlanStatusOK}
				}
			}
			inputs := toggleInputs(enableCmd, app)
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusModify,
				Reason:    fmt.Sprintf("would run %s on %s", enableCmd, app),
				Mutations: []string{fmt.Sprintf("%s %s", enableCmd, app)},
				Commands:  resolveCommands(inputs),
				apply:     applyToggle(enableCmd, app, StatePresent),
			}
		},
		StateAbsent: func() PlanResult {
			if probe != nil {
				enabled, err := probe(ctx)
				if err != nil {
					var sshErr *subprocess.SSHError
					if errors.As(err, &sshErr) {
						return PlanResult{Status: PlanStatusError, Error: err}
					}
					// non-SSH probe error: treat as drift, must mutate
				} else if !enabled {
					return PlanResult{InSync: true, Status: PlanStatusOK}
				}
			}
			inputs := toggleInputs(disableCmd, app)
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusModify,
				Reason:    fmt.Sprintf("would run %s on %s", disableCmd, app),
				Mutations: []string{fmt.Sprintf("%s %s", disableCmd, app)},
				Commands:  resolveCommands(inputs),
				apply:     applyToggle(disableCmd, app, StateAbsent),
			}
		},
	})
}

// toggleInputs returns the subprocess inputs that run a toggle command.
func toggleInputs(subcommand, target string) []subprocess.ExecCommandInput {
	return []subprocess.ExecCommandInput{
		{Command: "dokku", Args: []string{"--quiet", subcommand, target}},
	}
}

// applyToggle returns a closure that runs `dokku <subcommand> <target>` and
// reports the resulting state. The original initial state matches finalState
// (preserved from the pre-refactor behavior), so on error the reported State
// remains finalState.
func applyToggle(subcommand, target string, finalState State) func() TaskOutputState {
	inputs := toggleInputs(subcommand, target)
	return func() TaskOutputState {
		return runExecInputs(TaskOutputState{State: finalState}, finalState, inputs)
	}
}
