package tasks

import "context"

// PsPropertyTask manages the ps configuration for a given dokku application
type PsPropertyTask PropertyFields

// PsPropertyTaskExample contains an example of a PsPropertyTask
type PsPropertyTaskExample struct {
	// Name is the task name holding the PsPropertyTask description
	Name string `yaml:"-"`

	// PsPropertyTask is the PsPropertyTask configuration
	PsPropertyTask PsPropertyTask `yaml:"dokku_ps_property"`
}

// GetName returns the name of the example
func (e PsPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the ps property task
func (t PsPropertyTask) Doc() string {
	return "Manages the ps configuration for a given dokku application"
}

// Constraints declares that the property is set for an app or globally.
func (t PsPropertyTask) Constraints() []Constraint {
	return appOrGlobalConstraints
}

// ExportSupport reports how docket export handles this task.
func (t PsPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t PsPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// MinimumVersions reports the oldest Dokku this task's probe parses.
// The floor is `ps:report --format json`, which this task parses.
func (t PsPropertyTask) MinimumVersions() VersionSupport {
	return VersionSupport{Dokku: "0.38.9"}
}

// examples returns the examples for the ps property task
func (t PsPropertyTask) examples() ([]Doc, error) {
	return MarshalExamples([]PsPropertyTaskExample{
		{
			Name: "Setting the restart-policy value for an app",
			PsPropertyTask: PsPropertyTask{
				App:      "node-js-app",
				Property: "restart-policy",
				Value:    "on-failure:5",
			},
		},
		{
			Name: "Setting the restart-policy value globally",
			PsPropertyTask: PsPropertyTask{
				Global:   true,
				Property: "restart-policy",
				Value:    "on-failure:5",
			},
		},
		{
			Name: "Clearing the restart-policy value for an app",
			PsPropertyTask: PsPropertyTask{
				App:      "node-js-app",
				Property: "restart-policy",
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the ps property
func (t PsPropertyTask) Execute(ctx context.Context) TaskOutputState {
	return executeTask(ctx, t)
}

// psPropertyTable maps ps property names to the JSON keys emitted by
// `dokku ps:report --format json` on dokku 0.38.9+. dockerfile-start-cmd and
// start-cmd are per-app only; everything else (including restart-policy as of
// 0.38.9) is app+global.
var psPropertyTable = PropertyTable{
	Subcommand: "ps:set",
	Keys: map[string]PropertyKeys{
		"dockerfile-start-cmd": {PerApp: "dockerfile-start-cmd", Global: ""},
		"procfile-path":        {PerApp: "procfile-path", Global: "global-procfile-path"},
		"restart-policy":       {PerApp: "restart-policy", Global: "global-restart-policy"},
		"skip-deploy":          {PerApp: "skip-deploy", Global: "global-skip-deploy"},
		"start-cmd":            {PerApp: "start-cmd", Global: ""},
		"stop-timeout-seconds": {PerApp: "stop-timeout-seconds", Global: "global-stop-timeout-seconds"},
	},
}

// propertyTable returns the property schema this task manages.
func (t PsPropertyTask) propertyTable() PropertyTable {
	return psPropertyTable
}

// Validate checks the PsPropertyTask's inputs without contacting the server.
func (t PsPropertyTask) Validate() error {
	return validatePropertyInput(t, t.Global, t.Property)
}

// Plan reports the drift the PsPropertyTask would produce.
func (t PsPropertyTask) Plan(ctx context.Context) PlanResult {
	if r, ok := gatePlan(ctx, t); !ok {
		return r
	}
	return planProperty(ctx, t, func(f PropertyFields) Task { return PsPropertyTask(f) }, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t PsPropertyTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	return exportProperties(ctx, t, app, func(app, property, value string) interface{} {
		return PsPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t PsPropertyTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	return exportGlobalProperties(ctx, t, func(property, value string) interface{} {
		return PsPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the PsPropertyTask with the task registry
func init() {
	RegisterTask(&PsPropertyTask{})
}
