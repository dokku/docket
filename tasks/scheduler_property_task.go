package tasks

// SchedulerPropertyTask manages the scheduler configuration for a given dokku application
type SchedulerPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the scheduler configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the scheduler configuration should be applied globally"`

	// Property is the name of the scheduler property to set
	Property string `required:"true" yaml:"property" description:"Name of the scheduler property to set"`

	// Value is the value to set for the scheduler property
	Value string `required:"false" yaml:"value,omitempty" description:"Value to set for the scheduler property"`

	// State is the desired state of the scheduler configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the scheduler configuration"`
}

// SchedulerPropertyTaskExample contains an example of a SchedulerPropertyTask
type SchedulerPropertyTaskExample struct {
	// Name is the task name holding the SchedulerPropertyTask description
	Name string `yaml:"-"`

	// SchedulerPropertyTask is the SchedulerPropertyTask configuration
	SchedulerPropertyTask SchedulerPropertyTask `yaml:"dokku_scheduler_property"`
}

// GetName returns the name of the example
func (e SchedulerPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the scheduler property task
func (t SchedulerPropertyTask) Doc() string {
	return "Manages the scheduler configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t SchedulerPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Examples returns the examples for the scheduler property task
func (t SchedulerPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]SchedulerPropertyTaskExample{
		{
			Name: "Selecting the scheduler for an app",
			SchedulerPropertyTask: SchedulerPropertyTask{
				App:      "node-js-app",
				Property: "selected",
				Value:    "docker-local",
			},
		},
		{
			Name: "Selecting the scheduler globally",
			SchedulerPropertyTask: SchedulerPropertyTask{
				Global:   true,
				Property: "selected",
				Value:    "docker-local",
			},
		},
		{
			Name: "Clearing the scheduler property for an app",
			SchedulerPropertyTask: SchedulerPropertyTask{
				App:      "node-js-app",
				Property: "selected",
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the scheduler property
func (t SchedulerPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// schedulerPropertyKeys maps scheduler property names to the JSON keys
// emitted by `dokku scheduler:report --format json` on dokku 0.38.8+.
var schedulerPropertyKeys = map[string]PropertyKeys{
	"selected": {PerApp: "selected", Global: "global-selected"},
	"shell":    {PerApp: "shell", Global: "global-shell"},
}

// Validate checks the SchedulerPropertyTask's inputs without contacting the server.
func (t SchedulerPropertyTask) Validate() error {
	return validatePropertyInput(t.State, t.App, t.Global, t.Property, t.Value, "scheduler:set", schedulerPropertyKeys)
}

// Plan reports the drift the SchedulerPropertyTask would produce.
func (t SchedulerPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "scheduler:set", schedulerPropertyKeys)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t SchedulerPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(app, "scheduler:set", schedulerPropertyKeys, func(app, property, value string) interface{} {
		return SchedulerPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t SchedulerPropertyTask) ExportGlobal() ([]interface{}, error) {
	return exportGlobalProperties("scheduler:set", schedulerPropertyKeys, func(property, value string) interface{} {
		return SchedulerPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the SchedulerPropertyTask with the task registry
func init() {
	RegisterTask(&SchedulerPropertyTask{})
}
