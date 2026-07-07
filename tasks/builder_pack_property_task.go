package tasks

// BuilderPackPropertyTask manages the builder-pack configuration for a given dokku application
type BuilderPackPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the builder-pack configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the builder-pack configuration should be applied globally"`

	// Property is the name of the builder-pack property to set
	Property string `required:"true" yaml:"property" description:"Name of the builder-pack property to set"`

	// Value is the value to set for the builder-pack property
	Value string `required:"false" yaml:"value,omitempty" description:"Value to set for the builder-pack property"`

	// State is the desired state of the builder-pack configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the builder-pack configuration"`
}

// BuilderPackPropertyTaskExample contains an example of a BuilderPackPropertyTask
type BuilderPackPropertyTaskExample struct {
	// Name is the task name holding the BuilderPackPropertyTask description
	Name string `yaml:"-"`

	// BuilderPackPropertyTask is the BuilderPackPropertyTask configuration
	BuilderPackPropertyTask BuilderPackPropertyTask `yaml:"dokku_builder_pack_property"`
}

// GetName returns the name of the example
func (e BuilderPackPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the builder-pack property task
func (t BuilderPackPropertyTask) Doc() string {
	return "Manages the builder-pack configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t BuilderPackPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Examples returns the examples for the builder-pack property task
func (t BuilderPackPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]BuilderPackPropertyTaskExample{
		{
			Name: "Setting the project.toml path for an app",
			BuilderPackPropertyTask: BuilderPackPropertyTask{
				App:      "node-js-app",
				Property: "projecttoml-path",
				Value:    "config/project.toml",
			},
		},
		{
			Name: "Setting the project.toml path globally",
			BuilderPackPropertyTask: BuilderPackPropertyTask{
				Global:   true,
				Property: "projecttoml-path",
				Value:    "project.toml",
			},
		},
		{
			Name: "Clearing the project.toml path for an app",
			BuilderPackPropertyTask: BuilderPackPropertyTask{
				App:      "node-js-app",
				Property: "projecttoml-path",
			},
		},
	})
}

// Execute sets or unsets the builder-pack property
func (t BuilderPackPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// builderPackPropertyKeys maps builder-pack property names to the JSON keys
// emitted by `dokku builder-pack:report --format json` on dokku 0.38.8+.
var builderPackPropertyKeys = map[string]PropertyKeys{
	"projecttoml-path": {PerApp: "projecttoml-path", Global: "global-projecttoml-path"},
}

// Plan reports the drift the BuilderPackPropertyTask would produce.
func (t BuilderPackPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "builder-pack:set", builderPackPropertyKeys)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t BuilderPackPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(app, "builder-pack:set", builderPackPropertyKeys, func(app, property, value string) interface{} {
		return BuilderPackPropertyTask{App: app, Property: property, Value: value}
	})
}

// init registers the BuilderPackPropertyTask with the task registry
func init() {
	RegisterTask(&BuilderPackPropertyTask{})
}
