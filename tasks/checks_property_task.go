package tasks

// ChecksPropertyTask manages the checks configuration for a given dokku application
type ChecksPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the checks configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the checks configuration should be applied globally"`

	// Property is the name of the checks property to set
	Property string `required:"true" yaml:"property" description:"Name of the checks property to set"`

	// Value is the value to set for the checks property
	Value string `required:"false" yaml:"value,omitempty" description:"Value to set for the checks property"`

	// State is the desired state of the checks configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the checks configuration"`
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

// ExportSupport reports how docket export handles this task.
func (t ChecksPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
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
				State:    StateAbsent,
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

// Validate checks the ChecksPropertyTask's inputs without contacting the server.
func (t ChecksPropertyTask) Validate() error {
	return validatePropertyInput(t.State, t.App, t.Global, t.Property, t.Value, "checks:set", checksPropertyKeys)
}

// Plan reports the drift the ChecksPropertyTask would produce.
func (t ChecksPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "checks:set", checksPropertyKeys)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t ChecksPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(app, "checks:set", checksPropertyKeys, func(app, property, value string) interface{} {
		return ChecksPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t ChecksPropertyTask) ExportGlobal() ([]interface{}, error) {
	return exportGlobalProperties("checks:set", checksPropertyKeys, func(property, value string) interface{} {
		return ChecksPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the ChecksPropertyTask with the task registry
func init() {
	RegisterTask(&ChecksPropertyTask{})
}
