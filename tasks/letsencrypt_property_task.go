package tasks

// LetsencryptPropertyTask manages the letsencrypt configuration for a given dokku application
type LetsencryptPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the letsencrypt configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the letsencrypt configuration should be applied globally"`

	// Property is the name of the letsencrypt property to set
	Property string `required:"true" yaml:"property" description:"Name of the letsencrypt property to set"`

	// Value is the value to set for the letsencrypt property. Tagged sensitive
	// because some letsencrypt properties carry DNS-API credentials; benign
	// property values get masked too, which is preferable to leaking secrets.
	Value string `required:"false" sensitive:"true" yaml:"value,omitempty" description:"Value to set for the letsencrypt property"`

	// State is the desired state of the letsencrypt configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the letsencrypt configuration"`
}

// LetsencryptPropertyTaskExample contains an example of a LetsencryptPropertyTask
type LetsencryptPropertyTaskExample struct {
	// Name is the task name holding the LetsencryptPropertyTask description
	Name string `yaml:"-"`

	// LetsencryptPropertyTask is the LetsencryptPropertyTask configuration
	LetsencryptPropertyTask LetsencryptPropertyTask `yaml:"dokku_letsencrypt_property"`
}

// GetName returns the name of the example
func (e LetsencryptPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the letsencrypt property task
func (t LetsencryptPropertyTask) Doc() string {
	return "Manages the letsencrypt configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t LetsencryptPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Requirements lists the non-core dokku plugins this task depends on.
func (t LetsencryptPropertyTask) Requirements() []string {
	return []string{"dokku-letsencrypt plugin"}
}

// Examples returns the examples for the letsencrypt property task
func (t LetsencryptPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]LetsencryptPropertyTaskExample{
		{
			Name: "Setting the letsencrypt email for an app",
			LetsencryptPropertyTask: LetsencryptPropertyTask{
				App:      "node-js-app",
				Property: "email",
				Value:    "admin@example.com",
			},
		},
		{
			Name: "Setting the dns provider for an app",
			LetsencryptPropertyTask: LetsencryptPropertyTask{
				App:      "node-js-app",
				Property: "dns-provider",
				Value:    "namecheap",
			},
		},
		{
			Name: "Setting a dns-provider-* env var globally",
			LetsencryptPropertyTask: LetsencryptPropertyTask{
				Global:   true,
				Property: "dns-provider-NAMECHEAP_API_USER",
				Value:    "deploy-bot",
			},
		},
		{
			Name: "Clearing the letsencrypt email for an app",
			LetsencryptPropertyTask: LetsencryptPropertyTask{
				App:      "node-js-app",
				Property: "email",
			},
		},
	})
}

// Execute sets or unsets the letsencrypt property
func (t LetsencryptPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// letsencryptPropertyKeys maps letsencrypt property names to the JSON keys
// emitted by `dokku letsencrypt:report --format json` on
// dokku-letsencrypt v0.20.4+. The `dns-provider-*` family is dynamic and
// handled by isDynamicProperty without a map entry.
var letsencryptPropertyKeys = map[string]PropertyKeys{
	"dns-provider":        {PerApp: "dns-provider", Global: "global-dns-provider"},
	"email":               {PerApp: "email", Global: "global-email"},
	"graceperiod":         {PerApp: "graceperiod", Global: "global-graceperiod"},
	"lego-args":           {PerApp: "lego-args", Global: "global-lego-args"},
	"lego-docker-options": {PerApp: "lego-docker-options", Global: "global-lego-docker-options"},
	"server":              {PerApp: "server", Global: "global-server"},
}

// Validate checks the LetsencryptPropertyTask's inputs without contacting the server.
func (t LetsencryptPropertyTask) Validate() error {
	return validatePropertyInput(t.State, t.App, t.Global, t.Property, t.Value, "letsencrypt:set", letsencryptPropertyKeys)
}

// Plan reports the drift the LetsencryptPropertyTask would produce.
func (t LetsencryptPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "letsencrypt:set", letsencryptPropertyKeys)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t LetsencryptPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(app, "letsencrypt:set", letsencryptPropertyKeys, func(app, property, value string) interface{} {
		return LetsencryptPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t LetsencryptPropertyTask) ExportGlobal() ([]interface{}, error) {
	return exportGlobalProperties("letsencrypt:set", letsencryptPropertyKeys, func(property, value string) interface{} {
		return LetsencryptPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the LetsencryptPropertyTask with the task registry
func init() {
	RegisterTask(&LetsencryptPropertyTask{})
}
