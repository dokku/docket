package tasks

// ChecksPropertyTask manages the checks configuration for a given dokku application
type ChecksPropertyTask PropertyFields

// ChecksPropertyTaskExample contains an example of a ChecksPropertyTask
type ChecksPropertyTaskExample struct {
	// Name is the task name holding the ChecksPropertyTask description
	Name string `yaml:"-"`

	// ChecksPropertyTask is the ChecksPropertyTask configuration
	ChecksPropertyTask ChecksPropertyTask `yaml:"dokku_checks_property"`
}

// GetName returns the name of the example
func (e ChecksPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the checks property task
func (t ChecksPropertyTask) Doc() string {
	return "Manages the checks configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t ChecksPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t ChecksPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// Examples returns the examples for the checks property task
func (t ChecksPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]ChecksPropertyTaskExample{
		{
			Name: "Setting the wait-to-retire value for an app",
			ChecksPropertyTask: ChecksPropertyTask{
				App:      "node-js-app",
				Property: "wait-to-retire",
				Value:    "60",
			},
		},
		{
			Name: "Setting the wait-to-retire value globally",
			ChecksPropertyTask: ChecksPropertyTask{
				Global:   true,
				Property: "wait-to-retire",
				Value:    "60",
			},
		},
		{
			Name: "Clearing the wait-to-retire value for an app",
			ChecksPropertyTask: ChecksPropertyTask{
				App:      "node-js-app",
				Property: "wait-to-retire",
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the checks property
func (t ChecksPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// checksPropertyTable maps checks property names to the JSON keys emitted by
// `dokku checks:report --format json` on dokku 0.38.8+.
var checksPropertyTable = PropertyTable{
	Subcommand: "checks:set",
	Keys: map[string]PropertyKeys{
		"wait-to-retire": {PerApp: "wait-to-retire", Global: "global-wait-to-retire"},
	},
}

// PropertyTable returns the property schema this task manages.
func (t ChecksPropertyTask) PropertyTable() PropertyTable {
	return checksPropertyTable
}

// Validate checks the ChecksPropertyTask's inputs without contacting the server.
func (t ChecksPropertyTask) Validate() error {
	return validatePropertyInput(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// Plan reports the drift the ChecksPropertyTask would produce.
func (t ChecksPropertyTask) Plan() PlanResult {
	return planProperty(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t ChecksPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(t, app, func(app, property, value string) interface{} {
		return ChecksPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t ChecksPropertyTask) ExportGlobal() ([]interface{}, error) {
	return exportGlobalProperties(t, func(property, value string) interface{} {
		return ChecksPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the ChecksPropertyTask with the task registry
func init() {
	RegisterTask(&ChecksPropertyTask{})
}
