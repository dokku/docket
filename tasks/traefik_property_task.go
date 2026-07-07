package tasks

// TraefikPropertyTask manages the traefik configuration for a given dokku application
type TraefikPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the traefik configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the traefik configuration should be applied globally"`

	// Property is the name of the traefik property to set
	Property string `required:"true" yaml:"property" description:"Name of the traefik property to set"`

	// Value is the value to set for the traefik property
	Value string `required:"false" yaml:"value,omitempty" description:"Value to set for the traefik property"`

	// State is the desired state of the traefik configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the traefik configuration"`
}

// TraefikPropertyTaskExample contains an example of a TraefikPropertyTask
type TraefikPropertyTaskExample struct {
	// Name is the task name holding the TraefikPropertyTask description
	Name string `yaml:"-"`

	// TraefikPropertyTask is the TraefikPropertyTask configuration
	TraefikPropertyTask TraefikPropertyTask `yaml:"dokku_traefik_property"`
}

// GetName returns the name of the example
func (e TraefikPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the traefik property task
func (t TraefikPropertyTask) Doc() string {
	return "Manages the traefik configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t TraefikPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Examples returns the examples for the traefik property task
func (t TraefikPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]TraefikPropertyTaskExample{
		{
			Name: "Setting the letsencrypt email for an app",
			TraefikPropertyTask: TraefikPropertyTask{
				App:      "node-js-app",
				Property: "letsencrypt-email",
				Value:    "admin@example.com",
			},
		},
		{
			Name: "Setting the log level globally",
			TraefikPropertyTask: TraefikPropertyTask{
				Global:   true,
				Property: "log-level",
				Value:    "INFO",
			},
		},
		{
			Name: "Clearing the letsencrypt email for an app",
			TraefikPropertyTask: TraefikPropertyTask{
				App:      "node-js-app",
				Property: "letsencrypt-email",
			},
		},
	})
}

// Execute sets or unsets the traefik property
func (t TraefikPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// traefikPropertyKeys maps traefik property names to the JSON keys emitted
// by `dokku traefik:report --format json` on dokku 0.38.8+. All properties
// are global-only. The `dns-provider-*` family is dynamic and handled by
// isDynamicProperty without a map entry.
var traefikPropertyKeys = map[string]PropertyKeys{
	"api-enabled":             {PerApp: "", Global: "global-api-enabled"},
	"api-entry-point":         {PerApp: "", Global: "global-api-entry-point"},
	"api-entry-point-address": {PerApp: "", Global: "global-api-entry-point-address"},
	"api-vhost":               {PerApp: "", Global: "global-api-vhost"},
	"basic-auth-password":     {PerApp: "", Global: "global-basic-auth-password"},
	"basic-auth-username":     {PerApp: "", Global: "global-basic-auth-username"},
	"challenge-mode":          {PerApp: "", Global: "global-challenge-mode"},
	"dashboard-enabled":       {PerApp: "", Global: "global-dashboard-enabled"},
	"dns-provider":            {PerApp: "", Global: "global-dns-provider"},
	"http-entry-point":        {PerApp: "", Global: "global-http-entry-point"},
	"https-entry-point":       {PerApp: "", Global: "global-https-entry-point"},
	"image":                   {PerApp: "", Global: "global-image"},
	"letsencrypt-email":       {PerApp: "", Global: "global-letsencrypt-email"},
	"letsencrypt-server":      {PerApp: "", Global: "global-letsencrypt-server"},
	"log-level":               {PerApp: "", Global: "global-log-level"},
}

// Plan reports the drift the TraefikPropertyTask would produce.
func (t TraefikPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "traefik:set", traefikPropertyKeys)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t TraefikPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(app, "traefik:set", traefikPropertyKeys, func(app, property, value string) interface{} {
		return TraefikPropertyTask{App: app, Property: property, Value: value}
	})
}

// init registers the TraefikPropertyTask with the task registry
func init() {
	RegisterTask(&TraefikPropertyTask{})
}
