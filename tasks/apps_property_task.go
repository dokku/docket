package tasks

import "context"

// AppsPropertyTask manages the apps plugin configuration for a given dokku application or globally
type AppsPropertyTask PropertyFields

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

// ProbeSupport reports whether Plan() can read this task's current state.
func (t AppsPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
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
func (t AppsPropertyTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// appsPropertyTable maps apps property names to the JSON keys emitted by
// `dokku apps:report [<app>|--global] --format json` on dokku 0.38.12+.
// deploy-source and deploy-source-metadata are per-app only; disable-autocreation
// is global only - dokku 0.38.12 narrowed apps.GlobalProperties to drop the
// vestigial deploy-source* global forms, and maybeCreateApp only consults the
// global value of disable-autocreation. The bare key `global-disable-autocreation`
// falls out of stripping the `--app-` prefix from dokku's
// `--app-global-disable-autocreation` report flag.
var appsPropertyTable = PropertyTable{
	Subcommand: "apps:set",
	Keys: map[string]PropertyKeys{
		"deploy-source":          {PerApp: "deploy-source", Global: ""},
		"deploy-source-metadata": {PerApp: "deploy-source-metadata", Global: ""},
		"disable-autocreation":   {PerApp: "", Global: "global-disable-autocreation"},
	},
}

// PropertyTable returns the property schema this task manages.
func (t AppsPropertyTask) PropertyTable() PropertyTable {
	return appsPropertyTable
}

// Validate checks the AppsPropertyTask's inputs without contacting the server.
func (t AppsPropertyTask) Validate() error {
	return validatePropertyInput(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// Plan reports the drift the AppsPropertyTask would produce.
func (t AppsPropertyTask) Plan(ctx context.Context) PlanResult {
	return planProperty(ctx, t, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t AppsPropertyTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	return exportProperties(ctx, t, app, func(app, property, value string) interface{} {
		return AppsPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t AppsPropertyTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	return exportGlobalProperties(ctx, t, func(property, value string) interface{} {
		return AppsPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the AppsPropertyTask with the task registry
func init() {
	RegisterTask(&AppsPropertyTask{})
}
