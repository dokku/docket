package tasks

// BuilderTask manages the builder configuration for a given dokku application
type BuilderPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the builder configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the builder configuration should be applied globally"`

	// Property is the name of the builder property to set
	Property string `required:"true" yaml:"property" description:"Name of the builder property to set"`

	// Value is the value to set for the builder property
	Value string `required:"false" yaml:"value,omitempty" description:"Value to set for the builder property"`

	// State is the desired state of the builder configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the builder configuration"`
}

// BuilderPropertyTaskExample contains an example of a BuilderPropertyTask
type BuilderPropertyTaskExample struct {
	// Name is the task name holding the BuilderPropertyTask description
	Name string `yaml:"-"`

	// BuilderPropertyTask is the BuilderPropertyTask configuration
	BuilderPropertyTask BuilderPropertyTask `yaml:"dokku_builder_property"`
}

// GetName returns the name of the example
func (e BuilderPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the builder property task
func (t BuilderPropertyTask) Doc() string {
	return "Manages the builder configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t BuilderPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Examples returns the examples for the builder property task
func (t BuilderPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]BuilderPropertyTaskExample{
		{
			Name: "Overriding the auto-selected builder",
			BuilderPropertyTask: BuilderPropertyTask{
				App:      "node-js-app",
				Property: "selected",
				Value:    "dockerfile",
			},
		},
		{
			Name: "Setting the builder to the default value",
			BuilderPropertyTask: BuilderPropertyTask{
				App:      "node-js-app",
				Property: "selected",
			},
		},
		{
			Name: "Changing the build build directory",
			BuilderPropertyTask: BuilderPropertyTask{
				App:      "monorepo",
				Property: "build-dir",
				Value:    "backend",
			},
		},
		{
			Name: "Overriding the auto-selected builder globally",
			BuilderPropertyTask: BuilderPropertyTask{
				Global:   true,
				Property: "selected",
				Value:    "herokuish",
			},
		},
	})
}

// Execute executes the builder configuration task
func (t BuilderPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// builderPropertyKeys maps builder property names to the JSON keys emitted
// by `dokku builder:report --format json` on dokku 0.38.8+.
var builderPropertyKeys = map[string]PropertyKeys{
	"build-dir":    {PerApp: "build-dir", Global: "global-build-dir"},
	"selected":     {PerApp: "selected", Global: "global-selected"},
	"skip-cleanup": {PerApp: "skip-cleanup", Global: "global-skip-cleanup"},
}

// Plan reports the drift the BuilderPropertyTask would produce.
func (t BuilderPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "builder:set", builderPropertyKeys)
}

// init registers the BuilderTask with the task registry
func init() {
	RegisterTask(&BuilderPropertyTask{})
}
