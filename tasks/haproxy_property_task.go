package tasks

// HaproxyPropertyTask manages the haproxy configuration for a given dokku application
type HaproxyPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the haproxy configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the haproxy configuration should be applied globally"`

	// Property is the name of the haproxy property to set
	Property string `required:"true" yaml:"property" description:"Name of the haproxy property to set"`

	// Value is the value to set for the haproxy property
	Value string `required:"false" yaml:"value,omitempty" description:"Value to set for the haproxy property"`

	// State is the desired state of the haproxy configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the haproxy configuration"`
}

// HaproxyPropertyTaskExample contains an example of a HaproxyPropertyTask
type HaproxyPropertyTaskExample struct {
	// Name is the task name holding the HaproxyPropertyTask description
	Name string `yaml:"-"`

	// HaproxyPropertyTask is the HaproxyPropertyTask configuration
	HaproxyPropertyTask HaproxyPropertyTask `yaml:"dokku_haproxy_property"`
}

// GetName returns the name of the example
func (e HaproxyPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the haproxy property task
func (t HaproxyPropertyTask) Doc() string {
	return "Manages the haproxy configuration for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t HaproxyPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Examples returns the examples for the haproxy property task
func (t HaproxyPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]HaproxyPropertyTaskExample{
		{
			Name: "Setting the letsencrypt email for an app",
			HaproxyPropertyTask: HaproxyPropertyTask{
				App:      "node-js-app",
				Property: "letsencrypt-email",
				Value:    "admin@example.com",
			},
		},
		{
			Name: "Setting the log level globally",
			HaproxyPropertyTask: HaproxyPropertyTask{
				Global:   true,
				Property: "log-level",
				Value:    "INFO",
			},
		},
		{
			Name: "Clearing the letsencrypt email for an app",
			HaproxyPropertyTask: HaproxyPropertyTask{
				App:      "node-js-app",
				Property: "letsencrypt-email",
			},
		},
	})
}

// Execute sets or unsets the haproxy property
func (t HaproxyPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// haproxyPropertyKeys maps haproxy property names to the JSON keys emitted
// by `dokku haproxy:report --format json` on dokku 0.38.8+. All properties
// are global-only.
var haproxyPropertyKeys = map[string]PropertyKeys{
	"image":              {PerApp: "", Global: "global-image"},
	"letsencrypt-email":  {PerApp: "", Global: "global-letsencrypt-email"},
	"letsencrypt-server": {PerApp: "", Global: "global-letsencrypt-server"},
	"log-level":          {PerApp: "", Global: "global-log-level"},
	"refresh-conf":       {PerApp: "", Global: "global-refresh-conf"},
}

// Validate checks the HaproxyPropertyTask's inputs without contacting the server.
func (t HaproxyPropertyTask) Validate() error {
	return validatePropertyInput(t.State, t.App, t.Global, t.Property, t.Value, "haproxy:set", haproxyPropertyKeys)
}

// Plan reports the drift the HaproxyPropertyTask would produce.
func (t HaproxyPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "haproxy:set", haproxyPropertyKeys)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t HaproxyPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(app, "haproxy:set", haproxyPropertyKeys, func(app, property, value string) interface{} {
		return HaproxyPropertyTask{App: app, Property: property, Value: value}
	})
}

// init registers the HaproxyPropertyTask with the task registry
func init() {
	RegisterTask(&HaproxyPropertyTask{})
}
