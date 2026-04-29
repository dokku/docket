package tasks

import (
	"fmt"
	"github.com/dokku/docket/subprocess"
	"strconv"
	"strings"
)

// PsScaleTask manages the process scale for a given dokku application
type PsScaleTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app"`

	// Scale is a map of process types to quantities
	Scale map[string]int `required:"true" yaml:"scale"`

	// SkipDeploy skips the corresponding deploy
	SkipDeploy bool `yaml:"skip_deploy" default:"false"`

	// State is the desired state of the process scale
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present"`
}

// PsScaleTaskExample contains an example of a PsScaleTask
type PsScaleTaskExample struct {
	// Name is the task name holding the PsScaleTask description
	Name string `yaml:"-"`

	// PsScaleTask is the PsScaleTask configuration
	PsScaleTask PsScaleTask `yaml:"dokku_ps_scale"`
}

// GetName returns the name of the example
func (e PsScaleTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the ps scale task
func (t PsScaleTask) Doc() string {
	return "Manages the process scale for a given dokku application"
}

// Examples returns the examples for the ps scale task
func (t PsScaleTask) Examples() ([]Doc, error) {
	return MarshalExamples([]PsScaleTaskExample{
		{
			Name: "Scale web and worker processes",
			PsScaleTask: PsScaleTask{
				App: "hello-world",
				Scale: map[string]int{
					"web":    2,
					"worker": 1,
				},
			},
		},
		{
			Name: "Scale web and worker processes without deploy",
			PsScaleTask: PsScaleTask{
				App:        "hello-world",
				SkipDeploy: true,
				Scale: map[string]int{
					"web":    4,
					"worker": 4,
				},
			},
		},
	})
}

// Execute sets the process scale for a given dokku application
func (t PsScaleTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the PsScaleTask would produce.
func (t PsScaleTask) Plan() PlanResult {
	if t.State == StatePresent && len(t.Scale) == 0 {
		return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("scale must be specified when state is present")}
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			existing, err := getPsScale(t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			toScale := []string{}
			mutations := []string{}
			for proctype, qty := range t.Scale {
				if cur, ok := existing[proctype]; ok && cur == qty {
					continue
				}
				toScale = append(toScale, fmt.Sprintf("%s=%d", proctype, qty))
				if cur, ok := existing[proctype]; ok {
					mutations = append(mutations, fmt.Sprintf("scale %s=%d (was %d)", proctype, qty, cur))
				} else {
					mutations = append(mutations, fmt.Sprintf("scale %s=%d (new)", proctype, qty))
				}
			}
			if len(toScale) == 0 {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			args := []string{"ps:scale"}
			if t.SkipDeploy {
				args = append(args, "--skip-deploy")
			}
			args = append(args, t.App)
			args = append(args, toScale...)
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusModify,
				Reason:    fmt.Sprintf("%d process scale change(s)", len(mutations)),
				Mutations: mutations,
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
	})
}

// getPsScale retrieves the current process scale for a given dokku application
func getPsScale(app string) (map[string]int, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "ps:scale", app},
	})
	if err != nil {
		return nil, err
	}

	scale := map[string]int{}
	for _, line := range strings.Split(result.StdoutContents(), "\n") {
		// strip all whitespace from the line, matching the upstream ansible module
		line = strings.Join(strings.Fields(line), "")
		if !strings.Contains(line, ":") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		qty, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		scale[parts[0]] = qty
	}
	return scale, nil
}

// init registers the PsScaleTask with the task registry
func init() {
	RegisterTask(&PsScaleTask{})
}
