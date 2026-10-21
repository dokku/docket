package tasks

import "context"

// SchedulerPropertyTask manages the scheduler configuration for a given dokku application
type SchedulerPropertyTask PropertyFields

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

// Constraints declares that the property is set for an app or globally.
func (t SchedulerPropertyTask) Constraints() []Constraint {
	return appOrGlobalConstraints
}

// ExportSupport reports how docket export handles this task.
func (t SchedulerPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t SchedulerPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// MinimumVersions reports the oldest Dokku this task's probe parses.
// The floor is `scheduler:report --format json`, which this task parses.
func (t SchedulerPropertyTask) MinimumVersions() VersionSupport {
	return VersionSupport{Dokku: "0.38.8"}
}

// examples returns the examples for the scheduler property task
func (t SchedulerPropertyTask) examples() ([]Doc, error) {
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
func (t SchedulerPropertyTask) Execute(ctx context.Context) TaskOutputState {
	return executeTask(ctx, t)
}

// schedulerPropertyTable maps scheduler property names to the JSON keys
// emitted by `dokku scheduler:report --format json` on dokku 0.38.8+.
var schedulerPropertyTable = PropertyTable{
	Subcommand: "scheduler:set",
	Keys: map[string]PropertyKeys{
		"selected": {PerApp: "selected", Global: "global-selected"},
		"shell":    {PerApp: "shell", Global: "global-shell"},
	},
}

// propertyTable returns the property schema this task manages.
func (t SchedulerPropertyTask) propertyTable() PropertyTable {
	return schedulerPropertyTable
}

// Validate checks the SchedulerPropertyTask's inputs without contacting the server.
func (t SchedulerPropertyTask) Validate() error {
	return validatePropertyInput(t, t.Global, t.Property)
}

// Plan reports the drift the SchedulerPropertyTask would produce.
func (t SchedulerPropertyTask) Plan(ctx context.Context) PlanResult {
	if r, ok := gatePlan(ctx, t); !ok {
		return r
	}
	return planProperty(ctx, t, func(f PropertyFields) Task { return SchedulerPropertyTask(f) }, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t SchedulerPropertyTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	return exportProperties(ctx, t, app, func(app, property, value string) interface{} {
		return SchedulerPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t SchedulerPropertyTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	return exportGlobalProperties(ctx, t, func(property, value string) interface{} {
		return SchedulerPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the SchedulerPropertyTask with the task registry
func init() {
	RegisterTask(&SchedulerPropertyTask{})
}
