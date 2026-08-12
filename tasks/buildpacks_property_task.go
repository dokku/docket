package tasks

// BuildpacksPropertyTask manages the buildpacks configuration for a given dokku application
type BuildpacksPropertyTask PropertyFields

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

// ExportSupport reports how docket export handles this task.
func (t BuildpacksPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t BuildpacksPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
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
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the buildpacks property
func (t BuildpacksPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// buildpacksPropertyTable maps buildpacks property names to the JSON keys
// emitted by `dokku buildpacks:report --format json` on dokku 0.38.8+.
// The buildpacks list (set via `buildpacks:set <app> <buildpack>`) is not a
// property in the typed-task sense and is not modeled here.
var buildpacksPropertyTable = PropertyTable{
	Subcommand: "buildpacks:set-property",
	Keys: map[string]PropertyKeys{
		"stack": {PerApp: "stack", Global: "global-stack"},
	},
}

// PropertyTable returns the property schema this task manages.
func (t BuildpacksPropertyTask) PropertyTable() PropertyTable {
	return buildpacksPropertyTable
}

// Validate checks the BuildpacksPropertyTask's inputs without contacting the server.
func (t BuildpacksPropertyTask) Validate() error {
	return validatePropertyInput(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// Plan reports the drift the BuildpacksPropertyTask would produce.
func (t BuildpacksPropertyTask) Plan() PlanResult {
	return planProperty(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t BuildpacksPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(t, app, func(app, property, value string) interface{} {
		return BuildpacksPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t BuildpacksPropertyTask) ExportGlobal() ([]interface{}, error) {
	return exportGlobalProperties(t, func(property, value string) interface{} {
		return BuildpacksPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the BuildpacksPropertyTask with the task registry
func init() {
	RegisterTask(&BuildpacksPropertyTask{})
}
