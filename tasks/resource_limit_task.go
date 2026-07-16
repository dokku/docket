package tasks

// ResourceLimitTask manages the resource limits for a given dokku application
type ResourceLimitTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app" description:"Name of the app"`

	// ProcessType is the process type to set resource limits for
	ProcessType string `required:"false" yaml:"process_type,omitempty" description:"Process type to set resource limits for"`

	// Resources is a map of resource type to quantity
	Resources map[string]string `yaml:"resources" description:"Map of resource type to quantity"`

	// ClearBefore clears all resource limits before applying new ones. It is a
	// *bool so the value survives decoding unchanged; nil defaults to false.
	ClearBefore *bool `yaml:"clear_before,omitempty" default:"false" description:"ClearBefore clears all resource limits before applying new ones"`

	// State is the desired state of the resource limits
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the resource limits"`
}

// ResourceLimitTaskExample contains an example of a ResourceLimitTask
type ResourceLimitTaskExample struct {
	// Name is the task name holding the ResourceLimitTask description
	Name string `yaml:"-"`

	// ResourceLimitTask is the ResourceLimitTask configuration
	ResourceLimitTask ResourceLimitTask `yaml:"dokku_resource_limit"`
}

// GetName returns the name of the example
func (e ResourceLimitTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the resource limit task
func (t ResourceLimitTask) Doc() string {
	return "Manages the resource limits for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t ResourceLimitTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Examples returns the examples for the resource limit task
func (t ResourceLimitTask) Examples() ([]Doc, error) {
	return MarshalExamples([]ResourceLimitTaskExample{
		{
			Name: "Set CPU and memory limits",
			ResourceLimitTask: ResourceLimitTask{
				App: "hello-world",
				Resources: map[string]string{
					"cpu":    "100",
					"memory": "256",
				},
			},
		},
		{
			Name: "Set memory limit for web process type",
			ResourceLimitTask: ResourceLimitTask{
				App:         "hello-world",
				ProcessType: "web",
				Resources: map[string]string{
					"memory": "512",
				},
			},
		},
		{
			Name: "Clear all resource limits",
			ResourceLimitTask: ResourceLimitTask{
				App:   "hello-world",
				State: StateAbsent,
			},
		},
	})
}

// Execute sets or clears the resource limits for a given dokku application
func (t ResourceLimitTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// ExportApp reconstructs the app's resource limits, one task per process type.
func (t ResourceLimitTask) ExportApp(app string) ([]interface{}, error) {
	return exportResourceTasks(app, "limit", func(app, processType string, resources map[string]string) interface{} {
		return ResourceLimitTask{App: app, ProcessType: processType, Resources: resources}
	})
}

// Validate checks the ResourceLimitTask's inputs without contacting the server.
func (t ResourceLimitTask) Validate() error {
	return validateResourceInput(t.State, t.Resources)
}

// Plan reports the drift the ResourceLimitTask would produce.
func (t ResourceLimitTask) Plan() PlanResult {
	return planResource(t.State, t.App, t.ProcessType, t.Resources, boolValue(t.ClearBefore, false), "resource:limit")
}

// init registers the ResourceLimitTask with the task registry
func init() {
	RegisterTask(&ResourceLimitTask{})
}
