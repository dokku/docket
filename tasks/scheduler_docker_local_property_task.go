package tasks

// SchedulerDockerLocalPropertyTask manages the scheduler-docker-local configuration for a given dokku application
type SchedulerDockerLocalPropertyTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app" description:"Name of the app"`

	// Property is the name of the scheduler-docker-local property to set
	Property string `required:"true" yaml:"property" description:"Name of the scheduler-docker-local property to set"`

	// Value is the value to set for the scheduler-docker-local property
	Value string `required:"false" yaml:"value,omitempty" description:"Value to set for the scheduler-docker-local property"`

	// State is the desired state of the scheduler-docker-local configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the scheduler-docker-local configuration"`
}

// SchedulerDockerLocalPropertyTaskExample contains an example of a SchedulerDockerLocalPropertyTask
type SchedulerDockerLocalPropertyTaskExample struct {
	// Name is the task name holding the SchedulerDockerLocalPropertyTask description
	Name string `yaml:"-"`

	// SchedulerDockerLocalPropertyTask is the SchedulerDockerLocalPropertyTask configuration
	SchedulerDockerLocalPropertyTask SchedulerDockerLocalPropertyTask `yaml:"dokku_scheduler_docker_local_property"`
}

// GetName returns the name of the example
func (e SchedulerDockerLocalPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the scheduler-docker-local property task
func (t SchedulerDockerLocalPropertyTask) Doc() string {
	return "Manages the scheduler-docker-local configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t SchedulerDockerLocalPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Examples returns the examples for the scheduler-docker-local property task
func (t SchedulerDockerLocalPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]SchedulerDockerLocalPropertyTaskExample{
		{
			Name: "Enabling the init process for an app",
			SchedulerDockerLocalPropertyTask: SchedulerDockerLocalPropertyTask{
				App:      "node-js-app",
				Property: "init-process",
				Value:    "true",
			},
		},
		{
			Name: "Setting the parallel schedule count for an app",
			SchedulerDockerLocalPropertyTask: SchedulerDockerLocalPropertyTask{
				App:      "node-js-app",
				Property: "parallel-schedule-count",
				Value:    "4",
			},
		},
		{
			Name: "Clearing the init process for an app",
			SchedulerDockerLocalPropertyTask: SchedulerDockerLocalPropertyTask{
				App:      "node-js-app",
				Property: "init-process",
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the scheduler-docker-local property
func (t SchedulerDockerLocalPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// schedulerDockerLocalPropertyKeys maps scheduler-docker-local property
// names to the JSON keys emitted by
// `dokku scheduler-docker-local:report --format json` on dokku 0.38.8+.
// The task struct has no Global field today; map entries set Global="".
var schedulerDockerLocalPropertyKeys = map[string]PropertyKeys{
	"init-process":            {PerApp: "init-process", Global: ""},
	"parallel-schedule-count": {PerApp: "parallel-schedule-count", Global: ""},
}

// Validate checks the SchedulerDockerLocalPropertyTask's inputs without contacting the server.
func (t SchedulerDockerLocalPropertyTask) Validate() error {
	return validatePropertyInput(t.State, t.App, false, t.Property, t.Value, "scheduler-docker-local:set", schedulerDockerLocalPropertyKeys)
}

// Plan reports the drift the SchedulerDockerLocalPropertyTask would produce.
func (t SchedulerDockerLocalPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, false, t.Property, t.Value, "scheduler-docker-local:set", schedulerDockerLocalPropertyKeys)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t SchedulerDockerLocalPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(app, "scheduler-docker-local:set", schedulerDockerLocalPropertyKeys, func(app, property, value string) interface{} {
		return SchedulerDockerLocalPropertyTask{App: app, Property: property, Value: value}
	})
}

// init registers the SchedulerDockerLocalPropertyTask with the task registry
func init() {
	RegisterTask(&SchedulerDockerLocalPropertyTask{})
}
