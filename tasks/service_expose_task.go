package tasks

import (
	"errors"
	"fmt"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// ServiceExposeTask exposes or unexposes a dokku service on host ports
type ServiceExposeTask struct {
	// Service is the type of service to expose (e.g. redis, postgres, mysql)
	Service string `required:"true" yaml:"service" description:"Type of service to expose (e.g. redis, postgres, mysql)"`

	// Name is the name of the service instance
	Name string `required:"true" yaml:"name" description:"Name of the service instance"`

	// Ports are the host ports to expose the service on. Required when state is present.
	Ports []string `required:"false" yaml:"ports,omitempty" description:"Host ports to expose the service on. Required when state is present."`

	// State is the desired state of the service exposure
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the service exposure"`
}

// ServiceExposeTaskExample contains an example of a ServiceExposeTask
type ServiceExposeTaskExample struct {
	// Name is the task name holding the ServiceExposeTask description
	Name string `yaml:"-"`

	// ServiceExposeTask is the ServiceExposeTask configuration
	ServiceExposeTask ServiceExposeTask `yaml:"dokku_service_expose"`
}

// GetName returns the name of the example
func (e ServiceExposeTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the service expose task
func (t ServiceExposeTask) Doc() string {
	return "Exposes or unexposes a dokku service on host ports"
}

// ExportSupport reports how docket export handles this task.
func (t ServiceExposeTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportUnsupported, Caveat: serviceExportCaveat}
}

// Requirements lists the non-core dokku plugins this task depends on.
func (t ServiceExposeTask) Requirements() []string {
	return []string{"a dokku datastore service plugin matching the service type (e.g. dokku-postgres, dokku-redis, dokku-mysql)"}
}

// Examples returns a list of ServiceExposeTaskExamples as yaml
func (t ServiceExposeTask) Examples() ([]Doc, error) {
	return MarshalExamples([]ServiceExposeTaskExample{
		{
			Name: "Expose a postgres service named my-db on host port 5432",
			ServiceExposeTask: ServiceExposeTask{
				Service: "postgres",
				Name:    "my-db",
				Ports:   []string{"5432"},
			},
		},
		{
			Name: "Expose a redis service named my-redis on host port 6379",
			ServiceExposeTask: ServiceExposeTask{
				Service: "redis",
				Name:    "my-redis",
				Ports:   []string{"6379"},
			},
		},
		{
			Name: "Unexpose a postgres service named my-db",
			ServiceExposeTask: ServiceExposeTask{
				Service: "postgres",
				Name:    "my-db",
				State:   StateAbsent,
			},
		},
	})
}

// Execute exposes or unexposes a dokku service
func (t ServiceExposeTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Validate checks the ServiceExposeTask's inputs without contacting the server.
func (t ServiceExposeTask) Validate() error {
	if t.State == StatePresent && len(t.Ports) == 0 {
		return fmt.Errorf("'ports' is required when state is 'present'")
	}
	return nil
}

// Plan reports the drift the ServiceExposeTask would produce.
func (t ServiceExposeTask) Plan() PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			exists, err := serviceExists(t.Service, t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !exists {
				return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("service %s %s does not exist", t.Service, t.Name)}
			}
			current, err := serviceExposedPorts(t.Service, t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			desired := map[string]bool{}
			for _, p := range t.Ports {
				desired[p] = true
			}
			if portSetEqual(current, desired) {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{}
			mutations := []string{}
			status := PlanStatusCreate
			// dokku rejects expose when the service is already exposed, so a
			// change of ports must unexpose first, then re-expose the full set.
			if len(current) > 0 {
				status = PlanStatusModify
				inputs = append(inputs, subprocess.ExecCommandInput{
					Command: "dokku",
					Args:    []string{"--quiet", fmt.Sprintf("%s:unexpose", t.Service), t.Name},
				})
				mutations = append(mutations, fmt.Sprintf("%s:unexpose %s", t.Service, t.Name))
			}
			exposeArgs := []string{"--quiet", fmt.Sprintf("%s:expose", t.Service), t.Name}
			exposeArgs = append(exposeArgs, t.Ports...)
			inputs = append(inputs, subprocess.ExecCommandInput{Command: "dokku", Args: exposeArgs})
			mutations = append(mutations, fmt.Sprintf("%s:expose %s %s", t.Service, t.Name, strings.Join(t.Ports, " ")))
			return PlanResult{
				InSync:    false,
				Status:    status,
				Reason:    fmt.Sprintf("%s service %s not exposed on %s", t.Service, t.Name, strings.Join(t.Ports, " ")),
				Mutations: mutations,
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
				return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("service %s %s does not exist", t.Service, t.Name)}
			}
			current, err := serviceExposedPorts(t.Service, t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if len(current) == 0 {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", fmt.Sprintf("%s:unexpose", t.Service), t.Name},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    fmt.Sprintf("%s service %s exposed", t.Service, t.Name),
				Mutations: []string{fmt.Sprintf("%s:unexpose %s", t.Service, t.Name)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// serviceExposedPorts returns the set of host ports a dokku service is
// currently exposed on. A transport-level failure (`*subprocess.SSHError`)
// is propagated; a dokku-level non-zero exit (e.g. service not exposed) is
// treated as "no ports exposed."
func serviceExposedPorts(service, name string) (map[string]bool, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"--quiet",
			fmt.Sprintf("%s:info", service),
			name,
			"--exposed-ports",
		},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return map[string]bool{}, nil
	}

	ports := map[string]bool{}
	for _, p := range strings.Fields(result.StdoutContents()) {
		if p == "-" {
			continue
		}
		ports[p] = true
	}
	return ports, nil
}

// portSetEqual reports whether two port sets contain the same elements.
func portSetEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

// init registers the ServiceExposeTask with the task registry
func init() {
	RegisterTask(&ServiceExposeTask{})
}
