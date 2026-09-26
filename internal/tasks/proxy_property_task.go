package tasks

import "context"

// ProxyPropertyTask manages the proxy configuration for a given dokku application
type ProxyPropertyTask PropertyFields

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

// ProbeSupport reports whether Plan() can read this task's current state.
func (t ProxyPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
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
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the proxy property
func (t ProxyPropertyTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// proxyPropertyTable maps proxy property names to the JSON keys emitted by
// `dokku proxy:report --format json` on dokku 0.38.8+. `disabled`/`enabled`
// are managed via proxy:enable/proxy:disable through ProxyTogglePropertyTask.
var proxyPropertyTable = PropertyTable{
	Subcommand: "proxy:set",
	Keys: map[string]PropertyKeys{
		"type":           {PerApp: "type", Global: "global-type"},
		"proxy-port":     {PerApp: "proxy-port", Global: "global-proxy-port"},
		"proxy-ssl-port": {PerApp: "proxy-ssl-port", Global: "global-proxy-ssl-port"},
	},
}

// PropertyTable returns the property schema this task manages.
func (t ProxyPropertyTask) PropertyTable() PropertyTable {
	return proxyPropertyTable
}

// Validate checks the ProxyPropertyTask's inputs without contacting the server.
func (t ProxyPropertyTask) Validate() error {
	return validatePropertyInput(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// Plan reports the drift the ProxyPropertyTask would produce.
func (t ProxyPropertyTask) Plan(ctx context.Context) PlanResult {
	return planProperty(ctx, t, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t ProxyPropertyTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	return exportProperties(ctx, t, app, func(app, property, value string) interface{} {
		return ProxyPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t ProxyPropertyTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	return exportGlobalProperties(ctx, t, func(property, value string) interface{} {
		return ProxyPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the ProxyPropertyTask with the task registry
func init() {
	RegisterTask(&ProxyPropertyTask{})
}
