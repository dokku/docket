package tasks

// CronPropertyTask manages the cron configuration for a given dokku application
type CronPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app"`

	// Global is a flag indicating if the cron configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty"`

	// Property is the name of the cron property to set
	Property string `required:"true" yaml:"property"`

	// Value is the value to set for the cron property
	Value string `required:"false" yaml:"value,omitempty"`

	// State is the desired state of the cron configuration
	State State `required:"true" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// CronPropertyTaskExample contains an example of a CronPropertyTask
type CronPropertyTaskExample struct {
	// Name is the task name holding the CronPropertyTask description
	Name string `yaml:"-"`

	// CronPropertyTask is the CronPropertyTask configuration
	CronPropertyTask CronPropertyTask `yaml:"dokku_cron_property"`
}

// GetName returns the name of the example
func (e CronPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the cron property task
func (t CronPropertyTask) Doc() string {
	return "Manages the cron configuration for a given dokku application"
}

// Examples returns the examples for the cron property task
func (t CronPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]CronPropertyTaskExample{
		{
			Name: "Enabling maintenance mode for an app",
			CronPropertyTask: CronPropertyTask{
				App:      "node-js-app",
				Property: "maintenance",
				Value:    "true",
			},
		},
		{
			Name: "Setting the mailto address globally",
			CronPropertyTask: CronPropertyTask{
				Global:   true,
				Property: "mailto",
				Value:    "ops@example.com",
			},
		},
		{
			Name: "Clearing the maintenance mode for an app",
			CronPropertyTask: CronPropertyTask{
				App:      "node-js-app",
				Property: "maintenance",
			},
		},
	})
}

// Execute sets or unsets the cron property
func (t CronPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// cronPropertyKeys maps cron property names to the JSON keys emitted by
// `dokku cron:report --format json` on dokku 0.38.8+. mailfrom/mailto are
// global-only.
var cronPropertyKeys = map[string]PropertyKeys{
	"maintenance": {PerApp: "maintenance", Global: "global-maintenance"},
	"mailfrom":    {PerApp: "", Global: "global-mailfrom"},
	"mailto":      {PerApp: "", Global: "global-mailto"},
}

// Plan reports the drift the CronPropertyTask would produce.
func (t CronPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "cron:set", cronPropertyKeys)
}

// init registers the CronPropertyTask with the task registry
func init() {
	RegisterTask(&CronPropertyTask{})
}
