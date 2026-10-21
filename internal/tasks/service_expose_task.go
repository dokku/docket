package tasks

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/dokku/docket/internal/subprocess"
)

// ServiceExposeTask exposes or unexposes a dokku service on host ports
type ServiceExposeTask struct {
	// Service is the type of service to expose (e.g. redis, postgres, mysql)
	Service string `required:"true" identity:"key" serialize:"service.type" yaml:"service" description:"Type of service to expose (e.g. redis, postgres, mysql)"`

	// Name is the name of the service instance
	Name string `required:"true" identity:"key" serialize:"service.name" yaml:"name" description:"Name of the service instance"`

	// Ports are the host ports to expose the service on. Required when state is present.
	Ports []string `required:"false" required_when:"present" forbidden_when:"clear" identity:"collection" ordered:"true" yaml:"ports,omitempty" description:"Host ports to expose the service on"`

	// State is the desired state of the service exposure. present and set
	// both replace the whole port list; set also accepts an empty list, which
	// unexposes the service.
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent,set,clear" description:"Desired state of the service exposure"`
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
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t ServiceExposeTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// MinimumVersions reports the oldest Dokku, and any plugin, this task's probe parses.
// 2.0.0 is the datastore generation whose `<service>:info --exposed-ports`
// output this task parses. It is not the 2.3.1 fingerprint floor the info
// readers use; expose does not read those keys. 0.38.31 is the Dokku the
// integration suite runs.
func (t ServiceExposeTask) MinimumVersions() VersionSupport {
	return VersionSupport{
		Dokku: "0.38.31",
		Plugins: []PluginRequirement{{
			NameField: "Service",
			Minimum:   "2.0.0",
			Text:      "a dokku datastore service plugin >= 2.0.0 matching the service type (e.g. dokku-postgres, dokku-redis, dokku-mysql)",
		}},
	}
}

// ExportGlobal reconstructs the exposed-ports state of every datastore service
// on the server. Discovery is via listServices; the exposed host ports are read
// from `<service>:info <name> --exposed-ports`. Services with no exposed ports
// are skipped.
func (t ServiceExposeTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	services, err := listServices(ctx)
	if err != nil {
		return nil, err
	}
	var out []interface{}
	for _, s := range services {
		ports, err := serviceExposedPortList(ctx, s.Type, s.Name)
		if err != nil {
			return nil, err
		}
		if len(ports) == 0 {
			continue
		}
		out = append(out, ServiceExposeTask{Service: s.Type, Name: s.Name, Ports: ports, State: StatePresent})
	}
	return out, nil
}

// examples returns a list of ServiceExposeTaskExamples as yaml
func (t ServiceExposeTask) examples() ([]Doc, error) {
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
			Name: "Expose a postgres service named my-db on exactly these host ports",
			ServiceExposeTask: ServiceExposeTask{
				Service: "postgres",
				Name:    "my-db",
				Ports:   []string{"5433"},
				State:   StateSet,
			},
		},
		{
			Name: "Unexpose a postgres service named my-db",
			ServiceExposeTask: ServiceExposeTask{
				Service: "postgres",
				Name:    "my-db",
				State:   StateClear,
			},
		},
	})
}

// Execute exposes or unexposes a dokku service
func (t ServiceExposeTask) Execute(ctx context.Context) TaskOutputState {
	return executeTask(ctx, t)
}

// Validate checks the ServiceExposeTask's inputs without contacting the server.
// unexpose takes no ports, which is why a list is forbidden alongside clear:
// it would be silently discarded rather than removed.
func (t ServiceExposeTask) Validate() error {
	return checkConstraints(t)
}

// Plan reports the drift the ServiceExposeTask would produce.
func (t ServiceExposeTask) Plan(ctx context.Context) PlanResult {
	if r, ok := gatePlan(ctx, t); !ok {
		return r
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planServiceExpose(ctx, t, StatePresent) },
		StateAbsent:  func() PlanResult { return planServiceUnexpose(ctx, t, StateAbsent) },
		StateSet: func() PlanResult {
			if len(t.Ports) == 0 {
				return planSetAsClear(planServiceUnexpose(ctx, t, StateClear))
			}
			return planServiceExpose(ctx, t, StateSet)
		},
		StateClear: func() PlanResult { return planServiceUnexpose(ctx, t, StateClear) },
	})
}

// planServiceExpose reports drift for exposing the service on exactly the
// declared ports, for state present or set. finalState is the state the apply
// reports.
//
// dokku rejects expose when the service is already exposed, so a change of
// ports unexposes first and then re-exposes the full list: two commands, not
// one, and a failure between them leaves the service unexposed.
func planServiceExpose(ctx context.Context, t ServiceExposeTask, finalState State) PlanResult {
	exists, err := serviceExists(ctx, t.Service, t.Name)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	if !exists {
		return PlanResult{Status: PlanStatusError, Error: objectMissing(t, ObjectService)}
	}
	current, err := serviceExposedPortList(ctx, t.Service, t.Name)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	// dokku maps expose arguments positionally to the plugin's container
	// ports, so the comparison must be order-sensitive: a reorder of the
	// same ports is a real change (issue #332).
	if slices.Equal(current, t.Ports) {
		return observedServiceExpose(t, PlanResult{InSync: true, Status: PlanStatusOK}, finalState, current)
	}
	inputs := []subprocess.ExecCommandInput{}
	mutations := []string{}
	status := PlanStatusCreate
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
	return observedServiceExpose(t, PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%s service %s not exposed on %s", t.Service, t.Name, strings.Join(t.Ports, " ")),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, finalState, inputs)
		},
	}, finalState, current)
}

// planServiceUnexpose reports drift for unexposing the service from every
// port, for state absent or clear. finalState is the state the apply reports.
func planServiceUnexpose(ctx context.Context, t ServiceExposeTask, finalState State) PlanResult {
	exists, err := serviceExists(ctx, t.Service, t.Name)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	if !exists {
		return PlanResult{Status: PlanStatusError, Error: objectMissing(t, ObjectService)}
	}
	current, err := serviceExposedPortList(ctx, t.Service, t.Name)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	if len(current) == 0 {
		return observedServiceExpose(t, PlanResult{InSync: true, Status: PlanStatusOK}, finalState, current)
	}
	inputs := []subprocess.ExecCommandInput{{
		Command: "dokku",
		Args:    []string{"--quiet", fmt.Sprintf("%s:unexpose", t.Service), t.Name},
	}}
	return observedServiceExpose(t, PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("%s service %s exposed", t.Service, t.Name),
		Mutations: []string{fmt.Sprintf("%s:unexpose %s", t.Service, t.Name)},
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StatePresent}, finalState, inputs)
		},
	}, finalState, current)
}

// serviceExposedPortList returns the ordered host ports a dokku service is
// currently exposed on, read from `<service>:info <name> --exposed-ports`. That
// command reports `container->host` pairs (e.g. `5432->5432`, or `5432->127.0.0.1:5433`
// when bound to a specific interface), so only the host side after the first
// `->` is kept - which is what `<service>:expose` takes and what the task's
// Ports field holds. A transport-level failure (`*subprocess.SSHError`) is
// propagated; a dokku-level non-zero exit (e.g. service not exposed) is treated
// as "no ports exposed."
func serviceExposedPortList(ctx context.Context, service, name string) ([]string, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
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
		return nil, nil
	}

	var ports []string
	for _, field := range strings.Fields(result.StdoutContents()) {
		if field == "-" {
			continue
		}
		host := field
		if i := strings.Index(field, "->"); i >= 0 {
			host = field[i+len("->"):]
		}
		if host != "" {
			ports = append(ports, host)
		}
	}
	return ports, nil
}

// init registers the ServiceExposeTask with the task registry
func init() {
	RegisterTask(&ServiceExposeTask{})
}

// observedServiceExpose attaches the exposure the probe read to r. Every state
// owns the service's whole port list - absent unexposes every port - so
// Observed carries all of it. state is the state the plan was made for:
// present and absent report the exposure as present or absent, while set and
// clear report it as set, or as clear when no port is exposed.
func observedServiceExpose(t ServiceExposeTask, r PlanResult, state State, current []string) PlanResult {
	authoritative := state == StateSet || state == StateClear
	o := ServiceExposeTask{Service: t.Service, Name: t.Name, State: StateAbsent}
	if authoritative {
		o.State = StateClear
	}
	if len(current) > 0 {
		o.Ports = current
		o.State = StatePresent
		if authoritative {
			o.State = StateSet
		}
	}
	return observed(r, o, len(current) > 0)
}
