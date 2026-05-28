package tasks

// ChecksPropertyTask manages the checks configuration for a given dokku application
type ChecksPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app"`

	// Global is a flag indicating if the checks configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty"`

	// Property is the name of the checks property to set
	Property string `required:"true" yaml:"property"`

	// Value is the value to set for the checks property
	Value string `required:"false" yaml:"value,omitempty"`

	// State is the desired state of the checks configuration
	State State `required:"true" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// ChecksPropertyTaskExample contains an example of a ChecksPropertyTask
type ChecksPropertyTaskExample struct {
	// Name is the task name holding the ChecksPropertyTask description
	Name string `yaml:"-"`

	// ChecksPropertyTask is the ChecksPropertyTask configuration
	ChecksPropertyTask ChecksPropertyTask `yaml:"dokku_checks_property"`
}

// GetName returns the name of the example
func (e ChecksPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the checks property task
func (t ChecksPropertyTask) Doc() string {
	return "Manages the checks configuration for a given dokku application"
}

// Examples returns the examples for the checks property task
func (t ChecksPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]ChecksPropertyTaskExample{
		{
			Name: "Setting the wait-to-retire value for an app",
			ChecksPropertyTask: ChecksPropertyTask{
				App:      "node-js-app",
				Property: "wait-to-retire",
				Value:    "60",
			},
		},
		{
			Name: "Setting the wait-to-retire value globally",
			ChecksPropertyTask: ChecksPropertyTask{
				Global:   true,
				Property: "wait-to-retire",
				Value:    "60",
			},
		},
		{
			Name: "Clearing the wait-to-retire value for an app",
			ChecksPropertyTask: ChecksPropertyTask{
				App:      "node-js-app",
				Property: "wait-to-retire",
			},
		},
	})
}

// Execute sets or unsets the checks property
func (t ChecksPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// checksPropertyKeys maps checks property names to the JSON keys emitted by
// `dokku checks:report --format json` on dokku 0.38.8+.
var checksPropertyKeys = map[string]PropertyKeys{
	"wait-to-retire": {PerApp: "wait-to-retire", Global: "global-wait-to-retire"},
}

// Plan reports the drift the ChecksPropertyTask would produce.
func (t ChecksPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "checks:set", checksPropertyKeys)
}

// init registers the ChecksPropertyTask with the task registry
func init() {
	RegisterTask(&ChecksPropertyTask{})
}
