package tasks

// PsPropertyTask manages the ps configuration for a given dokku application
type PsPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app"`

	// Global is a flag indicating if the ps configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty"`

	// Property is the name of the ps property to set
	Property string `required:"true" yaml:"property"`

	// Value is the value to set for the ps property
	Value string `required:"false" yaml:"value,omitempty"`

	// State is the desired state of the ps configuration
	State State `required:"true" yaml:"state,omitempty" default:"present" options:"present,absent"`
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

// Plan reports the drift the PsPropertyTask would produce.
func (t PsPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "ps:set", psPropertyKeys)
}

// init registers the PsPropertyTask with the task registry
func init() {
	RegisterTask(&PsPropertyTask{})
}
