package tasks

// BuildsPropertyTask manages the builds configuration for a given dokku application
type BuildsPropertyTask PropertyFields

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

// ProbeSupport reports whether Plan() can read this task's current state.
func (t BuildsPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
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
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the builds property
func (t BuildsPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// buildsPropertyTable maps builds property names to the JSON keys emitted by
// `dokku builds:report --format json` on dokku 0.38.8+.
var buildsPropertyTable = PropertyTable{
	Subcommand: "builds:set",
	Keys: map[string]PropertyKeys{
		"retention": {PerApp: "retention", Global: "global-retention"},
	},
}

// PropertyTable returns the property schema this task manages.
func (t BuildsPropertyTask) PropertyTable() PropertyTable {
	return buildsPropertyTable
}

// Validate checks the BuildsPropertyTask's inputs without contacting the server.
func (t BuildsPropertyTask) Validate() error {
	return validatePropertyInput(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// Plan reports the drift the BuildsPropertyTask would produce.
func (t BuildsPropertyTask) Plan() PlanResult {
	return planProperty(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t BuildsPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(t, app, func(app, property, value string) interface{} {
		return BuildsPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t BuildsPropertyTask) ExportGlobal() ([]interface{}, error) {
	return exportGlobalProperties(t, func(property, value string) interface{} {
		return BuildsPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the BuildsPropertyTask with the task registry
func init() {
	RegisterTask(&BuildsPropertyTask{})
}
