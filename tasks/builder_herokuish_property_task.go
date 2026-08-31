package tasks

import "context"

// BuilderHerokuishPropertyTask manages the builder-herokuish configuration for a given dokku application
type BuilderHerokuishPropertyTask PropertyFields

// BuilderHerokuishPropertyTaskExample contains an example of a BuilderHerokuishPropertyTask
type BuilderHerokuishPropertyTaskExample struct {
	// Name is the task name holding the BuilderHerokuishPropertyTask description
	Name string `yaml:"-"`

	// BuilderHerokuishPropertyTask is the BuilderHerokuishPropertyTask configuration
	BuilderHerokuishPropertyTask BuilderHerokuishPropertyTask `yaml:"dokku_builder_herokuish_property"`
}

// GetName returns the name of the example
func (e BuilderHerokuishPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the builder-herokuish property task
func (t BuilderHerokuishPropertyTask) Doc() string {
	return "Manages the builder-herokuish configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t BuilderHerokuishPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t BuilderHerokuishPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// Examples returns the examples for the builder-herokuish property task
func (t BuilderHerokuishPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]BuilderHerokuishPropertyTaskExample{
		{
			Name: "Allowing the herokuish builder for an app",
			BuilderHerokuishPropertyTask: BuilderHerokuishPropertyTask{
				App:      "node-js-app",
				Property: "allowed",
				Value:    "true",
			},
		},
		{
			Name: "Allowing the herokuish builder globally",
			BuilderHerokuishPropertyTask: BuilderHerokuishPropertyTask{
				Global:   true,
				Property: "allowed",
				Value:    "true",
			},
		},
		{
			Name: "Clearing the allowed property for an app",
			BuilderHerokuishPropertyTask: BuilderHerokuishPropertyTask{
				App:      "node-js-app",
				Property: "allowed",
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the builder-herokuish property
func (t BuilderHerokuishPropertyTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// builderHerokuishPropertyTable maps builder-herokuish property names to the
// JSON keys emitted by `dokku builder-herokuish:report --format json` on
// dokku 0.38.8+.
var builderHerokuishPropertyTable = PropertyTable{
	Subcommand: "builder-herokuish:set",
	Keys: map[string]PropertyKeys{
		"allowed": {PerApp: "allowed", Global: "global-allowed"},
	},
}

// PropertyTable returns the property schema this task manages.
func (t BuilderHerokuishPropertyTask) PropertyTable() PropertyTable {
	return builderHerokuishPropertyTable
}

// Validate checks the BuilderHerokuishPropertyTask's inputs without contacting the server.
func (t BuilderHerokuishPropertyTask) Validate() error {
	return validatePropertyInput(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// Plan reports the drift the BuilderHerokuishPropertyTask would produce.
func (t BuilderHerokuishPropertyTask) Plan(ctx context.Context) PlanResult {
	return planProperty(ctx, t, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t BuilderHerokuishPropertyTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	return exportProperties(ctx, t, app, func(app, property, value string) interface{} {
		return BuilderHerokuishPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t BuilderHerokuishPropertyTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	return exportGlobalProperties(ctx, t, func(property, value string) interface{} {
		return BuilderHerokuishPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the BuilderHerokuishPropertyTask with the task registry
func init() {
	RegisterTask(&BuilderHerokuishPropertyTask{})
}
