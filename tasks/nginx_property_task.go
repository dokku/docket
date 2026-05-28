package tasks

// NginxPropertyTask manages the nginx configuration for a given dokku application
type NginxPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app"`

	// Global is a flag indicating if the nginx configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty"`

	// Property is the name of the nginx property to set
	Property string `required:"true" yaml:"property"`

	// Value is the value to set for the nginx property
	Value string `required:"false" yaml:"value,omitempty"`

	// State is the desired state of the nginx configuration
	State State `required:"true" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// NginxPropertyTaskExample contains an example of a NginxPropertyTask
type NginxPropertyTaskExample struct {
	// Name is the task name holding the NginxPropertyTask description
	Name string `yaml:"-"`

	// NginxPropertyTask is the NginxPropertyTask configuration
	NginxPropertyTask NginxPropertyTask `yaml:"dokku_nginx_property"`
}

// GetName returns the name of the example
func (e NginxPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the nginx property task
func (t NginxPropertyTask) Doc() string {
	return "Manages the nginx configuration for a given dokku application"
}

// Examples returns the examples for the nginx property task
func (t NginxPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]NginxPropertyTaskExample{
		{
			Name: "Setting the proxy read timeout for an app",
			NginxPropertyTask: NginxPropertyTask{
				App:      "node-js-app",
				Property: "proxy-read-timeout",
				Value:    "120s",
			},
		},
		{
			Name: "Setting the client max body size for an app",
			NginxPropertyTask: NginxPropertyTask{
				App:      "node-js-app",
				Property: "client-max-body-size",
				Value:    "50m",
			},
		},
		{
			Name: "Setting a global nginx property",
			NginxPropertyTask: NginxPropertyTask{
				Global:   true,
				Property: "bind-address-ipv4",
				Value:    "0.0.0.0",
			},
		},
		{
			Name: "Clearing an nginx property",
			NginxPropertyTask: NginxPropertyTask{
				App:      "node-js-app",
				Property: "proxy-read-timeout",
			},
		},
	})
}

// Execute sets or unsets the nginx property
func (t NginxPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// nginxPropertyKeys maps nginx property names to the JSON keys emitted by
// `dokku nginx:report --format json` on dokku 0.38.8+. All properties are
// app+global.
var nginxPropertyKeys = map[string]PropertyKeys{
	"access-log-format":       {PerApp: "access-log-format", Global: "global-access-log-format"},
	"access-log-path":         {PerApp: "access-log-path", Global: "global-access-log-path"},
	"bind-address-ipv4":       {PerApp: "bind-address-ipv4", Global: "global-bind-address-ipv4"},
	"bind-address-ipv6":       {PerApp: "bind-address-ipv6", Global: "global-bind-address-ipv6"},
	"client-body-timeout":     {PerApp: "client-body-timeout", Global: "global-client-body-timeout"},
	"client-header-timeout":   {PerApp: "client-header-timeout", Global: "global-client-header-timeout"},
	"client-max-body-size":    {PerApp: "client-max-body-size", Global: "global-client-max-body-size"},
	"disable-custom-config":   {PerApp: "disable-custom-config", Global: "global-disable-custom-config"},
	"error-log-path":          {PerApp: "error-log-path", Global: "global-error-log-path"},
	"hsts":                    {PerApp: "hsts", Global: "global-hsts"},
	"hsts-include-subdomains": {PerApp: "hsts-include-subdomains", Global: "global-hsts-include-subdomains"},
	"hsts-max-age":            {PerApp: "hsts-max-age", Global: "global-hsts-max-age"},
	"hsts-preload":            {PerApp: "hsts-preload", Global: "global-hsts-preload"},
	"keepalive-timeout":       {PerApp: "keepalive-timeout", Global: "global-keepalive-timeout"},
	"lingering-timeout":       {PerApp: "lingering-timeout", Global: "global-lingering-timeout"},
	"nginx-conf-sigil-path":   {PerApp: "nginx-conf-sigil-path", Global: "global-nginx-conf-sigil-path"},
	"nginx-service-command":   {PerApp: "nginx-service-command", Global: "global-nginx-service-command"},
	"proxy-buffer-size":       {PerApp: "proxy-buffer-size", Global: "global-proxy-buffer-size"},
	"proxy-buffering":         {PerApp: "proxy-buffering", Global: "global-proxy-buffering"},
	"proxy-buffers":           {PerApp: "proxy-buffers", Global: "global-proxy-buffers"},
	"proxy-busy-buffers-size": {PerApp: "proxy-busy-buffers-size", Global: "global-proxy-busy-buffers-size"},
	"proxy-connect-timeout":   {PerApp: "proxy-connect-timeout", Global: "global-proxy-connect-timeout"},
	"proxy-keepalive":         {PerApp: "proxy-keepalive", Global: "global-proxy-keepalive"},
	"proxy-read-timeout":      {PerApp: "proxy-read-timeout", Global: "global-proxy-read-timeout"},
	"proxy-send-timeout":      {PerApp: "proxy-send-timeout", Global: "global-proxy-send-timeout"},
	"send-timeout":            {PerApp: "send-timeout", Global: "global-send-timeout"},
	"underscore-in-headers":   {PerApp: "underscore-in-headers", Global: "global-underscore-in-headers"},
	"x-forwarded-for-value":   {PerApp: "x-forwarded-for-value", Global: "global-x-forwarded-for-value"},
	"x-forwarded-port-value":  {PerApp: "x-forwarded-port-value", Global: "global-x-forwarded-port-value"},
	"x-forwarded-proto-value": {PerApp: "x-forwarded-proto-value", Global: "global-x-forwarded-proto-value"},
	"x-forwarded-ssl":         {PerApp: "x-forwarded-ssl", Global: "global-x-forwarded-ssl"},
}

// Plan reports the drift the NginxPropertyTask would produce.
func (t NginxPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "nginx:set", nginxPropertyKeys)
}

// init registers the NginxPropertyTask with the task registry
func init() {
	RegisterTask(&NginxPropertyTask{})
}
