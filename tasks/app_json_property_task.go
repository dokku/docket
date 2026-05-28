package tasks

// AppJsonPropertyTask manages the app.json configuration for a given dokku application
type AppJsonPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app"`

	// Global is a flag indicating if the app.json configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty"`

	// Property is the name of the app.json property to set
	Property string `required:"true" yaml:"property"`

	// Value is the value to set for the app.json property
	Value string `required:"false" yaml:"value,omitempty"`

	// State is the desired state of the app.json configuration
	State State `required:"true" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// AppJsonPropertyTaskExample contains an example of an AppJsonPropertyTask
type AppJsonPropertyTaskExample struct {
	// Name is the task name holding the AppJsonPropertyTask description
	Name string `yaml:"-"`

	// AppJsonPropertyTask is the AppJsonPropertyTask configuration
	AppJsonPropertyTask AppJsonPropertyTask `yaml:"dokku_app_json_property"`
}

// GetName returns the name of the example
func (e AppJsonPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the app.json property task
func (t AppJsonPropertyTask) Doc() string {
	return "Manages the app.json configuration for a given dokku application"
}

// Examples returns the examples for the app.json property task
func (t AppJsonPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]AppJsonPropertyTaskExample{
		{
			Name: "Setting the appjson-path for an app",
			AppJsonPropertyTask: AppJsonPropertyTask{
				App:      "node-js-app",
				Property: "appjson-path",
				Value:    "app.json",
			},
		},
		{
			Name: "Setting the appjson-path globally",
			AppJsonPropertyTask: AppJsonPropertyTask{
				Global:   true,
				Property: "appjson-path",
				Value:    "app.json",
			},
		},
		{
			Name: "Clearing the appjson-path for an app",
			AppJsonPropertyTask: AppJsonPropertyTask{
				App:      "node-js-app",
				Property: "appjson-path",
			},
		},
	})
}

// Execute sets or unsets the app.json property
func (t AppJsonPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// appJsonPropertyKeys maps app-json property names to the JSON keys emitted
// by `dokku app-json:report --format json` on dokku 0.38.8+.
var appJsonPropertyKeys = map[string]PropertyKeys{
	"appjson-path": {PerApp: "appjson-path", Global: "global-appjson-path"},
}

// Plan reports the drift the AppJsonPropertyTask would produce.
func (t AppJsonPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "app-json:set", appJsonPropertyKeys)
}

// init registers the AppJsonPropertyTask with the task registry
func init() {
	RegisterTask(&AppJsonPropertyTask{})
}
