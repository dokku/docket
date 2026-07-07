package tasks

// NetworkPropertyTask manages the network property for a given dokku application
type NetworkPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the network property should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the network property should be applied globally"`

	// Property is the name of the network property to set
	Property string `required:"true" yaml:"property" description:"Name of the network property to set"`

	// Value is the value of the network property to set
	Value string `required:"false" yaml:"value,omitempty" description:"Value of the network property to set"`

	// State is the desired state of the network property
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the network property"`
}

// NetworkPropertyTaskExample contains an example of a NetworkPropertyTask
type NetworkPropertyTaskExample struct {
	// Name is the task name holding the NetworkPropertyTask description
	Name string `yaml:"-"`

	// NetworkPropertyTask is the NetworkPropertyTask configuration
	NetworkPropertyTask NetworkPropertyTask `yaml:"dokku_network_property"`
}

// GetName returns the name of the example
func (e NetworkPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the network property task
func (t NetworkPropertyTask) Doc() string {
	return "Manages the network property for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t NetworkPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Examples returns the examples for the network property task
func (t NetworkPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]NetworkPropertyTaskExample{
		{
			Name: "Associates a network after a container is created but before it is started",
			NetworkPropertyTask: NetworkPropertyTask{
				App:      "hello-world",
				Property: "attach-post-create",
				Value:    "example-network",
			},
		},
		{
			Name: "Associates the network at container creation",
			NetworkPropertyTask: NetworkPropertyTask{
				App:      "hello-world",
				Property: "initial-network",
				Value:    "example-network",
			},
		},
		{
			Name: "Setting a global network property",
			NetworkPropertyTask: NetworkPropertyTask{
				Global:   true,
				Property: "attach-post-create",
				Value:    "example-network",
			},
		},
		{
			Name: "Clearing a network property",
			NetworkPropertyTask: NetworkPropertyTask{
				App:      "hello-world",
				Property: "attach-post-create",
			},
		},
	})
}

// Execute sets or unsets the network property
func (t NetworkPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// networkPropertyKeys maps network property names to the JSON keys emitted
// by `dokku network:report --format json` on dokku 0.38.8+.
// static-web-listener is per-app only.
var networkPropertyKeys = map[string]PropertyKeys{
	"attach-post-create":  {PerApp: "attach-post-create", Global: "global-attach-post-create"},
	"attach-post-deploy":  {PerApp: "attach-post-deploy", Global: "global-attach-post-deploy"},
	"bind-all-interfaces": {PerApp: "bind-all-interfaces", Global: "global-bind-all-interfaces"},
	"initial-network":     {PerApp: "initial-network", Global: "global-initial-network"},
	"static-web-listener": {PerApp: "static-web-listener", Global: ""},
	"tld":                 {PerApp: "tld", Global: "global-tld"},
}

// Validate checks the NetworkPropertyTask's inputs without contacting the server.
func (t NetworkPropertyTask) Validate() error {
	return validatePropertyInput(t.State, t.App, t.Global, t.Property, t.Value, "network:set", networkPropertyKeys)
}

// Plan reports the drift the NetworkPropertyTask would produce.
func (t NetworkPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "network:set", networkPropertyKeys)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t NetworkPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(app, "network:set", networkPropertyKeys, func(app, property, value string) interface{} {
		return NetworkPropertyTask{App: app, Property: property, Value: value}
	})
}

// init registers the NetworkPropertyTask with the task registry
func init() {
	RegisterTask(&NetworkPropertyTask{})
}
