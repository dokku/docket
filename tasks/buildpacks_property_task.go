package tasks

// BuildpacksPropertyTask manages the buildpacks configuration for a given dokku application
type BuildpacksPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the buildpacks configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the buildpacks configuration should be applied globally"`

	// Property is the name of the buildpacks property to set
	Property string `required:"true" yaml:"property" description:"Name of the buildpacks property to set"`

	// Value is the value to set for the buildpacks property
	Value string `required:"false" yaml:"value,omitempty" description:"Value to set for the buildpacks property"`

	// State is the desired state of the buildpacks configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the buildpacks configuration"`
}

// BuildpacksPropertyTaskExample contains an example of a BuildpacksPropertyTask
type BuildpacksPropertyTaskExample struct {
	// Name is the task name holding the BuildpacksPropertyTask description
	Name string `yaml:"-"`

	// BuildpacksPropertyTask is the BuildpacksPropertyTask configuration
	BuildpacksPropertyTask BuildpacksPropertyTask `yaml:"dokku_buildpacks_property"`
}

// GetName returns the name of the example
func (e BuildpacksPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the buildpacks property task
func (t BuildpacksPropertyTask) Doc() string {
	return "Manages the buildpacks configuration for a given dokku application"
}

// Examples returns the examples for the buildpacks property task
func (t BuildpacksPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]BuildpacksPropertyTaskExample{
		{
			Name: "Setting the stack value for an app",
			BuildpacksPropertyTask: BuildpacksPropertyTask{
				App:      "node-js-app",
				Property: "stack",
				Value:    "gliderlabs/herokuish:latest",
			},
		},
		{
			Name: "Setting the stack value globally",
			BuildpacksPropertyTask: BuildpacksPropertyTask{
				Global:   true,
				Property: "stack",
				Value:    "gliderlabs/herokuish:latest",
			},
		},
		{
			Name: "Clearing the stack value for an app",
			BuildpacksPropertyTask: BuildpacksPropertyTask{
				App:      "node-js-app",
				Property: "stack",
			},
		},
	})
}

// Execute sets or unsets the buildpacks property
func (t BuildpacksPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// buildpacksPropertyKeys maps buildpacks property names to the JSON keys
// emitted by `dokku buildpacks:report --format json` on dokku 0.38.8+.
// The buildpacks list (set via `buildpacks:set <app> <buildpack>`) is not a
// property in the typed-task sense and is not modeled here.
var buildpacksPropertyKeys = map[string]PropertyKeys{
	"stack": {PerApp: "stack", Global: "global-stack"},
}

// Plan reports the drift the BuildpacksPropertyTask would produce.
func (t BuildpacksPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "buildpacks:set-property", buildpacksPropertyKeys)
}

// init registers the BuildpacksPropertyTask with the task registry
func init() {
	RegisterTask(&BuildpacksPropertyTask{})
}
