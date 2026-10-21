package tasks

import "context"

// ResourceReserveTask manages the resource reservations for a given dokku application
type ResourceReserveTask struct {
	// App is the name of the app
	App string `required:"true" identity:"key" serialize:"app" yaml:"app" description:"Name of the app"`

	// ProcessType is the process type to set resource reservations for
	ProcessType string `required:"false" identity:"key" yaml:"process_type,omitempty" description:"Process type to set resource reservations for"`

	// Resources is a map of resource type to quantity
	Resources map[string]string `required_when:"present" forbidden_when:"clear" identity:"collection" yaml:"resources,omitempty" description:"Map of resource type to quantity"`

	// ClearBefore clears all resource reservations before applying new ones. It
	// is a *bool so the value survives decoding unchanged; nil defaults to false.
	ClearBefore *bool `forbidden_when:"set,clear" apply_only:"true" yaml:"clear_before,omitempty" default:"false" description:"ClearBefore clears all resource reservations before applying new ones"`

	// State is the desired state of the resource reservations
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent,set,clear" description:"Desired state of the resource reservations"`
}

// ResourceReserveTaskExample contains an example of a ResourceReserveTask
type ResourceReserveTaskExample struct {
	// Name is the task name holding the ResourceReserveTask description
	Name string `yaml:"-"`

	// ResourceReserveTask is the ResourceReserveTask configuration
	ResourceReserveTask ResourceReserveTask `yaml:"dokku_resource_reserve"`
}

// GetName returns the name of the example
func (e ResourceReserveTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the resource reserve task
func (t ResourceReserveTask) Doc() string {
	return "Manages the resource reservations for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t ResourceReserveTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t ResourceReserveTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// MinimumVersions reports the oldest Dokku this task's probe parses.
// The floor is 0.38.31, the release the integration suite runs; the probe is `resource:reserve`.
func (t ResourceReserveTask) MinimumVersions() VersionSupport {
	return VersionSupport{Dokku: "0.38.31"}
}

// examples returns the examples for the resource reserve task
func (t ResourceReserveTask) examples() ([]Doc, error) {
	return MarshalExamples([]ResourceReserveTaskExample{
		{
			Name: "Set CPU and memory reservations",
			ResourceReserveTask: ResourceReserveTask{
				App: "hello-world",
				Resources: map[string]string{
					"cpu":    "100",
					"memory": "256",
				},
			},
		},
		{
			Name: "Set memory reservation for web process type",
			ResourceReserveTask: ResourceReserveTask{
				App:         "hello-world",
				ProcessType: "web",
				Resources: map[string]string{
					"memory": "512",
				},
			},
		},
		{
			Name: "Replace the resource reservations with exactly these",
			ResourceReserveTask: ResourceReserveTask{
				App: "hello-world",
				Resources: map[string]string{
					"memory": "256",
				},
				State: StateSet,
			},
		},
		{
			Name: "Clear all resource reservations",
			ResourceReserveTask: ResourceReserveTask{
				App:   "hello-world",
				State: StateClear,
			},
		},
	})
}

// Execute sets or clears the resource reservations for a given dokku application
func (t ResourceReserveTask) Execute(ctx context.Context) TaskOutputState {
	return executeTask(ctx, t)
}

// ExportApp reconstructs the app's resource reservations, one task per process type.
func (t ResourceReserveTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	return exportResourceTasks(ctx, app, "reserve", func(app, processType string, resources map[string]string) interface{} {
		return ResourceReserveTask{App: app, ProcessType: processType, Resources: resources}
	})
}

// Validate checks the ResourceReserveTask's inputs without contacting the server.
func (t ResourceReserveTask) Validate() error {
	if err := checkConstraints(t); err != nil {
		return err
	}
	return validateResourceInput(t.State, t.Resources)
}

// Plan reports the drift the ResourceReserveTask would produce.
func (t ResourceReserveTask) Plan(ctx context.Context) PlanResult {
	if r, ok := gatePlan(ctx, t); !ok {
		return r
	}
	return planResource(ctx, func(resources map[string]string, state State) Task {
		return ResourceReserveTask{App: t.App, ProcessType: t.ProcessType, Resources: resources, State: state}
	}, t.State, t.App, t.ProcessType, t.Resources, boolValue(t.ClearBefore, false), "resource:reserve")
}

// init registers the ResourceReserveTask with the task registry
func init() {
	RegisterTask(&ResourceReserveTask{})
}
