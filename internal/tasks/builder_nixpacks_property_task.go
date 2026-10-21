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

// Constraints declares that the property is set for an app or globally.
func (t BuilderNixpacksPropertyTask) Constraints() []Constraint {
	return appOrGlobalConstraints
}

// ExportSupport reports how docket export handles this task.
func (t BuilderNixpacksPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t BuilderNixpacksPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// MinimumVersions reports the oldest Dokku this task's probe parses.
// The floor is `builder-nixpacks:report --format json`, which this task parses.
func (t BuilderNixpacksPropertyTask) MinimumVersions() VersionSupport {
	return VersionSupport{Dokku: "0.38.8"}
}

// examples returns the examples for the builder-nixpacks property task
func (t BuilderNixpacksPropertyTask) examples() ([]Doc, error) {
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
	return executeTask(ctx, t)
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

// propertyTable returns the property schema this task manages.
func (t BuilderNixpacksPropertyTask) propertyTable() PropertyTable {
	return builderNixpacksPropertyTable
}

// Validate checks the BuilderNixpacksPropertyTask's inputs without contacting the server.
func (t BuilderNixpacksPropertyTask) Validate() error {
	return validatePropertyInput(t, t.Global, t.Property)
}

// Plan reports the drift the BuilderNixpacksPropertyTask would produce.
func (t BuilderNixpacksPropertyTask) Plan(ctx context.Context) PlanResult {
	if r, ok := gatePlan(ctx, t); !ok {
		return r
	}
	return planProperty(ctx, t, func(f PropertyFields) Task { return BuilderNixpacksPropertyTask(f) }, t.State, t.App, t.Global, t.Property, t.Value)
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
