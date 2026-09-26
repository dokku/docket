package tasks

import "context"

// BuilderPackPropertyTask manages the builder-pack configuration for a given dokku application
type BuilderPackPropertyTask PropertyFields

// BuilderPackPropertyTaskExample contains an example of a BuilderPackPropertyTask
type BuilderPackPropertyTaskExample struct {
	// Name is the task name holding the BuilderPackPropertyTask description
	Name string `yaml:"-"`

	// BuilderPackPropertyTask is the BuilderPackPropertyTask configuration
	BuilderPackPropertyTask BuilderPackPropertyTask `yaml:"dokku_builder_pack_property"`
}

// GetName returns the name of the example
func (e BuilderPackPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the builder-pack property task
func (t BuilderPackPropertyTask) Doc() string {
	return "Manages the builder-pack configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t BuilderPackPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t BuilderPackPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// Examples returns the examples for the builder-pack property task
func (t BuilderPackPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]BuilderPackPropertyTaskExample{
		{
			Name: "Setting the project.toml path for an app",
			BuilderPackPropertyTask: BuilderPackPropertyTask{
				App:      "node-js-app",
				Property: "projecttoml-path",
				Value:    "config/project.toml",
			},
		},
		{
			Name: "Setting the project.toml path globally",
			BuilderPackPropertyTask: BuilderPackPropertyTask{
				Global:   true,
				Property: "projecttoml-path",
				Value:    "project.toml",
			},
		},
		{
			Name: "Clearing the project.toml path for an app",
			BuilderPackPropertyTask: BuilderPackPropertyTask{
				App:      "node-js-app",
				Property: "projecttoml-path",
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the builder-pack property
func (t BuilderPackPropertyTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// builderPackPropertyTable maps builder-pack property names to the JSON keys
// emitted by `dokku builder-pack:report --format json` on dokku 0.38.8+.
var builderPackPropertyTable = PropertyTable{
	Subcommand: "builder-pack:set",
	Keys: map[string]PropertyKeys{
		"projecttoml-path": {PerApp: "projecttoml-path", Global: "global-projecttoml-path"},
	},
}

// PropertyTable returns the property schema this task manages.
func (t BuilderPackPropertyTask) PropertyTable() PropertyTable {
	return builderPackPropertyTable
}

// Validate checks the BuilderPackPropertyTask's inputs without contacting the server.
func (t BuilderPackPropertyTask) Validate() error {
	return validatePropertyInput(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// Plan reports the drift the BuilderPackPropertyTask would produce.
func (t BuilderPackPropertyTask) Plan(ctx context.Context) PlanResult {
	return planProperty(ctx, t, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t BuilderPackPropertyTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	return exportProperties(ctx, t, app, func(app, property, value string) interface{} {
		return BuilderPackPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t BuilderPackPropertyTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	return exportGlobalProperties(ctx, t, func(property, value string) interface{} {
		return BuilderPackPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the BuilderPackPropertyTask with the task registry
func init() {
	RegisterTask(&BuilderPackPropertyTask{})
}
