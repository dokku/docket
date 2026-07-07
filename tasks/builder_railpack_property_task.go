package tasks

// BuilderRailpackPropertyTask manages the builder-railpack configuration for a given dokku application
type BuilderRailpackPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the builder-railpack configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the builder-railpack configuration should be applied globally"`

	// Property is the name of the builder-railpack property to set
	Property string `required:"true" yaml:"property" description:"Name of the builder-railpack property to set"`

	// Value is the value to set for the builder-railpack property
	Value string `required:"false" yaml:"value,omitempty" description:"Value to set for the builder-railpack property"`

	// State is the desired state of the builder-railpack configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the builder-railpack configuration"`
}

// BuilderRailpackPropertyTaskExample contains an example of a BuilderRailpackPropertyTask
type BuilderRailpackPropertyTaskExample struct {
	// Name is the task name holding the BuilderRailpackPropertyTask description
	Name string `yaml:"-"`

	// BuilderRailpackPropertyTask is the BuilderRailpackPropertyTask configuration
	BuilderRailpackPropertyTask BuilderRailpackPropertyTask `yaml:"dokku_builder_railpack_property"`
}

// GetName returns the name of the example
func (e BuilderRailpackPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the builder-railpack property task
func (t BuilderRailpackPropertyTask) Doc() string {
	return "Manages the builder-railpack configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t BuilderRailpackPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Examples returns the examples for the builder-railpack property task
func (t BuilderRailpackPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]BuilderRailpackPropertyTaskExample{
		{
			Name: "Setting the railpack.json path for an app",
			BuilderRailpackPropertyTask: BuilderRailpackPropertyTask{
				App:      "node-js-app",
				Property: "railpackjson-path",
				Value:    "config/railpack.json",
			},
		},
		{
			Name: "Setting the railpack.json path globally",
			BuilderRailpackPropertyTask: BuilderRailpackPropertyTask{
				Global:   true,
				Property: "railpackjson-path",
				Value:    "railpack.json",
			},
		},
		{
			Name: "Clearing the railpack.json path for an app",
			BuilderRailpackPropertyTask: BuilderRailpackPropertyTask{
				App:      "node-js-app",
				Property: "railpackjson-path",
			},
		},
	})
}

// Execute sets or unsets the builder-railpack property
func (t BuilderRailpackPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// builderRailpackPropertyKeys maps builder-railpack property names to the
// JSON keys emitted by `dokku builder-railpack:report --format json` on
// dokku 0.38.8+.
var builderRailpackPropertyKeys = map[string]PropertyKeys{
	"railpackjson-path": {PerApp: "railpackjson-path", Global: "global-railpackjson-path"},
}

// Plan reports the drift the BuilderRailpackPropertyTask would produce.
func (t BuilderRailpackPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "builder-railpack:set", builderRailpackPropertyKeys)
}

// init registers the BuilderRailpackPropertyTask with the task registry
func init() {
	RegisterTask(&BuilderRailpackPropertyTask{})
}
