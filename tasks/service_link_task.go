package tasks

import (
	"fmt"
	"github.com/dokku/docket/subprocess"
)

// ServiceLinkTask links or unlinks a dokku service to an app
type ServiceLinkTask struct {
	// App is the name of the app to link the service to
	App string `required:"true" yaml:"app"`

	// Service is the type of service to link (e.g. redis, postgres, mysql)
	Service string `required:"true" yaml:"service"`

	// Name is the name of the service instance
	Name string `required:"true" yaml:"name"`

	// State is the desired state of the service link
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// ServiceLinkTaskExample contains an example of a ServiceLinkTask
type ServiceLinkTaskExample struct {
	// Name is the task name holding the ServiceLinkTask description
	Name string `yaml:"-"`

	// ServiceLinkTask is the ServiceLinkTask configuration
	ServiceLinkTask ServiceLinkTask `yaml:"dokku_service_link"`
}

// GetName returns the name of the example
func (e ServiceLinkTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the service link task
func (t ServiceLinkTask) Doc() string {
	return "Links or unlinks a dokku service to an app"
}

// Examples returns a list of ServiceLinkTaskExamples as yaml
func (t ServiceLinkTask) Examples() ([]Doc, error) {
	return MarshalExamples([]ServiceLinkTaskExample{
		{
			Name: "Link a redis service named my-redis to my-app",
			ServiceLinkTask: ServiceLinkTask{
				App:     "my-app",
				Service: "redis",
				Name:    "my-redis",
			},
		},
		{
			Name: "Link a postgres service named my-db to my-app",
			ServiceLinkTask: ServiceLinkTask{
				App:     "my-app",
				Service: "postgres",
				Name:    "my-db",
			},
		},
		{
			Name: "Unlink a redis service named my-redis from my-app",
			ServiceLinkTask: ServiceLinkTask{
				App:     "my-app",
				Service: "redis",
				Name:    "my-redis",
				State:   "absent",
			},
		},
	})
}

// Execute links or unlinks a dokku service to an app
func (t ServiceLinkTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the ServiceLinkTask would produce.
func (t ServiceLinkTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			svcExists, err := serviceExists(t.Service, t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !svcExists {
				return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("service %s %s does not exist", t.Service, t.Name)}
			}
			appOK, err := appExists(t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !appOK {
				return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("app %s does not exist", t.App)}
			}
			linked, err := serviceLinked(t.Service, t.Name, t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if linked {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", fmt.Sprintf("%s:link", t.Service), t.Name, t.App},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    fmt.Sprintf("%s service %s not linked to %s", t.Service, t.Name, t.App),
				Mutations: []string{fmt.Sprintf("%s:link %s %s", t.Service, t.Name, t.App)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			svcExists, err := serviceExists(t.Service, t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !svcExists {
				return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("service %s %s does not exist", t.Service, t.Name)}
			}
			appOK, err := appExists(t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !appOK {
				return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("app %s does not exist", t.App)}
			}
			linked, err := serviceLinked(t.Service, t.Name, t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !linked {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", fmt.Sprintf("%s:unlink", t.Service), t.Name, t.App},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    fmt.Sprintf("%s service %s linked to %s", t.Service, t.Name, t.App),
				Mutations: []string{fmt.Sprintf("%s:unlink %s %s", t.Service, t.Name, t.App)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// serviceLinked checks if a dokku service is linked to an app. Returns
// (false, *subprocess.SSHError) on transport failure; (false, nil) when
// dokku reports no link; (true, nil) when linked.
func serviceLinked(service, name, app string) (bool, error) {
	return subprocess.Probe(subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"--quiet",
			fmt.Sprintf("%s:linked", service),
			name,
			app,
		},
	})
}

// linkService links a dokku service to an app
func linkService(service, name, app string) TaskOutputState {
	state := TaskOutputState{
		Changed: false,
		State:   "absent",
	}

	svcExists, _ := serviceExists(service, name)
	if !svcExists {
		state.Error = fmt.Errorf("service %s %s does not exist", service, name)
		return state
	}

	appOK, _ := appExists(app)
	if !appOK {
		state.Error = fmt.Errorf("app %s does not exist", app)
		return state
	}

	linked, _ := serviceLinked(service, name, app)
	if linked {
		state.State = "present"
		return state
	}

	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"--quiet",
			fmt.Sprintf("%s:link", service),
			name,
			app,
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

// unlinkService unlinks a dokku service from an app
func unlinkService(service, name, app string) TaskOutputState {
	state := TaskOutputState{
		Changed: false,
		State:   "present",
	}

	svcExists, _ := serviceExists(service, name)
	if !svcExists {
		state.Error = fmt.Errorf("service %s %s does not exist", service, name)
		return state
	}

	appOK, _ := appExists(app)
	if !appOK {
		state.Error = fmt.Errorf("app %s does not exist", app)
		return state
	}

	linked, _ := serviceLinked(service, name, app)
	if !linked {
		state.State = "absent"
		return state
	}

	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"--quiet",
			fmt.Sprintf("%s:unlink", service),
			name,
			app,
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

// init registers the ServiceLinkTask with the task registry
func init() {
	RegisterTask(&ServiceLinkTask{})
}
