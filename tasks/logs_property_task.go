package tasks

// LogsPropertyTask manages the logs configuration for a given dokku application
type LogsPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the logs configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the logs configuration should be applied globally"`

	// Property is the name of the logs property to set
	Property string `required:"true" yaml:"property" description:"Name of the logs property to set"`

	// Value is the value to set for the logs property
	Value string `required:"false" yaml:"value,omitempty" description:"Value to set for the logs property"`

	// State is the desired state of the logs configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the logs configuration"`
}

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
			},
		},
	})
}

// Execute sets or unsets the logs property
func (t LogsPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// logsPropertyKeys maps logs property names to the JSON keys emitted by
// `dokku logs:report --format json` on dokku 0.38.8+. vector-image and
// vector-networks are global-only.
var logsPropertyKeys = map[string]PropertyKeys{
	"app-label-alias": {PerApp: "app-label-alias", Global: "global-app-label-alias"},
	"max-size":        {PerApp: "max-size", Global: "global-max-size"},
	"vector-image":    {PerApp: "", Global: "global-vector-image"},
	"vector-networks": {PerApp: "", Global: "global-vector-networks"},
	"vector-sink":     {PerApp: "vector-sink", Global: "global-vector-sink"},
}

// Validate checks the LogsPropertyTask's inputs without contacting the server.
func (t LogsPropertyTask) Validate() error {
	return validatePropertyInput(t.State, t.App, t.Global, t.Property, t.Value, "logs:set", logsPropertyKeys)
}

// Plan reports the drift the LogsPropertyTask would produce.
func (t LogsPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "logs:set", logsPropertyKeys)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t LogsPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(app, "logs:set", logsPropertyKeys, func(app, property, value string) interface{} {
		return LogsPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t LogsPropertyTask) ExportGlobal() ([]interface{}, error) {
	return exportGlobalProperties("logs:set", logsPropertyKeys, func(property, value string) interface{} {
		return LogsPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the LogsPropertyTask with the task registry
func init() {
	RegisterTask(&LogsPropertyTask{})
}
