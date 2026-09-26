package tasks

import "context"

// BuilderNixpacksPropertyTask manages the builder-nixpacks configuration for a given dokku application
type BuilderNixpacksPropertyTask PropertyFields

// BuilderNixpacksPropertyTaskExample contains an example of a BuilderNixpacksPropertyTask
type BuilderNixpacksPropertyTaskExample struct {
	// Name is the task name holding the BuilderNixpacksPropertyTask description
	Name string `yaml:"-"`

	// BuilderNixpacksPropertyTask is the BuilderNixpacksPropertyTask configuration
	BuilderNixpacksPropertyTask BuilderNixpacksPropertyTask `yaml:"dokku_builder_nixpacks_property"`
}

// GetName returns the name of the example
func (e BuilderNixpacksPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the builder-nixpacks property task
func (t BuilderNixpacksPropertyTask) Doc() string {
	return "Manages the builder-nixpacks configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t BuilderNixpacksPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t BuilderNixpacksPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// Examples returns the examples for the builder-nixpacks property task
func (t BuilderNixpacksPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]BuilderNixpacksPropertyTaskExample{
		{
			Name: "Setting the nixpacks.toml path for an app",
			BuilderNixpacksPropertyTask: BuilderNixpacksPropertyTask{
				App:      "node-js-app",
				Property: "nixpackstoml-path",
				Value:    "config/nixpacks.toml",
			},
		},
		{
			Name: "Setting the nixpacks.toml path globally",
			BuilderNixpacksPropertyTask: BuilderNixpacksPropertyTask{
				Global:   true,
				Property: "nixpackstoml-path",
				Value:    "nixpacks.toml",
			},
		},
		{
			Name: "Clearing the nixpacks.toml path for an app",
			BuilderNixpacksPropertyTask: BuilderNixpacksPropertyTask{
				App:      "node-js-app",
				Property: "nixpackstoml-path",
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the builder-nixpacks property
func (t BuilderNixpacksPropertyTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// builderNixpacksPropertyTable maps builder-nixpacks property names to the
// JSON keys emitted by `dokku builder-nixpacks:report --format json` on
// dokku 0.38.8+.
var builderNixpacksPropertyTable = PropertyTable{
	Subcommand: "builder-nixpacks:set",
	Keys: map[string]PropertyKeys{
		"nixpackstoml-path": {PerApp: "nixpackstoml-path", Global: "global-nixpackstoml-path"},
	},
}

// PropertyTable returns the property schema this task manages.
func (t BuilderNixpacksPropertyTask) PropertyTable() PropertyTable {
	return builderNixpacksPropertyTable
}

// Validate checks the BuilderNixpacksPropertyTask's inputs without contacting the server.
func (t BuilderNixpacksPropertyTask) Validate() error {
	return validatePropertyInput(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// Plan reports the drift the BuilderNixpacksPropertyTask would produce.
func (t BuilderNixpacksPropertyTask) Plan(ctx context.Context) PlanResult {
	return planProperty(ctx, t, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t BuilderNixpacksPropertyTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	return exportProperties(ctx, t, app, func(app, property, value string) interface{} {
		return BuilderNixpacksPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t BuilderNixpacksPropertyTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	return exportGlobalProperties(ctx, t, func(property, value string) interface{} {
		return BuilderNixpacksPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the BuilderNixpacksPropertyTask with the task registry
func init() {
	RegisterTask(&BuilderNixpacksPropertyTask{})
}
