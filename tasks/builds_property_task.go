package tasks

// BuildsPropertyTask manages the builds configuration for a given dokku application
type BuildsPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app"`

	// Global is a flag indicating if the builds configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty"`

	// Property is the name of the builds property to set
	Property string `required:"true" yaml:"property"`

	// Value is the value to set for the builds property
	Value string `required:"false" yaml:"value,omitempty"`

	// State is the desired state of the builds configuration
	State State `required:"true" yaml:"state,omitempty" default:"present" options:"present,absent"`
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

// Plan reports the drift the BuildsPropertyTask would produce.
func (t BuildsPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "builds:set", buildsPropertyKeys)
}

// init registers the BuildsPropertyTask with the task registry
func init() {
	RegisterTask(&BuildsPropertyTask{})
}
