package tasks

import "context"

// BuilderTask manages the builder configuration for a given dokku application
type BuilderPropertyTask PropertyFields

// BuilderPropertyTaskExample contains an example of a BuilderPropertyTask
type BuilderPropertyTaskExample struct {
	// Name is the task name holding the BuilderPropertyTask description
	Name string `yaml:"-"`

	// BuilderPropertyTask is the BuilderPropertyTask configuration
	BuilderPropertyTask BuilderPropertyTask `yaml:"dokku_builder_property"`
}

// GetName returns the name of the example
func (e BuilderPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the builder property task
func (t BuilderPropertyTask) Doc() string {
	return "Manages the builder configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t BuilderPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t BuilderPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// Examples returns the examples for the builder property task
func (t BuilderPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]BuilderPropertyTaskExample{
		{
			Name: "Overriding the auto-selected builder",
			BuilderPropertyTask: BuilderPropertyTask{
				App:      "node-js-app",
				Property: "selected",
				Value:    "dockerfile",
			},
		},
		{
			Name: "Setting the builder to the default value",
			BuilderPropertyTask: BuilderPropertyTask{
				App:      "node-js-app",
				Property: "selected",
				State:    StateAbsent,
			},
		},
		{
			Name: "Changing the build build directory",
			BuilderPropertyTask: BuilderPropertyTask{
				App:      "monorepo",
				Property: "build-dir",
				Value:    "backend",
			},
		},
		{
			Name: "Overriding the auto-selected builder globally",
			BuilderPropertyTask: BuilderPropertyTask{
				Global:   true,
				Property: "selected",
				Value:    "herokuish",
			},
		},
	})
}

// Execute executes the builder configuration task
func (t BuilderPropertyTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// builderPropertyTable maps builder property names to the JSON keys emitted
// by `dokku builder:report --format json` on dokku 0.38.8+.
var builderPropertyTable = PropertyTable{
	Subcommand: "builder:set",
	Keys: map[string]PropertyKeys{
		"build-dir":    {PerApp: "build-dir", Global: "global-build-dir"},
		"selected":     {PerApp: "selected", Global: "global-selected"},
		"skip-cleanup": {PerApp: "skip-cleanup", Global: "global-skip-cleanup"},
	},
}

// PropertyTable returns the property schema this task manages.
func (t BuilderPropertyTask) PropertyTable() PropertyTable {
	return builderPropertyTable
}

// Validate checks the BuilderPropertyTask's inputs without contacting the server.
func (t BuilderPropertyTask) Validate() error {
	return validatePropertyInput(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// Plan reports the drift the BuilderPropertyTask would produce.
func (t BuilderPropertyTask) Plan(ctx context.Context) PlanResult {
	return planProperty(ctx, t, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t BuilderPropertyTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	return exportProperties(ctx, t, app, func(app, property, value string) interface{} {
		return BuilderPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t BuilderPropertyTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	return exportGlobalProperties(ctx, t, func(property, value string) interface{} {
		return BuilderPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the BuilderTask with the task registry
func init() {
	RegisterTask(&BuilderPropertyTask{})
}
