package tasks

// BuildsPropertyTask manages the builds configuration for a given dokku application
type BuildsPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the builds configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the builds configuration should be applied globally"`

	// Property is the name of the builds property to set
	Property string `required:"true" yaml:"property" description:"Name of the builds property to set"`

	// Value is the value to set for the builds property
	Value string `required:"false" yaml:"value,omitempty" description:"Value to set for the builds property"`

	// State is the desired state of the builds configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the builds configuration"`
}

// BuildsPropertyTaskExample contains an example of a BuildsPropertyTask
type BuildsPropertyTaskExample struct {
	// Name is the task name holding the BuildsPropertyTask description
	Name string `yaml:"-"`

	// BuildsPropertyTask is the BuildsPropertyTask configuration
	BuildsPropertyTask BuildsPropertyTask `yaml:"dokku_builds_property"`
}

// GetName returns the name of the example
func (e BuildsPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the builds property task
func (t BuildsPropertyTask) Doc() string {
	return "Manages the builds configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t BuildsPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Examples returns the examples for the builds property task
func (t BuildsPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]BuildsPropertyTaskExample{
		{
			Name: "Setting the retention value for an app",
			BuildsPropertyTask: BuildsPropertyTask{
				App:      "node-js-app",
				Property: "retention",
				Value:    "50",
			},
		},
		{
			Name: "Setting the retention value globally",
			BuildsPropertyTask: BuildsPropertyTask{
				Global:   true,
				Property: "retention",
				Value:    "50",
			},
		},
		{
			Name: "Clearing the retention value for an app",
			BuildsPropertyTask: BuildsPropertyTask{
				App:      "node-js-app",
				Property: "retention",
			},
		},
	})
}

// Execute sets or unsets the builds property
func (t BuildsPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// buildsPropertyKeys maps builds property names to the JSON keys emitted by
// `dokku builds:report --format json` on dokku 0.38.8+.
var buildsPropertyKeys = map[string]PropertyKeys{
	"retention": {PerApp: "retention", Global: "global-retention"},
}

// Validate checks the BuildsPropertyTask's inputs without contacting the server.
func (t BuildsPropertyTask) Validate() error {
	return validatePropertyInput(t.State, t.App, t.Global, t.Property, t.Value, "builds:set", buildsPropertyKeys)
}

// Plan reports the drift the BuildsPropertyTask would produce.
func (t BuildsPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "builds:set", buildsPropertyKeys)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t BuildsPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(app, "builds:set", buildsPropertyKeys, func(app, property, value string) interface{} {
		return BuildsPropertyTask{App: app, Property: property, Value: value}
	})
}

// init registers the BuildsPropertyTask with the task registry
func init() {
	RegisterTask(&BuildsPropertyTask{})
}
