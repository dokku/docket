package tasks

import (
	"errors"
	"fmt"
	"github.com/dokku/docket/subprocess"
	"strconv"
	"strings"
)

// PortsTask manages the ports for a given dokku application
type PortsTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app"`

	// PortMappings are the port mappings to set
	PortMappings []PortMapping `required:"true" yaml:"port_mappings"`

	// State is the desired state of the ports
	State State `required:"false" yaml:"state" default:"present" options:"present,absent"`
}

// PortsTaskExample contains an example of a PortsTask
type PortsTaskExample struct {
	// Name is the task name holding the PortsTask description
	Name string `yaml:"-"`

	// PortsTask is the PortsTask configuration
	PortsTask PortsTask `yaml:"dokku_ports"`
}

// GetName returns the name of the example
func (e PortsTaskExample) GetName() string {
	return e.Name
}

// PortMapping represents a port mapping
type PortMapping struct {
	// Scheme is the scheme of the port mapping
	Scheme string `required:"true" yaml:"scheme"`

	// Host is the host of the port mapping
	Host int `required:"true" yaml:"host"`

	// Container is the container of the port mapping
	Container int `required:"true" yaml:"container"`
}

// String returns the string representation of the port mapping
func (p PortMapping) String() string {
	return fmt.Sprintf("%s:%d:%d", p.Scheme, p.Host, p.Container)
}

// Doc returns the docblock for the ports task
func (t PortsTask) Doc() string {
	return "Manages the ports for a given dokku application"
}

// Examples returns the examples for the ports task
func (t PortsTask) Examples() ([]Doc, error) {
	return MarshalExamples([]PortsTaskExample{})
}

// Execute sets or unsets the ports
func (t PortsTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the PortsTask would produce.
func (t PortsTask) Plan() PlanResult {
	if len(t.PortMappings) == 0 {
		return PlanResult{Status: PlanStatusError, Error: errors.New("no port mappings provided")}
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			currentPorts, err := getPorts(t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			toAdd := []PortMapping{}
			mutations := []string{}
			for _, pm := range t.PortMappings {
				if _, ok := currentPorts[pm.String()]; !ok {
					toAdd = append(toAdd, pm)
					mutations = append(mutations, fmt.Sprintf("add %s", pm.String()))
				}
			}
			if len(toAdd) == 0 {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			status := PlanStatusModify
			if len(currentPorts) == 0 {
				status = PlanStatusCreate
			}
			args := []string{"--quiet", "ports:add", t.App}
			for _, pm := range toAdd {
				args = append(args, pm.String())
			}
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
			return PlanResult{
				InSync:    false,
				Status:    status,
				Reason:    fmt.Sprintf("%d port mapping(s) to add", len(toAdd)),
				Mutations: mutations,
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			currentPorts, err := getPorts(t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			toRemove := []PortMapping{}
			mutations := []string{}
			for _, pm := range t.PortMappings {
				if _, ok := currentPorts[pm.String()]; ok {
					toRemove = append(toRemove, pm)
					mutations = append(mutations, fmt.Sprintf("remove %s", pm.String()))
				}
			}
			if len(toRemove) == 0 {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			args := []string{"--quiet", "ports:remove", t.App}
			for _, pm := range toRemove {
				args = append(args, pm.String())
			}
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    fmt.Sprintf("%d port mapping(s) to remove", len(toRemove)),
				Mutations: mutations,
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// getPorts gets the ports for a given app. A transport-level failure
// (`*subprocess.SSHError`) is propagated; a dokku-level non-zero exit
// (e.g. app does not exist) is treated as "no ports configured."
func getPorts(appName string) (map[string]PortMapping, error) {
	// todo: update dokku to add proper json output for this
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"--quiet",
			"ports:report",
			appName,
			"--ports-map",
		},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return map[string]PortMapping{}, nil
	}

	portMappings := map[string]PortMapping{}
	for _, mapping := range strings.Fields(result.StdoutContents()) {
		parts := strings.Split(mapping, ":")
		if len(parts) != 3 {
			continue
		}

		scheme := parts[0]
		host, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}

		container, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}

		portMappings[mapping] = PortMapping{
			Scheme:    scheme,
			Host:      host,
			Container: container,
		}
	}

	return portMappings, nil
}

// init registers the PortsTask with the task registry
func init() {
	RegisterTask(&PortsTask{})
}
