package tasks

// CaddyPropertyTask manages the caddy configuration for a given dokku application
type CaddyPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app"`

	// Global is a flag indicating if the caddy configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty"`

	// Property is the name of the caddy property to set
	Property string `required:"true" yaml:"property"`

	// Value is the value to set for the caddy property
	Value string `required:"false" yaml:"value,omitempty"`

	// State is the desired state of the caddy configuration
	State State `required:"true" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// CaddyPropertyTaskExample contains an example of a CaddyPropertyTask
type CaddyPropertyTaskExample struct {
	// Name is the task name holding the CaddyPropertyTask description
	Name string `yaml:"-"`

	// CaddyPropertyTask is the CaddyPropertyTask configuration
	CaddyPropertyTask CaddyPropertyTask `yaml:"dokku_caddy_property"`
}

// GetName returns the name of the example
func (e CaddyPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the caddy property task
func (t CaddyPropertyTask) Doc() string {
	return "Manages the caddy configuration for a given dokku application"
}

// Examples returns the examples for the caddy property task
func (t CaddyPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]CaddyPropertyTaskExample{
		{
			Name: "Enabling internal TLS for an app",
			CaddyPropertyTask: CaddyPropertyTask{
				App:      "node-js-app",
				Property: "tls-internal",
				Value:    "true",
			},
		},
		{
			Name: "Setting the letsencrypt email globally",
			CaddyPropertyTask: CaddyPropertyTask{
				Global:   true,
				Property: "letsencrypt-email",
				Value:    "admin@example.com",
			},
		},
		{
			Name: "Clearing internal TLS for an app",
			CaddyPropertyTask: CaddyPropertyTask{
				App:      "node-js-app",
				Property: "tls-internal",
			},
		},
	})
}

// Execute sets or unsets the caddy property
func (t CaddyPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// caddyPropertyKeys maps caddy property names to the JSON keys emitted by
// `dokku caddy:report --format json` on dokku 0.38.8+. All except
// tls-internal are global-only.
var caddyPropertyKeys = map[string]PropertyKeys{
	"image":              {PerApp: "", Global: "global-image"},
	"letsencrypt-email":  {PerApp: "", Global: "global-letsencrypt-email"},
	"letsencrypt-server": {PerApp: "", Global: "global-letsencrypt-server"},
	"log-level":          {PerApp: "", Global: "global-log-level"},
	"polling-interval":   {PerApp: "", Global: "global-polling-interval"},
	"tls-internal":       {PerApp: "tls-internal", Global: "global-tls-internal"},
}

// Plan reports the drift the CaddyPropertyTask would produce.
func (t CaddyPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "caddy:set", caddyPropertyKeys)
}

// init registers the CaddyPropertyTask with the task registry
func init() {
	RegisterTask(&CaddyPropertyTask{})
}
