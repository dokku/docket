package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dokku/docket/internal/subprocess"
)

// BuildpacksTask manages the buildpacks for a given dokku application
type BuildpacksTask struct {
	// App is the name of the app
	App string `required:"true" identity:"key" serialize:"app" yaml:"app" description:"Name of the app"`

	// Buildpacks is the list of buildpack URLs
	Buildpacks []string `required:"false" required_when:"present" forbidden_when:"clear" identity:"collection" ordered:"true" yaml:"buildpacks,omitempty" description:"List of buildpack URLs"`

	// State is the desired state of the buildpacks. present and set both
	// replace the whole ordered list, since buildpack order is build
	// precedence; set also accepts an empty list, which clears it.
	State State `required:"false" yaml:"state" default:"present" options:"present,absent,set,clear" description:"Desired state of the buildpacks"`
}

// BuildpacksTaskExample contains an example of a BuildpacksTask
type BuildpacksTaskExample struct {
	// Name is the task name holding the BuildpacksTask description
	Name string `yaml:"-"`

	// BuildpacksTask is the BuildpacksTask configuration
	BuildpacksTask BuildpacksTask `yaml:"dokku_buildpacks"`
}

// GetName returns the name of the example
func (e BuildpacksTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the buildpacks task
func (t BuildpacksTask) Doc() string {
	return "Manages the buildpacks for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t BuildpacksTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t BuildpacksTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// MinimumVersions reports the oldest Dokku this task's probe parses.
// The floor is 0.38.31, the release the integration suite runs; the probe is `buildpacks:list`.
func (t BuildpacksTask) MinimumVersions() VersionSupport {
	return VersionSupport{Dokku: "0.38.31"}
}

// examples returns the examples for the buildpacks task
func (t BuildpacksTask) examples() ([]Doc, error) {
	return MarshalExamples([]BuildpacksTaskExample{
		{
			Name: "Add buildpacks to an app",
			BuildpacksTask: BuildpacksTask{
				App: "node-js-app",
				Buildpacks: []string{
					"https://github.com/heroku/heroku-buildpack-nodejs.git",
					"https://github.com/heroku/heroku-buildpack-nginx.git",
				},
			},
		},
		{
			Name: "Remove a buildpack from an app",
			BuildpacksTask: BuildpacksTask{
				App: "node-js-app",
				Buildpacks: []string{
					"https://github.com/heroku/heroku-buildpack-nginx.git",
				},
				State: StateAbsent,
			},
		},
		{
			Name: "Replace the buildpacks for an app with exactly this list",
			BuildpacksTask: BuildpacksTask{
				App: "node-js-app",
				Buildpacks: []string{
					"https://github.com/heroku/heroku-buildpack-nodejs.git",
				},
				State: StateSet,
			},
		},
		{
			Name: "Clear all buildpacks from an app",
			BuildpacksTask: BuildpacksTask{
				App:   "node-js-app",
				State: StateClear,
			},
		},
	})
}

// Execute manages the buildpacks for a given app
func (t BuildpacksTask) Execute(ctx context.Context) TaskOutputState {
	return executeTask(ctx, t)
}

// Validate checks the BuildpacksTask's inputs without contacting the server.
// buildpacks:clear takes no buildpacks, which is why a list is forbidden
// alongside it: it would be silently discarded rather than removed.
func (t BuildpacksTask) Validate() error {
	if err := checkConstraints(t); err != nil {
		return err
	}
	if t.App == "" {
		return fmt.Errorf("'app' is required")
	}
	return nil
}

// Plan reports the drift the BuildpacksTask would produce.
func (t BuildpacksTask) Plan(ctx context.Context) PlanResult {
	if r, ok := gatePlan(ctx, t); !ok {
		return r
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planBuildpacksAdd(ctx, t, StatePresent) },
		StateAbsent:  func() PlanResult { return planBuildpacksRemove(ctx, t, StateAbsent) },
		StateSet: func() PlanResult {
			if len(t.Buildpacks) == 0 {
				return planSetAsClear(planBuildpacksRemove(ctx, t, StateClear))
			}
			return planBuildpacksAdd(ctx, t, StateSet)
		},
		StateClear: func() PlanResult { return planBuildpacksRemove(ctx, t, StateClear) },
	})
}

// observedBuildpacks attaches the buildpacks the probe read to r, in the
// server's precedence order: every one of them when the plan owns the whole
// list, or only those t names when an absent plan removes named buildpacks.
// state is the state the plan was made for: present and absent report what
// they read as present or absent, while set and clear report it as set, or as
// clear when there are none.
func observedBuildpacks(r PlanResult, t BuildpacksTask, state State, current []string, whole bool) PlanResult {
	var held []string
	for _, bp := range current {
		if whole || slices.Contains(t.Buildpacks, bp) {
			held = append(held, bp)
		}
	}
	authoritative := state == StateSet || state == StateClear
	observedState := StateAbsent
	switch {
	case authoritative && len(held) > 0:
		observedState = StateSet
	case authoritative:
		observedState = StateClear
	case len(held) > 0:
		observedState = StatePresent
	}
	return observed(r, BuildpacksTask{App: t.App, Buildpacks: held, State: observedState}, len(held) > 0)
}

// planBuildpacksAdd reports drift for replacing the app's buildpacks with the
// declared ordered list, for state present or set. finalState is the state
// the apply reports.
func planBuildpacksAdd(ctx context.Context, t BuildpacksTask, finalState State) PlanResult {
	current, err := getOrderedBuildpacks(ctx, t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	// Buildpack order determines build precedence, so the current and desired
	// lists must match as ordered slices: a reorder or a partial set is real
	// drift, not in sync (issue #356). Validate() already guarantees the desired
	// list is non-empty for state present.
	if slices.Equal(current, t.Buildpacks) {
		return observedBuildpacks(PlanResult{InSync: true, Status: PlanStatusOK}, t, finalState, current, true)
	}
	status := PlanStatusModify
	if len(current) == 0 {
		status = PlanStatusCreate
	}
	// dokku has no atomic ordered-set for the list; buildpacks:set --replace
	// replaces the whole list in one call, applied in the given precedence order.
	args := append([]string{"--quiet", "buildpacks:set", "--replace", t.App}, t.Buildpacks...)
	inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
	mutations := make([]string, 0, len(t.Buildpacks))
	for i, bp := range t.Buildpacks {
		mutations = append(mutations, fmt.Sprintf("set %s (position %d)", bp, i))
	}
	return observedBuildpacks(PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("set %d buildpack(s) in order", len(t.Buildpacks)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, finalState, inputs)
		},
	}, t, finalState, current, true)
}

// planBuildpacksRemove reports drift for removing the named buildpacks, or
// every buildpack when none are named, which is all state clear ever does.
// finalState is the state the apply reports.
func planBuildpacksRemove(ctx context.Context, t BuildpacksTask, finalState State) PlanResult {
	ordered, err := listBuildpacks(ctx, t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	current := make(map[string]bool, len(ordered))
	for _, bp := range ordered {
		current[bp] = true
	}
	// With no buildpacks named the plan clears the whole list, so it reads all
	// of them; otherwise it reads only the ones it would remove.
	whole := len(t.Buildpacks) == 0
	if whole {
		if len(current) == 0 {
			return observedBuildpacks(PlanResult{InSync: true, Status: PlanStatusOK}, t, finalState, ordered, whole)
		}
		// Removals are listed in precedence order, so every plan of the same
		// list reads the same.
		mutations := make([]string, 0, len(current))
		listed := make(map[string]bool, len(current))
		for _, bp := range ordered {
			if !listed[bp] {
				listed[bp] = true
				mutations = append(mutations, "remove "+bp)
			}
		}
		inputs := []subprocess.ExecCommandInput{{
			Command: "dokku",
			Args:    []string{"--quiet", "buildpacks:clear", t.App},
		}}
		return observedBuildpacks(PlanResult{
			InSync:    false,
			Status:    PlanStatusDestroy,
			Reason:    fmt.Sprintf("clear %d buildpack(s)", len(current)),
			Mutations: mutations,
			Commands:  resolveCommands(ctx, inputs),
			apply: func(ctx context.Context) TaskOutputState {
				return runExecInputs(ctx, TaskOutputState{State: StatePresent}, finalState, inputs)
			},
		}, t, finalState, ordered, whole)
	}
	toRemove := []string{}
	mutations := []string{}
	for _, bp := range t.Buildpacks {
		if current[bp] {
			toRemove = append(toRemove, bp)
			mutations = append(mutations, "remove "+bp)
		}
	}
	if len(toRemove) == 0 {
		return observedBuildpacks(PlanResult{InSync: true, Status: PlanStatusOK}, t, finalState, ordered, whole)
	}
	inputs := make([]subprocess.ExecCommandInput, 0, len(toRemove))
	for _, bp := range toRemove {
		inputs = append(inputs, subprocess.ExecCommandInput{
			Command: "dokku",
			Args:    []string{"--quiet", "buildpacks:remove", t.App, bp},
		})
	}
	return observedBuildpacks(PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("%d buildpack(s) to remove", len(toRemove)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StatePresent}, finalState, inputs)
		},
	}, t, finalState, ordered, whole)
}

// ExportApp reads the app's buildpacks and returns a dokku_buildpacks task, or
// nil when none are set.
func (t BuildpacksTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	list, err := getOrderedBuildpacks(ctx, app)
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return nil, nil
	}
	if len(list) == 0 {
		return nil, nil
	}
	return []interface{}{BuildpacksTask{App: app, Buildpacks: list, State: StatePresent}}, nil
}

// getOrderedBuildpacks fetches the app's buildpacks in build-precedence order
// from the `list` key of buildpacks:report. Unlike getBuildpacks (an unordered
// set) this preserves order, which the plan probe and export both require.
func getOrderedBuildpacks(ctx context.Context, app string) ([]string, error) {
	response, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "buildpacks:report", app, "--format", "json"},
	})
	if err != nil {
		return nil, err
	}

	var report struct {
		List string `json:"list"`
	}
	if err := json.Unmarshal(response.StdoutBytes(), &report); err != nil {
		return nil, err
	}
	// The list key is a comma-separated string in build-precedence order.
	var list []string
	for _, bp := range strings.Split(report.List, ",") {
		if bp = strings.TrimSpace(bp); bp != "" {
			list = append(list, bp)
		}
	}
	return list, nil
}

// getBuildpacks fetches the current buildpacks list for an app as a set.
func getBuildpacks(ctx context.Context, app string) (map[string]bool, error) {
	list, err := listBuildpacks(ctx, app)
	if err != nil {
		return nil, err
	}
	buildpacks := make(map[string]bool, len(list))
	for _, bp := range list {
		buildpacks[bp] = true
	}
	return buildpacks, nil
}

// listBuildpacks fetches the app's buildpacks from `buildpacks:list`, in the
// precedence order it prints them.
func listBuildpacks(ctx context.Context, app string) ([]string, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "buildpacks:list", app},
	})
	if err != nil {
		return nil, err
	}

	var buildpacks []string
	for _, line := range strings.Split(result.StdoutContents(), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "=====>") {
			continue
		}
		buildpacks = append(buildpacks, trimmed)
	}
	return buildpacks, nil
}

// init registers the BuildpacksTask with the task registry
func init() {
	RegisterTask(&BuildpacksTask{})
}
