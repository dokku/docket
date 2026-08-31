package tasks

import "context"

// BuilderLambdaPropertyTask manages the builder-lambda configuration for a given dokku application
type BuilderLambdaPropertyTask PropertyFields

// BuilderLambdaPropertyTaskExample contains an example of a BuilderLambdaPropertyTask
type BuilderLambdaPropertyTaskExample struct {
	// Name is the task name holding the BuilderLambdaPropertyTask description
	Name string `yaml:"-"`

	// BuilderLambdaPropertyTask is the BuilderLambdaPropertyTask configuration
	BuilderLambdaPropertyTask BuilderLambdaPropertyTask `yaml:"dokku_builder_lambda_property"`
}

// GetName returns the name of the example
func (e BuilderLambdaPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the builder-lambda property task
func (t BuilderLambdaPropertyTask) Doc() string {
	return "Manages the builder-lambda configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t BuilderLambdaPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t BuilderLambdaPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// Examples returns the examples for the builder-lambda property task
func (t BuilderLambdaPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]BuilderLambdaPropertyTaskExample{
		{
			Name: "Setting the lambda.yml path for an app",
			BuilderLambdaPropertyTask: BuilderLambdaPropertyTask{
				App:      "node-js-app",
				Property: "lambdayml-path",
				Value:    "config/lambda.yml",
			},
		},
		{
			Name: "Setting the lambda.yml path globally",
			BuilderLambdaPropertyTask: BuilderLambdaPropertyTask{
				Global:   true,
				Property: "lambdayml-path",
				Value:    "lambda.yml",
			},
		},
		{
			Name: "Clearing the lambda.yml path for an app",
			BuilderLambdaPropertyTask: BuilderLambdaPropertyTask{
				App:      "node-js-app",
				Property: "lambdayml-path",
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the builder-lambda property
func (t BuilderLambdaPropertyTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// builderLambdaPropertyTable maps builder-lambda property names to the JSON
// keys emitted by `dokku builder-lambda:report --format json` on dokku
// 0.38.8+.
var builderLambdaPropertyTable = PropertyTable{
	Subcommand: "builder-lambda:set",
	Keys: map[string]PropertyKeys{
		"lambdayml-path": {PerApp: "lambdayml-path", Global: "global-lambdayml-path"},
	},
}

// PropertyTable returns the property schema this task manages.
func (t BuilderLambdaPropertyTask) PropertyTable() PropertyTable {
	return builderLambdaPropertyTable
}

// Validate checks the BuilderLambdaPropertyTask's inputs without contacting the server.
func (t BuilderLambdaPropertyTask) Validate() error {
	return validatePropertyInput(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// Plan reports the drift the BuilderLambdaPropertyTask would produce.
func (t BuilderLambdaPropertyTask) Plan(ctx context.Context) PlanResult {
	return planProperty(ctx, t, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t BuilderLambdaPropertyTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	return exportProperties(ctx, t, app, func(app, property, value string) interface{} {
		return BuilderLambdaPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t BuilderLambdaPropertyTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	return exportGlobalProperties(ctx, t, func(property, value string) interface{} {
		return BuilderLambdaPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the BuilderLambdaPropertyTask with the task registry
func init() {
	RegisterTask(&BuilderLambdaPropertyTask{})
}
