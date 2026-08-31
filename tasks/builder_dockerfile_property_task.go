package tasks

import "context"

// BuilderDockerfilePropertyTask manages the builder-dockerfile configuration for a given dokku application
type BuilderDockerfilePropertyTask PropertyFields

// BuilderDockerfilePropertyTaskExample contains an example of a BuilderDockerfilePropertyTask
type BuilderDockerfilePropertyTaskExample struct {
	// Name is the task name holding the BuilderDockerfilePropertyTask description
	Name string `yaml:"-"`

	// BuilderDockerfilePropertyTask is the BuilderDockerfilePropertyTask configuration
	BuilderDockerfilePropertyTask BuilderDockerfilePropertyTask `yaml:"dokku_builder_dockerfile_property"`
}

// GetName returns the name of the example
func (e BuilderDockerfilePropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the builder-dockerfile property task
func (t BuilderDockerfilePropertyTask) Doc() string {
	return "Manages the builder-dockerfile configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t BuilderDockerfilePropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t BuilderDockerfilePropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// Examples returns the examples for the builder-dockerfile property task
func (t BuilderDockerfilePropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]BuilderDockerfilePropertyTaskExample{
		{
			Name: "Setting the dockerfile path for an app",
			BuilderDockerfilePropertyTask: BuilderDockerfilePropertyTask{
				App:      "node-js-app",
				Property: "dockerfile-path",
				Value:    "Dockerfile.production",
			},
		},
		{
			Name: "Setting the dockerfile path globally",
			BuilderDockerfilePropertyTask: BuilderDockerfilePropertyTask{
				Global:   true,
				Property: "dockerfile-path",
				Value:    "Dockerfile",
			},
		},
		{
			Name: "Clearing the dockerfile path for an app",
			BuilderDockerfilePropertyTask: BuilderDockerfilePropertyTask{
				App:      "node-js-app",
				Property: "dockerfile-path",
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the builder-dockerfile property
func (t BuilderDockerfilePropertyTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// builderDockerfilePropertyTable maps builder-dockerfile property names to
// the JSON keys emitted by `dokku builder-dockerfile:report --format json`
// on dokku 0.38.8+.
var builderDockerfilePropertyTable = PropertyTable{
	Subcommand: "builder-dockerfile:set",
	Keys: map[string]PropertyKeys{
		"dockerfile-path": {PerApp: "dockerfile-path", Global: "global-dockerfile-path"},
	},
}

// PropertyTable returns the property schema this task manages.
func (t BuilderDockerfilePropertyTask) PropertyTable() PropertyTable {
	return builderDockerfilePropertyTable
}

// Validate checks the BuilderDockerfilePropertyTask's inputs without contacting the server.
func (t BuilderDockerfilePropertyTask) Validate() error {
	return validatePropertyInput(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// Plan reports the drift the BuilderDockerfilePropertyTask would produce.
func (t BuilderDockerfilePropertyTask) Plan(ctx context.Context) PlanResult {
	return planProperty(ctx, t, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t BuilderDockerfilePropertyTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	return exportProperties(ctx, t, app, func(app, property, value string) interface{} {
		return BuilderDockerfilePropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t BuilderDockerfilePropertyTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	return exportGlobalProperties(ctx, t, func(property, value string) interface{} {
		return BuilderDockerfilePropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the BuilderDockerfilePropertyTask with the task registry
func init() {
	RegisterTask(&BuilderDockerfilePropertyTask{})
}
