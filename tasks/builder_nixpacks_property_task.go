package tasks

// BuilderNixpacksPropertyTask manages the builder-nixpacks configuration for a given dokku application
type BuilderNixpacksPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app"`

	// Global is a flag indicating if the builder-nixpacks configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty"`

	// Property is the name of the builder-nixpacks property to set
	Property string `required:"true" yaml:"property"`

	// Value is the value to set for the builder-nixpacks property
	Value string `required:"false" yaml:"value,omitempty"`

	// State is the desired state of the builder-nixpacks configuration
	State State `required:"true" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// BuilderNixpacksPropertyTaskExample contains an example of a BuilderNixpacksPropertyTask
type BuilderNixpacksPropertyTaskExample struct {
	// Name is the task name holding the BuilderNixpacksPropertyTask description
	Name string `yaml:"-"`

	// BuilderNixpacksPropertyTask is the BuilderNixpacksPropertyTask configuration
	BuilderNixpacksPropertyTask BuilderNixpacksPropertyTask `yaml:"dokku_builder_nixpacks_property"`
}

// GetName returns the name of the example
func (e BuilderNixpacksPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the builder-nixpacks property task
func (t BuilderNixpacksPropertyTask) Doc() string {
	return "Manages the builder-nixpacks configuration for a given dokku application"
}

// Examples returns the examples for the builder-nixpacks property task
func (t BuilderNixpacksPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]BuilderNixpacksPropertyTaskExample{
		{
			Name: "Setting the nixpacks.toml path for an app",
			BuilderNixpacksPropertyTask: BuilderNixpacksPropertyTask{
				App:      "node-js-app",
				Property: "nixpackstoml-path",
				Value:    "config/nixpacks.toml",
			},
		},
		{
			Name: "Setting the nixpacks.toml path globally",
			BuilderNixpacksPropertyTask: BuilderNixpacksPropertyTask{
				Global:   true,
				Property: "nixpackstoml-path",
				Value:    "nixpacks.toml",
			},
		},
		{
			Name: "Clearing the nixpacks.toml path for an app",
			BuilderNixpacksPropertyTask: BuilderNixpacksPropertyTask{
				App:      "node-js-app",
				Property: "nixpackstoml-path",
			},
		},
	})
}

// Execute sets or unsets the builder-nixpacks property
func (t BuilderNixpacksPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// builderNixpacksPropertyKeys maps builder-nixpacks property names to the
// JSON keys emitted by `dokku builder-nixpacks:report --format json` on
// dokku 0.38.8+.
var builderNixpacksPropertyKeys = map[string]PropertyKeys{
	"nixpackstoml-path": {PerApp: "nixpackstoml-path", Global: "global-nixpackstoml-path"},
}

// Plan reports the drift the BuilderNixpacksPropertyTask would produce.
func (t BuilderNixpacksPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "builder-nixpacks:set", builderNixpacksPropertyKeys)
}

// init registers the BuilderNixpacksPropertyTask with the task registry
func init() {
	RegisterTask(&BuilderNixpacksPropertyTask{})
}
