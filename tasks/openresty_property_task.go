package tasks

// OpenrestyPropertyTask manages the openresty configuration for a given dokku application
type OpenrestyPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app"`

	// Global is a flag indicating if the openresty configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty"`

	// Property is the name of the openresty property to set
	Property string `required:"true" yaml:"property"`

	// Value is the value to set for the openresty property
	Value string `required:"false" yaml:"value,omitempty"`

	// State is the desired state of the openresty configuration
	State State `required:"true" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// OpenrestyPropertyTaskExample contains an example of an OpenrestyPropertyTask
type OpenrestyPropertyTaskExample struct {
	// Name is the task name holding the OpenrestyPropertyTask description
	Name string `yaml:"-"`

	// OpenrestyPropertyTask is the OpenrestyPropertyTask configuration
	OpenrestyPropertyTask OpenrestyPropertyTask `yaml:"dokku_openresty_property"`
}

// GetName returns the name of the example
func (e OpenrestyPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the openresty property task
func (t OpenrestyPropertyTask) Doc() string {
	return "Manages the openresty configuration for a given dokku application"
}

// Examples returns the examples for the openresty property task
func (t OpenrestyPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]OpenrestyPropertyTaskExample{
		{
			Name: "Setting the proxy read timeout for an app",
			OpenrestyPropertyTask: OpenrestyPropertyTask{
				App:      "node-js-app",
				Property: "proxy-read-timeout",
				Value:    "120s",
			},
		},
		{
			Name: "Setting the client max body size for an app",
			OpenrestyPropertyTask: OpenrestyPropertyTask{
				App:      "node-js-app",
				Property: "client-max-body-size",
				Value:    "50m",
			},
		},
		{
			Name: "Setting a global openresty property",
			OpenrestyPropertyTask: OpenrestyPropertyTask{
				Global:   true,
				Property: "bind-address-ipv4",
				Value:    "0.0.0.0",
			},
		},
		{
			Name: "Clearing an openresty property",
			OpenrestyPropertyTask: OpenrestyPropertyTask{
				App:      "node-js-app",
				Property: "proxy-read-timeout",
			},
		},
	})
}

// Execute sets or unsets the openresty property
func (t OpenrestyPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// openrestyPropertyKeys maps openresty property names to the JSON keys
// emitted by `dokku openresty:report --format json` on dokku 0.38.9+, which
// gave openresty a full raw/computed/global split. Every property is
// app+global except image/letsencrypt-email/letsencrypt-server/log-level/
// allowed-letsencrypt-domains-func-base64, which remain global-only.
var openrestyPropertyKeys = map[string]PropertyKeys{
	"access-log-format":                       {PerApp: "access-log-format", Global: "global-access-log-format"},
	"access-log-path":                         {PerApp: "access-log-path", Global: "global-access-log-path"},
	"allowed-letsencrypt-domains-func-base64": {PerApp: "", Global: "global-allowed-letsencrypt-domains-func-base64"},
	"bind-address-ipv4":                       {PerApp: "bind-address-ipv4", Global: "global-bind-address-ipv4"},
	"bind-address-ipv6":                       {PerApp: "bind-address-ipv6", Global: "global-bind-address-ipv6"},
	"client-body-timeout":                     {PerApp: "client-body-timeout", Global: "global-client-body-timeout"},
	"client-header-timeout":                   {PerApp: "client-header-timeout", Global: "global-client-header-timeout"},
	"client-max-body-size":                    {PerApp: "client-max-body-size", Global: "global-client-max-body-size"},
	"error-log-path":                          {PerApp: "error-log-path", Global: "global-error-log-path"},
	"hsts":                                    {PerApp: "hsts", Global: "global-hsts"},
	"hsts-include-subdomains":                 {PerApp: "hsts-include-subdomains", Global: "global-hsts-include-subdomains"},
	"hsts-max-age":                            {PerApp: "hsts-max-age", Global: "global-hsts-max-age"},
	"hsts-preload":                            {PerApp: "hsts-preload", Global: "global-hsts-preload"},
	"image":                                   {PerApp: "", Global: "global-image"},
	"keepalive-timeout":                       {PerApp: "keepalive-timeout", Global: "global-keepalive-timeout"},
	"letsencrypt-email":                       {PerApp: "", Global: "global-letsencrypt-email"},
	"letsencrypt-server":                      {PerApp: "", Global: "global-letsencrypt-server"},
	"lingering-timeout":                       {PerApp: "lingering-timeout", Global: "global-lingering-timeout"},
	"log-level":                               {PerApp: "", Global: "global-log-level"},
	"proxy-buffer-size":                       {PerApp: "proxy-buffer-size", Global: "global-proxy-buffer-size"},
	"proxy-buffering":                         {PerApp: "proxy-buffering", Global: "global-proxy-buffering"},
	"proxy-buffers":                           {PerApp: "proxy-buffers", Global: "global-proxy-buffers"},
	"proxy-busy-buffers-size":                 {PerApp: "proxy-busy-buffers-size", Global: "global-proxy-busy-buffers-size"},
	"proxy-connect-timeout":                   {PerApp: "proxy-connect-timeout", Global: "global-proxy-connect-timeout"},
	"proxy-read-timeout":                      {PerApp: "proxy-read-timeout", Global: "global-proxy-read-timeout"},
	"proxy-send-timeout":                      {PerApp: "proxy-send-timeout", Global: "global-proxy-send-timeout"},
	"send-timeout":                            {PerApp: "send-timeout", Global: "global-send-timeout"},
	"underscore-in-headers":                   {PerApp: "underscore-in-headers", Global: "global-underscore-in-headers"},
	"x-forwarded-for-value":                   {PerApp: "x-forwarded-for-value", Global: "global-x-forwarded-for-value"},
	"x-forwarded-port-value":                  {PerApp: "x-forwarded-port-value", Global: "global-x-forwarded-port-value"},
	"x-forwarded-proto-value":                 {PerApp: "x-forwarded-proto-value", Global: "global-x-forwarded-proto-value"},
	"x-forwarded-ssl":                         {PerApp: "x-forwarded-ssl", Global: "global-x-forwarded-ssl"},
}

// Plan reports the drift the OpenrestyPropertyTask would produce.
func (t OpenrestyPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "openresty:set", openrestyPropertyKeys)
}

// init registers the OpenrestyPropertyTask with the task registry
func init() {
	RegisterTask(&OpenrestyPropertyTask{})
}
