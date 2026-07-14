package tasks

// AppsPropertyTask manages the apps plugin configuration for a given dokku application or globally
type AppsPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the apps property should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the apps property should be applied globally"`

	// Property is the name of the apps property to set
	Property string `required:"true" yaml:"property" description:"Name of the apps property to set"`

	// Value is the value to set for the apps property
	Value string `required:"false" yaml:"value,omitempty" description:"Value to set for the apps property"`

	// State is the desired state of the apps configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the apps configuration"`
}

// AppsPropertyTaskExample contains an example of an AppsPropertyTask
type AppsPropertyTaskExample struct {
	// Name is the task name holding the AppsPropertyTask description
	Name string `yaml:"-"`

	// AppsPropertyTask is the AppsPropertyTask configuration
	AppsPropertyTask AppsPropertyTask `yaml:"dokku_apps_property"`
}

// GetName returns the name of the example
func (e AppsPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the apps property task
func (t AppsPropertyTask) Doc() string {
	return "Manages the apps plugin configuration for a given dokku application or globally"
}

// ExportSupport reports how docket export handles this task.
func (t AppsPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Examples returns the examples for the apps property task
func (t AppsPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]AppsPropertyTaskExample{
		{
			Name: "Disabling app auto-creation globally",
			AppsPropertyTask: AppsPropertyTask{
				Global:   true,
				Property: "disable-autocreation",
				Value:    "true",
			},
		},
		{
			Name: "Overriding the deploy-source for an app",
			AppsPropertyTask: AppsPropertyTask{
				App:      "node-js-app",
				Property: "deploy-source",
				Value:    "git",
			},
		},
		{
			Name: "Overriding the deploy-source-metadata for an app",
			AppsPropertyTask: AppsPropertyTask{
				App:      "node-js-app",
				Property: "deploy-source-metadata",
				Value:    "https://example.com/repo",
			},
		},
		{
			Name: "Re-enabling app auto-creation globally",
			AppsPropertyTask: AppsPropertyTask{
				Global:   true,
				Property: "disable-autocreation",
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the apps property
func (t AppsPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// appsPropertyKeys maps apps property names to the JSON keys emitted by
// `dokku apps:report [<app>|--global] --format json` on dokku 0.38.12+.
// deploy-source and deploy-source-metadata are per-app only; disable-autocreation
// is global only - dokku 0.38.12 narrowed apps.GlobalProperties to drop the
// vestigial deploy-source* global forms, and maybeCreateApp only consults the
// global value of disable-autocreation. The bare key `global-disable-autocreation`
// falls out of stripping the `--app-` prefix from dokku's
// `--app-global-disable-autocreation` report flag.
var appsPropertyKeys = map[string]PropertyKeys{
	"deploy-source":          {PerApp: "deploy-source", Global: ""},
	"deploy-source-metadata": {PerApp: "deploy-source-metadata", Global: ""},
	"disable-autocreation":   {PerApp: "", Global: "global-disable-autocreation"},
}

// Validate checks the AppsPropertyTask's inputs without contacting the server.
func (t AppsPropertyTask) Validate() error {
	return validatePropertyInput(t.State, t.App, t.Global, t.Property, t.Value, "apps:set", appsPropertyKeys)
}

// Plan reports the drift the AppsPropertyTask would produce.
func (t AppsPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "apps:set", appsPropertyKeys)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t AppsPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(app, "apps:set", appsPropertyKeys, func(app, property, value string) interface{} {
		return AppsPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t AppsPropertyTask) ExportGlobal() ([]interface{}, error) {
	return exportGlobalProperties("apps:set", appsPropertyKeys, func(property, value string) interface{} {
		return AppsPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the AppsPropertyTask with the task registry
func init() {
	RegisterTask(&AppsPropertyTask{})
}
