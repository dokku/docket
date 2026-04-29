package tasks

import (
	"fmt"
	"github.com/dokku/docket/subprocess"
)

// ServiceCreateTask creates or destroys a dokku service
type ServiceCreateTask struct {
	// Service is the type of service to create (e.g. redis, postgres, mysql)
	Service string `required:"true" yaml:"service"`

	// Name is the name of the service instance
	Name string `required:"true" yaml:"name"`

	// State is the desired state of the service
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// ServiceCreateTaskExample contains an example of a ServiceCreateTask
type ServiceCreateTaskExample struct {
	// Name is the task name holding the ServiceCreateTask description
	Name string `yaml:"-"`

	// ServiceCreateTask is the ServiceCreateTask configuration
	ServiceCreateTask ServiceCreateTask `yaml:"dokku_service_create"`
}

// GetName returns the name of the example
func (e ServiceCreateTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the service create task
func (t ServiceCreateTask) Doc() string {
	return "Creates or destroys a dokku service"
}

// Examples returns a list of ServiceCreateTaskExamples as yaml
func (t ServiceCreateTask) Examples() ([]Doc, error) {
	return MarshalExamples([]ServiceCreateTaskExample{
		{
			Name: "Create a redis service named my-redis",
			ServiceCreateTask: ServiceCreateTask{
				Service: "redis",
				Name:    "my-redis",
			},
		},
		{
			Name: "Create a postgres service named my-db",
			ServiceCreateTask: ServiceCreateTask{
				Service: "postgres",
				Name:    "my-db",
			},
		},
		{
			Name: "Destroy a redis service named my-redis",
			ServiceCreateTask: ServiceCreateTask{
				Service: "redis",
				Name:    "my-redis",
				State:   "absent",
			},
		},
	})
}

// Execute creates or destroys a dokku service
func (t ServiceCreateTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the ServiceCreateTask would produce.
func (t ServiceCreateTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			exists, err := serviceExists(t.Service, t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if exists {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", fmt.Sprintf("%s:create", t.Service), t.Name},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    fmt.Sprintf("%s service %s missing", t.Service, t.Name),
				Mutations: []string{fmt.Sprintf("%s:create %s", t.Service, t.Name)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			exists, err := serviceExists(t.Service, t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !exists {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "--force", fmt.Sprintf("%s:destroy", t.Service), t.Name},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    fmt.Sprintf("%s service %s present", t.Service, t.Name),
				Mutations: []string{fmt.Sprintf("%s:destroy %s", t.Service, t.Name)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// serviceExists checks if a dokku service exists. Returns (false,
// *subprocess.SSHError) on transport failure, (false, nil) when dokku
// reports the service absent, (true, nil) when present.
func serviceExists(service, name string) (bool, error) {
	return subprocess.Probe(subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"--quiet",
			fmt.Sprintf("%s:exists", service),
			name,
		},
	})
}

// createService creates a dokku service
func createService(service, name string) TaskOutputState {
	state := TaskOutputState{
		Changed: false,
		State:   "absent",
	}
	exists, _ := serviceExists(service, name)
	if exists {
		state.State = "present"
		return state
	}

	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"--quiet",
			fmt.Sprintf("%s:create", service),
			name,
		},
	})
	state.Commands = append(state.Commands, result.Command)
	if err != nil {
		return TaskOutputErrorFromExec(state, err, result)
	}

	state.Changed = true
	state.State = "present"
	return state
}

// destroyService destroys a dokku service
func destroyService(service, name string) TaskOutputState {
	state := TaskOutputState{
		Changed: false,
		State:   "present",
	}
	exists, _ := serviceExists(service, name)
	if !exists {
		state.State = "absent"
		return state
	}

	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"--quiet",
			"--force",
			fmt.Sprintf("%s:destroy", service),
			name,
		},
	})
	state.Commands = append(state.Commands, result.Command)
	if err != nil {
		return TaskOutputErrorFromExec(state, err, result)
	}

	state.Changed = true
	state.State = "absent"
	return state
}

// init registers the ServiceCreateTask with the task registry
func init() {
	RegisterTask(&ServiceCreateTask{})
}
