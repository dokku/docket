package tasks

import (
	"context"
	"errors"
	"fmt"

	"github.com/dokku/docket/subprocess"
)

// ToggleFields is the recipe shape every toggle task shares: the app whose
// plugin is being turned on or off, and whether it should be enabled (present)
// or disabled (absent). A toggle task declares `type XToggleTask ToggleFields`
// rather than restating the two fields, so a cross-cutting field change - the
// identity tags of #427, say - lands in one place instead of in every copy.
//
// A defined type rather than an embedded struct because the field set stays
// flat to reflection: the catalog, the required-field walk behind
// missing_required_field, the identity walk that names an unnamed task, and the
// sensitive-value walks all read a task's fields at the top level, and an
// embedded field set would empty every one of them out.
//
// The descriptions are deliberately plugin-agnostic. A task's page already
// names its plugin in the title and the synopsis, so repeating it in every
// parameter row bought nothing and cost a copy of the field set per plugin.
//
// There is no `global` field: planToggle always targets an app, the global
// machinery having been removed in #322. A toggle that later needs one declares
// its own fields and goes on the toggleTasksWithOwnFields allowlist with the
// reason. See TestToggleTasksDeclareTheSharedFields.
type ToggleFields struct {
	// App is the name of the app
	App string `required:"true" identity:"key" yaml:"app" description:"Name of the app"`

	// State is the desired state of the plugin for the app
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the plugin for the app"`
}

// ToggleContext represents the context for a toggle operation
type ToggleContext struct {
	// App is the name of the app
	App string
}

// ToggleProbe returns whether the toggle is currently in the "enabled"
// (state: present) position. nil from a probe (or a non-nil error) is
// treated as "drift, must mutate" so we still run the underlying command,
// except an SSH transport failure, which is surfaced as a plan error.
//
// The Go context comes first, as everywhere else; tc is the toggle's own
// addressing context.
type ToggleProbe func(ctx context.Context, tc ToggleContext) (enabled bool, err error)

// planToggle is the shared Plan() implementation for toggle tasks. The
// probe reports whether the underlying plugin is currently in the
// "enabled" position; when probe is nil or fails with a non-transport
// error, planToggle reports drift and the apply closure runs the
// underlying enable/disable command. An SSH transport failure short-
// circuits to a plan error so an unreachable host is not mistaken for
// drift.
func planToggle(ctx context.Context, state State, app string, enableCmd, disableCmd string, probe ToggleProbe) PlanResult {
	tc := ToggleContext{App: app}

	return DispatchPlan(state, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			if probe != nil {
				enabled, err := probe(ctx, tc)
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
				Commands:  resolveCommands(ctx, inputs),
				apply:     applyToggle(enableCmd, app, StatePresent),
			}
		},
		StateAbsent: func() PlanResult {
			if probe != nil {
				enabled, err := probe(ctx, tc)
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
				Commands:  resolveCommands(ctx, inputs),
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
func applyToggle(subcommand, target string, finalState State) func(ctx context.Context) TaskOutputState {
	inputs := toggleInputs(subcommand, target)
	return func(ctx context.Context) TaskOutputState {
		return runExecInputs(ctx, TaskOutputState{State: finalState}, finalState, inputs)
	}
}
