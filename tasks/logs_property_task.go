package tasks

// LogsPropertyTask manages the logs configuration for a given dokku application
type LogsPropertyTask PropertyFields

// LogsPropertyTaskExample contains an example of a LogsPropertyTask
type LogsPropertyTaskExample struct {
	// Name is the task name holding the LogsPropertyTask description
	Name string `yaml:"-"`

	// LogsPropertyTask is the LogsPropertyTask configuration
	LogsPropertyTask LogsPropertyTask `yaml:"dokku_logs_property"`
}

// GetName returns the name of the example
func (e LogsPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the logs property task
func (t LogsPropertyTask) Doc() string {
	return "Manages the logs configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t LogsPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t LogsPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// Examples returns the examples for the logs property task
func (t LogsPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]LogsPropertyTaskExample{
		{
			Name: "Setting the max-size value for an app",
			LogsPropertyTask: LogsPropertyTask{
				App:      "node-js-app",
				Property: "max-size",
				Value:    "100m",
			},
		},
		{
			Name: "Setting the max-size value globally",
			LogsPropertyTask: LogsPropertyTask{
				Global:   true,
				Property: "max-size",
				Value:    "100m",
			},
		},
		{
			Name: "Clearing the max-size value for an app",
			LogsPropertyTask: LogsPropertyTask{
				App:      "node-js-app",
				Property: "max-size",
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the logs property
func (t LogsPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// logsPropertyTable maps logs property names to the JSON keys emitted by
// `dokku logs:report --format json` on dokku 0.38.8+. vector-image and
// vector-networks are global-only.
var logsPropertyTable = PropertyTable{
	Subcommand: "logs:set",
	Keys: map[string]PropertyKeys{
		"app-label-alias": {PerApp: "app-label-alias", Global: "global-app-label-alias"},
		"max-size":        {PerApp: "max-size", Global: "global-max-size"},
		"vector-image":    {PerApp: "", Global: "global-vector-image"},
		"vector-networks": {PerApp: "", Global: "global-vector-networks"},
		"vector-sink":     {PerApp: "vector-sink", Global: "global-vector-sink"},
	},
}

// PropertyTable returns the property schema this task manages.
func (t LogsPropertyTask) PropertyTable() PropertyTable {
	return logsPropertyTable
}

// Validate checks the LogsPropertyTask's inputs without contacting the server.
func (t LogsPropertyTask) Validate() error {
	return validatePropertyInput(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// Plan reports the drift the LogsPropertyTask would produce.
func (t LogsPropertyTask) Plan() PlanResult {
	return planProperty(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t LogsPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(t, app, func(app, property, value string) interface{} {
		return LogsPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t LogsPropertyTask) ExportGlobal() ([]interface{}, error) {
	return exportGlobalProperties(t, func(property, value string) interface{} {
		return LogsPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the LogsPropertyTask with the task registry
func init() {
	RegisterTask(&LogsPropertyTask{})
}
