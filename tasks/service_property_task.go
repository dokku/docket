package tasks

import (
	"fmt"

	"github.com/dokku/docket/subprocess"
)

// ServicePropertyTask manages a single dokku service property via
// <service>:set.
//
// Idempotency is intentionally skipped here because dokku service plugins
// expose no uniform way to read back an arbitrary set key: there is no
// per-service report JSON, and the `<service>:info` flags do not correspond
// one-to-one to settable keys (e.g. restart-policy, shm-size, and image are
// settable but not readable via info). The plan therefore reports drift
// unconditionally, and docket export cannot reconstruct these properties. Once
// a machine-readable service property report exists (requested upstream in
// dokku/dokku-datastore#98), this task should switch to probing instead of
// always reporting Changed=true, and its export can move off unsupported.
type ServicePropertyTask struct {
	// Service is the type of service to configure (e.g. redis, postgres, mysql)
	Service string `required:"true" yaml:"service" description:"Type of service to configure (e.g. redis, postgres, mysql)"`

	// Name is the name of the service instance
	Name string `required:"true" yaml:"name" description:"Name of the service instance"`

	// Property is the name of the property to set
	Property string `required:"true" yaml:"property" description:"Name of the property to set"`

	// Value is the value to set the property to. Required when state is present.
	Value string `required:"false" yaml:"value,omitempty" description:"Value to set the property to. Required when state is present."`

	// State is the desired state of the property
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the property"`
}

// ServicePropertyTaskExample contains an example of a ServicePropertyTask
type ServicePropertyTaskExample struct {
	// Name is the task name holding the ServicePropertyTask description
	Name string `yaml:"-"`

	// ServicePropertyTask is the ServicePropertyTask configuration
	ServicePropertyTask ServicePropertyTask `yaml:"dokku_service_property"`
}

// GetName returns the name of the example
func (e ServicePropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the service property task
func (t ServicePropertyTask) Doc() string {
	return "Manages a property for a given dokku service"
}

// ExportSupport reports how docket export handles this task.
func (t ServicePropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportUnsupported, Caveat: "no datastore plugin exposes a machine-readable report of the properties set via `<service>:set`, so they cannot be read back (tracked upstream in dokku/dokku-datastore#98)"}
}

// Requirements lists the non-core dokku plugins this task depends on.
func (t ServicePropertyTask) Requirements() []string {
	return []string{"a dokku datastore service plugin matching the service type (e.g. dokku-postgres, dokku-redis, dokku-mysql)"}
}

// Examples returns a list of ServicePropertyTaskExamples as yaml
func (t ServicePropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]ServicePropertyTaskExample{
		{
			Name: "Set the restart-policy for a postgres service",
			ServicePropertyTask: ServicePropertyTask{
				Service:  "postgres",
				Name:     "my-db",
				Property: "restart-policy",
				Value:    "always",
			},
		},
		{
			Name: "Clear the restart-policy for a postgres service",
			ServicePropertyTask: ServicePropertyTask{
				Service:  "postgres",
				Name:     "my-db",
				Property: "restart-policy",
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or clears the dokku service property
func (t ServicePropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Validate checks the ServicePropertyTask's inputs without contacting the server.
func (t ServicePropertyTask) Validate() error {
	if t.Property == "" {
		return fmt.Errorf("'property' is required")
	}
	if t.State == StatePresent && t.Value == "" {
		return fmt.Errorf("'value' is required when state is 'present'")
	}
	if t.State == StateAbsent && t.Value != "" {
		return fmt.Errorf("'value' must not be set when state is 'absent'")
	}
	return nil
}

// Plan reports the drift the ServicePropertyTask would produce. dokku has no
// reliable way to read back a service set key, so the plan reports drift
// unconditionally once the service is confirmed to exist.
func (t ServicePropertyTask) Plan() PlanResult {
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
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", fmt.Sprintf("%s:set", t.Service), t.Name, t.Property, t.Value},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusModify,
				Reason:    "service property state not probed",
				Mutations: []string{fmt.Sprintf("%s:set %s %s %s", t.Service, t.Name, t.Property, t.Value)},
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
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", fmt.Sprintf("%s:set", t.Service), t.Name, t.Property},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    "service property state not probed",
				Mutations: []string{fmt.Sprintf("%s:set %s %s (clear)", t.Service, t.Name, t.Property)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// init registers the ServicePropertyTask with the task registry
func init() {
	RegisterTask(&ServicePropertyTask{})
}
