package tasks

import "context"

// BuilderRailpackPropertyTask manages the builder-railpack configuration for a given dokku application
type BuilderRailpackPropertyTask PropertyFields

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

// ProbeSupport reports whether Plan() can read this task's current state.
func (t BuilderRailpackPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
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
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the builder-railpack property
func (t BuilderRailpackPropertyTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// builderRailpackPropertyTable maps builder-railpack property names to the
// JSON keys emitted by `dokku builder-railpack:report --format json` on
// dokku 0.38.8+.
var builderRailpackPropertyTable = PropertyTable{
	Subcommand: "builder-railpack:set",
	Keys: map[string]PropertyKeys{
		"railpackjson-path": {PerApp: "railpackjson-path", Global: "global-railpackjson-path"},
	},
}

// PropertyTable returns the property schema this task manages.
func (t BuilderRailpackPropertyTask) PropertyTable() PropertyTable {
	return builderRailpackPropertyTable
}

// Validate checks the BuilderRailpackPropertyTask's inputs without contacting the server.
func (t BuilderRailpackPropertyTask) Validate() error {
	return validatePropertyInput(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// Plan reports the drift the BuilderRailpackPropertyTask would produce.
func (t BuilderRailpackPropertyTask) Plan(ctx context.Context) PlanResult {
	return planProperty(ctx, t, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t BuilderRailpackPropertyTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	return exportProperties(ctx, t, app, func(app, property, value string) interface{} {
		return BuilderRailpackPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t BuilderRailpackPropertyTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	return exportGlobalProperties(ctx, t, func(property, value string) interface{} {
		return BuilderRailpackPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the BuilderRailpackPropertyTask with the task registry
func init() {
	RegisterTask(&BuilderRailpackPropertyTask{})
}
