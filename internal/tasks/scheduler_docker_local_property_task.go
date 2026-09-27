package tasks

import "context"

// SchedulerDockerLocalPropertyTask manages the scheduler-docker-local configuration for a given dokku application
type SchedulerDockerLocalPropertyTask PropertyFields

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

// ProbeSupport reports whether Plan() can read this task's current state.
func (t SchedulerDockerLocalPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// examples returns the examples for the scheduler-docker-local property task
func (t SchedulerDockerLocalPropertyTask) examples() ([]Doc, error) {
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
			Name: "Setting the parallel schedule count globally",
			SchedulerDockerLocalPropertyTask: SchedulerDockerLocalPropertyTask{
				Global:   true,
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
func (t SchedulerDockerLocalPropertyTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// schedulerDockerLocalPropertyTable maps scheduler-docker-local property
// names to the JSON keys emitted by
// `dokku scheduler-docker-local:report --format json` on dokku 0.38.8+.
var schedulerDockerLocalPropertyTable = PropertyTable{
	Subcommand: "scheduler-docker-local:set",
	Keys: map[string]PropertyKeys{
		"init-process":            {PerApp: "init-process", Global: "global-init-process"},
		"parallel-schedule-count": {PerApp: "parallel-schedule-count", Global: "global-parallel-schedule-count"},
	},
}

// propertyTable returns the property schema this task manages.
func (t SchedulerDockerLocalPropertyTask) propertyTable() PropertyTable {
	return schedulerDockerLocalPropertyTable
}

// Validate checks the SchedulerDockerLocalPropertyTask's inputs without contacting the server.
func (t SchedulerDockerLocalPropertyTask) Validate() error {
	return validatePropertyInput(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// Plan reports the drift the SchedulerDockerLocalPropertyTask would produce.
func (t SchedulerDockerLocalPropertyTask) Plan(ctx context.Context) PlanResult {
	return planProperty(ctx, t, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t SchedulerDockerLocalPropertyTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	return exportProperties(ctx, t, app, func(app, property, value string) interface{} {
		return SchedulerDockerLocalPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t SchedulerDockerLocalPropertyTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	return exportGlobalProperties(ctx, t, func(property, value string) interface{} {
		return SchedulerDockerLocalPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the SchedulerDockerLocalPropertyTask with the task registry
func init() {
	RegisterTask(&SchedulerDockerLocalPropertyTask{})
}
