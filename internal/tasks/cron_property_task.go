package tasks

import "context"

// CronPropertyTask manages the cron configuration for a given dokku application
type CronPropertyTask PropertyFields

// CronPropertyTaskExample contains an example of a CronPropertyTask
type CronPropertyTaskExample struct {
	// Name is the task name holding the CronPropertyTask description
	Name string `yaml:"-"`

	// CronPropertyTask is the CronPropertyTask configuration
	CronPropertyTask CronPropertyTask `yaml:"dokku_cron_property"`
}

// GetName returns the name of the example
func (e CronPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the cron property task
func (t CronPropertyTask) Doc() string {
	return "Manages the cron configuration for a given dokku application"
}

// Constraints declares that the property is set for an app or globally.
func (t CronPropertyTask) Constraints() []Constraint {
	return appOrGlobalConstraints
}

// ExportSupport reports how docket export handles this task.
func (t CronPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t CronPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// MinimumVersions reports the oldest Dokku this task's probe parses.
// The floor is `cron:report --format json`, which this task parses.
func (t CronPropertyTask) MinimumVersions() VersionSupport {
	return VersionSupport{Dokku: "0.38.8"}
}

// examples returns the examples for the cron property task
func (t CronPropertyTask) examples() ([]Doc, error) {
	return MarshalExamples([]CronPropertyTaskExample{
		{
			Name: "Enabling maintenance mode for an app",
			CronPropertyTask: CronPropertyTask{
				App:      "node-js-app",
				Property: "maintenance",
				Value:    "true",
			},
		},
		{
			Name: "Setting the mailto address globally",
			CronPropertyTask: CronPropertyTask{
				Global:   true,
				Property: "mailto",
				Value:    "ops@example.com",
			},
		},
		{
			Name: "Clearing the maintenance mode for an app",
			CronPropertyTask: CronPropertyTask{
				App:      "node-js-app",
				Property: "maintenance",
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the cron property
func (t CronPropertyTask) Execute(ctx context.Context) TaskOutputState {
	return executeTask(ctx, t)
}

// cronPropertyTable maps cron property names to the JSON keys emitted by
// `dokku cron:report --format json` on dokku 0.38.8+. mailfrom/mailto are
// global-only.
var cronPropertyTable = PropertyTable{
	Subcommand: "cron:set",
	Keys: map[string]PropertyKeys{
		"maintenance": {PerApp: "maintenance", Global: "global-maintenance"},
		"mailfrom":    {PerApp: "", Global: "global-mailfrom"},
		"mailto":      {PerApp: "", Global: "global-mailto"},
	},
}

// propertyTable returns the property schema this task manages.
func (t CronPropertyTask) propertyTable() PropertyTable {
	return cronPropertyTable
}

// Validate checks the CronPropertyTask's inputs without contacting the server.
func (t CronPropertyTask) Validate() error {
	return validatePropertyInput(t, t.Global, t.Property)
}

// Plan reports the drift the CronPropertyTask would produce.
func (t CronPropertyTask) Plan(ctx context.Context) (result PlanResult) {
	if r, ok := gatePlan(ctx, t); !ok {
		return r
	}
	defer recordPlanned(&result, t)
	return planProperty(ctx, t, func(f PropertyFields) Task { return CronPropertyTask(f) }, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t CronPropertyTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	return exportProperties(ctx, t, app, func(app, property, value string) interface{} {
		return CronPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t CronPropertyTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	return exportGlobalProperties(ctx, t, func(property, value string) interface{} {
		return CronPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the CronPropertyTask with the task registry
func init() {
	RegisterTask(&CronPropertyTask{})
}
