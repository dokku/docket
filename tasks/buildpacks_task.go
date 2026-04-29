package tasks

import (
	"fmt"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// BuildpacksTask manages the buildpacks for a given dokku application
type BuildpacksTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app"`

	// Buildpacks is the list of buildpack URLs
	Buildpacks []string `required:"false" yaml:"buildpacks"`

	// State is the desired state of the buildpacks
	State State `required:"false" yaml:"state" default:"present" options:"present,absent"`
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

// Plan reports the drift the BuildpacksTask would produce.
func (t BuildpacksTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planBuildpacksAdd(t) },
		StateAbsent:  func() PlanResult { return planBuildpacksRemove(t) },
	})
}

func planBuildpacksAdd(t BuildpacksTask) PlanResult {
	if t.App == "" {
		return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("'app' is required")}
	}
	if len(t.Buildpacks) == 0 {
		return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("'buildpacks' must not be empty for state 'present'")}
	}
	current, err := getBuildpacks(t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	toAdd := []string{}
	mutations := []string{}
	for _, bp := range t.Buildpacks {
		if !current[bp] {
			toAdd = append(toAdd, bp)
			mutations = append(mutations, "add "+bp)
		}
	}
	if len(toAdd) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	status := PlanStatusModify
	if len(current) == 0 {
		status = PlanStatusCreate
	}
	inputs := make([]subprocess.ExecCommandInput, 0, len(toAdd))
	for _, bp := range toAdd {
		inputs = append(inputs, subprocess.ExecCommandInput{
			Command: "dokku",
			Args:    []string{"--quiet", "buildpacks:add", t.App, bp},
		})
	}
	return PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%d buildpack(s) to add", len(toAdd)),
		Mutations: mutations,
		Commands:  resolveCommands(inputs),
		apply: func() TaskOutputState {
			return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
		},
	}
}

func planBuildpacksRemove(t BuildpacksTask) PlanResult {
	if t.App == "" {
		return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("'app' is required")}
	}
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
