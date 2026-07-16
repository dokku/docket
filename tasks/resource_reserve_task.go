package tasks

// ResourceReserveTask manages the resource reservations for a given dokku application
type ResourceReserveTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app" description:"Name of the app"`

	// ProcessType is the process type to set resource reservations for
	ProcessType string `required:"false" yaml:"process_type,omitempty" description:"Process type to set resource reservations for"`

	// Resources is a map of resource type to quantity
	Resources map[string]string `yaml:"resources" description:"Map of resource type to quantity"`

	// ClearBefore clears all resource reservations before applying new ones. It
	// is a *bool so the value survives decoding unchanged; nil defaults to false.
	ClearBefore *bool `yaml:"clear_before,omitempty" default:"false" description:"ClearBefore clears all resource reservations before applying new ones"`

	// State is the desired state of the resource reservations
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the resource reservations"`
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

// Examples returns the examples for the resource reserve task
func (t ResourceReserveTask) Examples() ([]Doc, error) {
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
			Name: "Clear all resource reservations",
			ResourceReserveTask: ResourceReserveTask{
				App:   "hello-world",
				State: StateAbsent,
			},
		},
	})
}

// Execute sets or clears the resource reservations for a given dokku application
func (t ResourceReserveTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// ExportApp reconstructs the app's resource reservations, one task per process type.
func (t ResourceReserveTask) ExportApp(app string) ([]interface{}, error) {
	return exportResourceTasks(app, "reserve", func(app, processType string, resources map[string]string) interface{} {
		return ResourceReserveTask{App: app, ProcessType: processType, Resources: resources}
	})
}

// Validate checks the ResourceReserveTask's inputs without contacting the server.
func (t ResourceReserveTask) Validate() error {
	return validateResourceInput(t.State, t.Resources)
}

// Plan reports the drift the ResourceReserveTask would produce.
func (t ResourceReserveTask) Plan() PlanResult {
	return planResource(t.State, t.App, t.ProcessType, t.Resources, boolValue(t.ClearBefore, false), "resource:reserve")
}

// init registers the ResourceReserveTask with the task registry
func init() {
	RegisterTask(&ResourceReserveTask{})
}
