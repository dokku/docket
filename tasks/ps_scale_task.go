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
	App string `required:"true" yaml:"app" description:"Name of the app"`

	// Scale is a map of process types to quantities
	Scale map[string]int `required:"true" yaml:"scale" description:"Map of process types to quantities"`

	// SkipDeploy skips the corresponding deploy. It is a *bool so the value
	// survives decoding unchanged; nil defaults to false.
	SkipDeploy *bool `yaml:"skip_deploy,omitempty" default:"false" description:"Skip the corresponding deploy"`

	// State is the desired state of the process scale
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present" description:"Desired state of the process scale"`
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

// ExportSupport reports how docket export handles this task.
func (t PsScaleTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
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
				SkipDeploy: boolPtr(true),
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

// Validate checks the PsScaleTask's inputs without contacting the server.
func (t PsScaleTask) Validate() error {
	if t.State == StatePresent && len(t.Scale) == 0 {
		return fmt.Errorf("scale must be specified when state is present")
	}
	return nil
}

// Plan reports the drift the PsScaleTask would produce.
func (t PsScaleTask) Plan() PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
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
			if boolValue(t.SkipDeploy, false) {
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

// ExportApp reads the app's process scale and returns a dokku_ps_scale task
// when any process is scaled above zero (an undeployed app has nothing to set).
func (t PsScaleTask) ExportApp(app string) ([]interface{}, error) {
	scale, err := getPsScale(app)
	if err != nil {
		return nil, err
	}
	any := false
	for _, qty := range scale {
		if qty > 0 {
			any = true
			break
		}
	}
	if !any {
		return nil, nil
	}
	return []interface{}{PsScaleTask{App: app, Scale: scale}}, nil
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
