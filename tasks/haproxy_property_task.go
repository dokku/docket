package tasks

// HaproxyPropertyTask manages the haproxy configuration for a given dokku application
type HaproxyPropertyTask PropertyFields

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

// ProbeSupport reports whether Plan() can read this task's current state.
func (t HaproxyPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// Examples returns the examples for the haproxy property task
func (t HaproxyPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]HaproxyPropertyTaskExample{
		{
			Name: "Setting the letsencrypt email globally",
			HaproxyPropertyTask: HaproxyPropertyTask{
				Global:   true,
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
			Name: "Clearing the letsencrypt email globally",
			HaproxyPropertyTask: HaproxyPropertyTask{
				Global:   true,
				Property: "letsencrypt-email",
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the haproxy property
func (t HaproxyPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// haproxyPropertyTable maps haproxy property names to the JSON keys emitted
// by `dokku haproxy:report --format json` on dokku 0.38.8+. All properties
// are global-only.
var haproxyPropertyTable = PropertyTable{
	Subcommand: "haproxy:set",
	Keys: map[string]PropertyKeys{
		"image":              {PerApp: "", Global: "global-image"},
		"letsencrypt-email":  {PerApp: "", Global: "global-letsencrypt-email"},
		"letsencrypt-server": {PerApp: "", Global: "global-letsencrypt-server"},
		"log-level":          {PerApp: "", Global: "global-log-level"},
		"refresh-conf":       {PerApp: "", Global: "global-refresh-conf"},
	},
}

// PropertyTable returns the property schema this task manages.
func (t HaproxyPropertyTask) PropertyTable() PropertyTable {
	return haproxyPropertyTable
}

// Validate checks the HaproxyPropertyTask's inputs without contacting the server.
func (t HaproxyPropertyTask) Validate() error {
	return validatePropertyInput(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// Plan reports the drift the HaproxyPropertyTask would produce.
func (t HaproxyPropertyTask) Plan() PlanResult {
	return planProperty(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t HaproxyPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(t, app, func(app, property, value string) interface{} {
		return HaproxyPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t HaproxyPropertyTask) ExportGlobal() ([]interface{}, error) {
	return exportGlobalProperties(t, func(property, value string) interface{} {
		return HaproxyPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the HaproxyPropertyTask with the task registry
func init() {
	RegisterTask(&HaproxyPropertyTask{})
}
