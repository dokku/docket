package tasks

// AppJsonPropertyTask manages the app.json configuration for a given dokku application
type AppJsonPropertyTask PropertyFields

// AppJsonPropertyTaskExample contains an example of an AppJsonPropertyTask
type AppJsonPropertyTaskExample struct {
	// Name is the task name holding the AppJsonPropertyTask description
	Name string `yaml:"-"`

	// AppJsonPropertyTask is the AppJsonPropertyTask configuration
	AppJsonPropertyTask AppJsonPropertyTask `yaml:"dokku_app_json_property"`
}

// GetName returns the name of the example
func (e AppJsonPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the app.json property task
func (t AppJsonPropertyTask) Doc() string {
	return "Manages the app.json configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t AppJsonPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t AppJsonPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// Examples returns the examples for the app.json property task
func (t AppJsonPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]AppJsonPropertyTaskExample{
		{
			Name: "Setting the appjson-path for an app",
			AppJsonPropertyTask: AppJsonPropertyTask{
				App:      "node-js-app",
				Property: "appjson-path",
				Value:    "app.json",
			},
		},
		{
			Name: "Setting the appjson-path globally",
			AppJsonPropertyTask: AppJsonPropertyTask{
				Global:   true,
				Property: "appjson-path",
				Value:    "app.json",
			},
		},
		{
			Name: "Clearing the appjson-path for an app",
			AppJsonPropertyTask: AppJsonPropertyTask{
				App:      "node-js-app",
				Property: "appjson-path",
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the app.json property
func (t AppJsonPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// appJsonPropertyTable maps app-json property names to the JSON keys emitted
// by `dokku app-json:report --format json` on dokku 0.38.8+.
var appJsonPropertyTable = PropertyTable{
	Subcommand: "app-json:set",
	Keys: map[string]PropertyKeys{
		"appjson-path": {PerApp: "appjson-path", Global: "global-appjson-path"},
	},
}

// PropertyTable returns the property schema this task manages.
func (t AppJsonPropertyTask) PropertyTable() PropertyTable {
	return appJsonPropertyTable
}

// Validate checks the AppJsonPropertyTask's inputs without contacting the server.
func (t AppJsonPropertyTask) Validate() error {
	return validatePropertyInput(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// Plan reports the drift the AppJsonPropertyTask would produce.
func (t AppJsonPropertyTask) Plan() PlanResult {
	return planProperty(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t AppJsonPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(t, app, func(app, property, value string) interface{} {
		return AppJsonPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t AppJsonPropertyTask) ExportGlobal() ([]interface{}, error) {
	return exportGlobalProperties(t, func(property, value string) interface{} {
		return AppJsonPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the AppJsonPropertyTask with the task registry
func init() {
	RegisterTask(&AppJsonPropertyTask{})
}
