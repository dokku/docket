package tasks

// BuilderDockerfilePropertyTask manages the builder-dockerfile configuration for a given dokku application
type BuilderDockerfilePropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the builder-dockerfile configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the builder-dockerfile configuration should be applied globally"`

	// Property is the name of the builder-dockerfile property to set
	Property string `required:"true" yaml:"property" description:"Name of the builder-dockerfile property to set"`

	// Value is the value to set for the builder-dockerfile property
	Value string `required:"false" yaml:"value,omitempty" description:"Value to set for the builder-dockerfile property"`

	// State is the desired state of the builder-dockerfile configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the builder-dockerfile configuration"`
}

// BuilderDockerfilePropertyTaskExample contains an example of a BuilderDockerfilePropertyTask
type BuilderDockerfilePropertyTaskExample struct {
	// Name is the task name holding the BuilderDockerfilePropertyTask description
	Name string `yaml:"-"`

	// BuilderDockerfilePropertyTask is the BuilderDockerfilePropertyTask configuration
	BuilderDockerfilePropertyTask BuilderDockerfilePropertyTask `yaml:"dokku_builder_dockerfile_property"`
}

// GetName returns the name of the example
func (e BuilderDockerfilePropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the builder-dockerfile property task
func (t BuilderDockerfilePropertyTask) Doc() string {
	return "Manages the builder-dockerfile configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t BuilderDockerfilePropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Examples returns the examples for the builder-dockerfile property task
func (t BuilderDockerfilePropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]BuilderDockerfilePropertyTaskExample{
		{
			Name: "Setting the dockerfile path for an app",
			BuilderDockerfilePropertyTask: BuilderDockerfilePropertyTask{
				App:      "node-js-app",
				Property: "dockerfile-path",
				Value:    "Dockerfile.production",
			},
		},
		{
			Name: "Setting the dockerfile path globally",
			BuilderDockerfilePropertyTask: BuilderDockerfilePropertyTask{
				Global:   true,
				Property: "dockerfile-path",
				Value:    "Dockerfile",
			},
		},
		{
			Name: "Clearing the dockerfile path for an app",
			BuilderDockerfilePropertyTask: BuilderDockerfilePropertyTask{
				App:      "node-js-app",
				Property: "dockerfile-path",
			},
		},
	})
}

// Execute sets or unsets the builder-dockerfile property
func (t BuilderDockerfilePropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// builderDockerfilePropertyKeys maps builder-dockerfile property names to
// the JSON keys emitted by `dokku builder-dockerfile:report --format json`
// on dokku 0.38.8+.
var builderDockerfilePropertyKeys = map[string]PropertyKeys{
	"dockerfile-path": {PerApp: "dockerfile-path", Global: "global-dockerfile-path"},
}

// Plan reports the drift the BuilderDockerfilePropertyTask would produce.
func (t BuilderDockerfilePropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "builder-dockerfile:set", builderDockerfilePropertyKeys)
}

// init registers the BuilderDockerfilePropertyTask with the task registry
func init() {
	RegisterTask(&BuilderDockerfilePropertyTask{})
}
