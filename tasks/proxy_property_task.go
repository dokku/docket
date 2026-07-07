package tasks

// ProxyPropertyTask manages the proxy configuration for a given dokku application
type ProxyPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the proxy configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the proxy configuration should be applied globally"`

	// Property is the name of the proxy property to set
	Property string `required:"true" yaml:"property" description:"Name of the proxy property to set"`

	// Value is the value to set for the proxy property
	Value string `required:"false" yaml:"value,omitempty" description:"Value to set for the proxy property"`

	// State is the desired state of the proxy configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the proxy configuration"`
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

// ExportSupport reports how docket export handles this task.
func (t ProxyPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
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

// proxyPropertyKeys maps proxy property names to the JSON keys emitted by
// `dokku proxy:report --format json` on dokku 0.38.8+. `disabled`/`enabled`
// are managed via proxy:enable/proxy:disable through ProxyTogglePropertyTask.
var proxyPropertyKeys = map[string]PropertyKeys{
	"type":           {PerApp: "type", Global: "global-type"},
	"proxy-port":     {PerApp: "proxy-port", Global: "global-proxy-port"},
	"proxy-ssl-port": {PerApp: "proxy-ssl-port", Global: "global-proxy-ssl-port"},
}

// Validate checks the ProxyPropertyTask's inputs without contacting the server.
func (t ProxyPropertyTask) Validate() error {
	return validatePropertyInput(t.State, t.App, t.Global, t.Property, t.Value, "proxy:set", proxyPropertyKeys)
}

// Plan reports the drift the ProxyPropertyTask would produce.
func (t ProxyPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "proxy:set", proxyPropertyKeys)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t ProxyPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(app, "proxy:set", proxyPropertyKeys, func(app, property, value string) interface{} {
		return ProxyPropertyTask{App: app, Property: property, Value: value}
	})
}

// init registers the ProxyPropertyTask with the task registry
func init() {
	RegisterTask(&ProxyPropertyTask{})
}
