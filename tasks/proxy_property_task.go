package tasks

// ProxyPropertyTask manages the proxy configuration for a given dokku application
type ProxyPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app"`

	// Global is a flag indicating if the proxy configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty"`

	// Property is the name of the proxy property to set
	Property string `required:"true" yaml:"property"`

	// Value is the value to set for the proxy property
	Value string `required:"false" yaml:"value,omitempty"`

	// State is the desired state of the proxy configuration
	State State `required:"true" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// ProxyPropertyTaskExample contains an example of a ProxyPropertyTask
type ProxyPropertyTaskExample struct {
	// Name is the task name holding the ProxyPropertyTask description
	Name string `yaml:"-"`

	// ProxyPropertyTask is the ProxyPropertyTask configuration
	ProxyPropertyTask ProxyPropertyTask `yaml:"dokku_proxy_property"`
}

// GetName returns the name of the example
func (e ProxyPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the proxy property task
func (t ProxyPropertyTask) Doc() string {
	return "Manages the proxy configuration for a given dokku application"
}

// Examples returns the examples for the proxy property task
func (t ProxyPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]ProxyPropertyTaskExample{
		{
			Name: "Setting the proxy type for an app",
			ProxyPropertyTask: ProxyPropertyTask{
				App:      "node-js-app",
				Property: "type",
				Value:    "nginx",
			},
		},
		{
			Name: "Setting the proxy type globally",
			ProxyPropertyTask: ProxyPropertyTask{
				Global:   true,
				Property: "type",
				Value:    "haproxy",
			},
		},
		{
			Name: "Setting the proxy port for an app",
			ProxyPropertyTask: ProxyPropertyTask{
				App:      "node-js-app",
				Property: "proxy-port",
				Value:    "8080",
			},
		},
		{
			Name: "Clearing the proxy type for an app",
			ProxyPropertyTask: ProxyPropertyTask{
				App:      "node-js-app",
				Property: "type",
			},
		},
	})
}

// Execute sets or unsets the proxy property
func (t ProxyPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the ProxyPropertyTask would produce.
func (t ProxyPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "proxy:set")
}

// init registers the ProxyPropertyTask with the task registry
func init() {
	RegisterTask(&ProxyPropertyTask{})
}
