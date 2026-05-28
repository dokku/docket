package tasks

// BuilderHerokuishPropertyTask manages the builder-herokuish configuration for a given dokku application
type BuilderHerokuishPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app"`

	// Global is a flag indicating if the builder-herokuish configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty"`

	// Property is the name of the builder-herokuish property to set
	Property string `required:"true" yaml:"property"`

	// Value is the value to set for the builder-herokuish property
	Value string `required:"false" yaml:"value,omitempty"`

	// State is the desired state of the builder-herokuish configuration
	State State `required:"true" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// BuilderHerokuishPropertyTaskExample contains an example of a BuilderHerokuishPropertyTask
type BuilderHerokuishPropertyTaskExample struct {
	// Name is the task name holding the BuilderHerokuishPropertyTask description
	Name string `yaml:"-"`

	// BuilderHerokuishPropertyTask is the BuilderHerokuishPropertyTask configuration
	BuilderHerokuishPropertyTask BuilderHerokuishPropertyTask `yaml:"dokku_builder_herokuish_property"`
}

// GetName returns the name of the example
func (e BuilderHerokuishPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the builder-herokuish property task
func (t BuilderHerokuishPropertyTask) Doc() string {
	return "Manages the builder-herokuish configuration for a given dokku application"
}

// Examples returns the examples for the builder-herokuish property task
func (t BuilderHerokuishPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]BuilderHerokuishPropertyTaskExample{
		{
			Name: "Allowing the herokuish builder for an app",
			BuilderHerokuishPropertyTask: BuilderHerokuishPropertyTask{
				App:      "node-js-app",
				Property: "allowed",
				Value:    "true",
			},
		},
		{
			Name: "Allowing the herokuish builder globally",
			BuilderHerokuishPropertyTask: BuilderHerokuishPropertyTask{
				Global:   true,
				Property: "allowed",
				Value:    "true",
			},
		},
		{
			Name: "Clearing the allowed property for an app",
			BuilderHerokuishPropertyTask: BuilderHerokuishPropertyTask{
				App:      "node-js-app",
				Property: "allowed",
			},
		},
	})
}

// Execute sets or unsets the builder-herokuish property
func (t BuilderHerokuishPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// builderHerokuishPropertyKeys maps builder-herokuish property names to the
// JSON keys emitted by `dokku builder-herokuish:report --format json` on
// dokku 0.38.8+.
var builderHerokuishPropertyKeys = map[string]PropertyKeys{
	"allowed": {PerApp: "allowed", Global: "global-allowed"},
}

// Plan reports the drift the BuilderHerokuishPropertyTask would produce.
func (t BuilderHerokuishPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "builder-herokuish:set", builderHerokuishPropertyKeys)
}

// init registers the BuilderHerokuishPropertyTask with the task registry
func init() {
	RegisterTask(&BuilderHerokuishPropertyTask{})
}
