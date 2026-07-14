package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// BuildpacksTask manages the buildpacks for a given dokku application
type BuildpacksTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app" description:"Name of the app"`

	// Buildpacks is the list of buildpack URLs
	Buildpacks []string `required:"false" yaml:"buildpacks" description:"List of buildpack URLs"`

	// State is the desired state of the buildpacks
	State State `required:"false" yaml:"state" default:"present" options:"present,absent" description:"Desired state of the buildpacks"`
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

// Examples returns the examples for the buildpacks task
func (t BuildpacksTask) Examples() ([]Doc, error) {
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
			Name: "Clear all buildpacks from an app",
			BuildpacksTask: BuildpacksTask{
				App:   "node-js-app",
				State: StateAbsent,
			},
		},
	})
}

// Execute manages the buildpacks for a given app
func (t BuildpacksTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Validate checks the BuildpacksTask's inputs without contacting the server.
func (t BuildpacksTask) Validate() error {
	if t.App == "" {
		return fmt.Errorf("'app' is required")
	}
	if t.State == StatePresent && len(t.Buildpacks) == 0 {
		return fmt.Errorf("'buildpacks' must not be empty for state 'present'")
	}
	return nil
}

// Plan reports the drift the BuildpacksTask would produce.
func (t BuildpacksTask) Plan() PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planBuildpacksAdd(t) },
		StateAbsent:  func() PlanResult { return planBuildpacksRemove(t) },
	})
}

func planBuildpacksAdd(t BuildpacksTask) PlanResult {
	current, err := getOrderedBuildpacks(t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	// Buildpack order determines build precedence, so the current and desired
	// lists must match as ordered slices: a reorder or a partial set is real
	// drift, not in sync (issue #356). Validate() already guarantees the desired
	// list is non-empty for state present.
	if slices.Equal(current, t.Buildpacks) {
		return PlanResult{InSync: true, Status: PlanStatusOK}
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
	return PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("set %d buildpack(s) in order", len(t.Buildpacks)),
		Mutations: mutations,
		Commands:  resolveCommands(inputs),
		apply: func() TaskOutputState {
			return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
		},
	}
}

func planBuildpacksRemove(t BuildpacksTask) PlanResult {
	current, err := getBuildpacks(t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	if len(t.Buildpacks) == 0 {
		if len(current) == 0 {
			return PlanResult{InSync: true, Status: PlanStatusOK}
		}
		mutations := make([]string, 0, len(current))
		for bp := range current {
			mutations = append(mutations, "remove "+bp)
		}
		inputs := []subprocess.ExecCommandInput{{
			Command: "dokku",
			Args:    []string{"--quiet", "buildpacks:clear", t.App},
		}}
		return PlanResult{
			InSync:    false,
			Status:    PlanStatusDestroy,
			Reason:    fmt.Sprintf("clear %d buildpack(s)", len(current)),
			Mutations: mutations,
			Commands:  resolveCommands(inputs),
			apply: func() TaskOutputState {
				return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
			},
		}
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
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	inputs := make([]subprocess.ExecCommandInput, 0, len(toRemove))
	for _, bp := range toRemove {
		inputs = append(inputs, subprocess.ExecCommandInput{
			Command: "dokku",
			Args:    []string{"--quiet", "buildpacks:remove", t.App, bp},
		})
	}
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("%d buildpack(s) to remove", len(toRemove)),
		Mutations: mutations,
		Commands:  resolveCommands(inputs),
		apply: func() TaskOutputState {
			return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
		},
	}
}

// ExportApp reads the app's buildpacks and returns a dokku_buildpacks task, or
// nil when none are set.
func (t BuildpacksTask) ExportApp(app string) ([]interface{}, error) {
	list, err := getOrderedBuildpacks(app)
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
func getOrderedBuildpacks(app string) ([]string, error) {
	response, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
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

// getBuildpacks fetches the current buildpacks list for an app
func getBuildpacks(app string) (map[string]bool, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "buildpacks:list", app},
	})
	if err != nil {
		return nil, err
	}

	buildpacks := map[string]bool{}
	for _, line := range strings.Split(result.StdoutContents(), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "=====>") {
			continue
		}
		buildpacks[trimmed] = true
	}
	return buildpacks, nil
}

// init registers the BuildpacksTask with the task registry
func init() {
	RegisterTask(&BuildpacksTask{})
}
