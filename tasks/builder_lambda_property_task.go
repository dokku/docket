package tasks

// BuilderLambdaPropertyTask manages the builder-lambda configuration for a given dokku application
type BuilderLambdaPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the builder-lambda configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the builder-lambda configuration should be applied globally"`

	// Property is the name of the builder-lambda property to set
	Property string `required:"true" yaml:"property" description:"Name of the builder-lambda property to set"`

	// Value is the value to set for the builder-lambda property
	Value string `required:"false" yaml:"value,omitempty" description:"Value to set for the builder-lambda property"`

	// State is the desired state of the builder-lambda configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the builder-lambda configuration"`
}

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
			},
		},
	})
}

// Execute sets or unsets the builder-lambda property
func (t BuilderLambdaPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// builderLambdaPropertyKeys maps builder-lambda property names to the JSON
// keys emitted by `dokku builder-lambda:report --format json` on dokku
// 0.38.8+.
var builderLambdaPropertyKeys = map[string]PropertyKeys{
	"lambdayml-path": {PerApp: "lambdayml-path", Global: "global-lambdayml-path"},
}

// Plan reports the drift the BuilderLambdaPropertyTask would produce.
func (t BuilderLambdaPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "builder-lambda:set", builderLambdaPropertyKeys)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t BuilderLambdaPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(app, "builder-lambda:set", builderLambdaPropertyKeys, func(app, property, value string) interface{} {
		return BuilderLambdaPropertyTask{App: app, Property: property, Value: value}
	})
}

// init registers the BuilderLambdaPropertyTask with the task registry
func init() {
	RegisterTask(&BuilderLambdaPropertyTask{})
}
