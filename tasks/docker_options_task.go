package tasks

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// DockerOptionsTask manages docker-options for a given dokku application
type DockerOptionsTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app"`

	// Phase is the deployment phase the option applies to
	Phase string `required:"true" yaml:"phase" options:"build,deploy,run"`

	// Option is the docker option string (e.g. "-v /var/run/docker.sock:/var/run/docker.sock")
	Option string `required:"true" yaml:"option"`

	// State is the desired state of the docker option
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// DockerOptionsTaskExample contains an example of a DockerOptionsTask
type DockerOptionsTaskExample struct {
	// Name is the task name holding the DockerOptionsTask description
	Name string `yaml:"-"`

	// DockerOptionsTask is the DockerOptionsTask configuration
	DockerOptionsTask DockerOptionsTask `yaml:"dokku_docker_options"`
}

// GetName returns the name of the example
func (e DockerOptionsTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the docker options task
func (t DockerOptionsTask) Doc() string {
	return "Manages docker-options for a given dokku application"
}

// Examples returns the examples for the docker options task
func (t DockerOptionsTask) Examples() ([]Doc, error) {
	return MarshalExamples([]DockerOptionsTaskExample{
		{
			Name: "Mount the docker socket at deploy",
			DockerOptionsTask: DockerOptionsTask{
				App:    "node-js-app",
				Phase:  "deploy",
				Option: "-v /var/run/docker.sock:/var/run/docker.sock",
			},
		},
		{
			Name: "Remove a docker option from the deploy phase",
			DockerOptionsTask: DockerOptionsTask{
				App:    "node-js-app",
				Phase:  "deploy",
				Option: "-v /var/run/docker.sock:/var/run/docker.sock",
				State:  StateAbsent,
			},
		},
	})
}

// Execute manages the docker option
func (t DockerOptionsTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the DockerOptionsTask would produce.
func (t DockerOptionsTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			if err := validateDockerOptionsTask(t); err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			current, err := getDockerOptions(t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if optionPresent(current[t.Phase], t.Option) {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "docker-options:add", t.App, t.Phase, t.Option},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    fmt.Sprintf("missing on %s phase", t.Phase),
				Mutations: []string{fmt.Sprintf("add %s option %q", t.Phase, t.Option)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			if err := validateDockerOptionsTask(t); err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			current, err := getDockerOptions(t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !optionPresent(current[t.Phase], t.Option) {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "docker-options:remove", t.App, t.Phase, t.Option},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    fmt.Sprintf("present on %s phase", t.Phase),
				Mutations: []string{fmt.Sprintf("remove %s option %q", t.Phase, t.Option)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

var dockerOptionPhases = map[string]bool{"build": true, "deploy": true, "run": true}

// validateDockerOptionsTask validates the docker options task parameters
func validateDockerOptionsTask(t DockerOptionsTask) error {
	if t.App == "" {
		return fmt.Errorf("'app' is required")
	}
	if !dockerOptionPhases[t.Phase] {
		return fmt.Errorf("'phase' must be one of build, deploy, run")
	}
	if strings.TrimSpace(t.Option) == "" {
		return fmt.Errorf("'option' is required")
	}
	return nil
}

var dockerOptionsReportRe = regexp.MustCompile(`^Docker options (build|deploy|run):\s*(.*)$`)

// getDockerOptions returns the current docker options for each phase of an app
func getDockerOptions(app string) (map[string]string, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "docker-options:report", app},
	})
	if err != nil {
		return nil, err
	}

	options := map[string]string{"build": "", "deploy": "", "run": ""}
	for _, line := range strings.Split(result.StdoutContents(), "\n") {
		match := dockerOptionsReportRe.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		options[match[1]] = strings.TrimSpace(match[2])
	}
	return options, nil
}

// optionPresent returns true if option appears as a contiguous token sequence in existing
func optionPresent(existing, option string) bool {
	optionTokens := strings.Fields(option)
	if len(optionTokens) == 0 {
		return false
	}
	existingTokens := strings.Fields(existing)
	for i := 0; i+len(optionTokens) <= len(existingTokens); i++ {
		match := true
		for j, tok := range optionTokens {
			if existingTokens[i+j] != tok {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// init registers the DockerOptionsTask with the task registry
func init() {
	RegisterTask(&DockerOptionsTask{})
}
