package tasks

// PsPropertyTask manages the ps configuration for a given dokku application
type PsPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the ps configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the ps configuration should be applied globally"`

	// Property is the name of the ps property to set
	Property string `required:"true" yaml:"property" description:"Name of the ps property to set"`

	// Value is the value to set for the ps property
	Value string `required:"false" yaml:"value,omitempty" description:"Value to set for the ps property"`

	// State is the desired state of the ps configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the ps configuration"`
}

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

// ExportSupport reports how docket export handles this task.
func (t PsPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Examples returns the examples for the ps property task
func (t PsPropertyTask) Examples() ([]Doc, error) {
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
func (t PsPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// psPropertyKeys maps ps property names to the JSON keys emitted by
// `dokku ps:report --format json` on dokku 0.38.9+. dockerfile-start-cmd and
// start-cmd are per-app only; everything else (including restart-policy as of
// 0.38.9) is app+global.
var psPropertyKeys = map[string]PropertyKeys{
	"dockerfile-start-cmd": {PerApp: "dockerfile-start-cmd", Global: ""},
	"procfile-path":        {PerApp: "procfile-path", Global: "global-procfile-path"},
	"restart-policy":       {PerApp: "restart-policy", Global: "global-restart-policy"},
	"skip-deploy":          {PerApp: "skip-deploy", Global: "global-skip-deploy"},
	"start-cmd":            {PerApp: "start-cmd", Global: ""},
	"stop-timeout-seconds": {PerApp: "stop-timeout-seconds", Global: "global-stop-timeout-seconds"},
}

// Validate checks the PsPropertyTask's inputs without contacting the server.
func (t PsPropertyTask) Validate() error {
	return validatePropertyInput(t.State, t.App, t.Global, t.Property, t.Value, "ps:set", psPropertyKeys)
}

// Plan reports the drift the PsPropertyTask would produce.
func (t PsPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "ps:set", psPropertyKeys)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t PsPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(app, "ps:set", psPropertyKeys, func(app, property, value string) interface{} {
		return PsPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t PsPropertyTask) ExportGlobal() ([]interface{}, error) {
	return exportGlobalProperties("ps:set", psPropertyKeys, func(property, value string) interface{} {
		return PsPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the PsPropertyTask with the task registry
func init() {
	RegisterTask(&PsPropertyTask{})
}
