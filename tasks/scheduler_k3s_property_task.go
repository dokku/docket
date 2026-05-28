package tasks

// SchedulerK3sPropertyTask manages the scheduler-k3s configuration for a given dokku application
type SchedulerK3sPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app"`

	// Global is a flag indicating if the scheduler-k3s configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty"`

	// Property is the name of the scheduler-k3s property to set
	Property string `required:"true" yaml:"property"`

	// Value is the value to set for the scheduler-k3s property
	Value string `required:"false" yaml:"value,omitempty"`

	// State is the desired state of the scheduler-k3s configuration
	State State `required:"true" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// SchedulerK3sPropertyTaskExample contains an example of a SchedulerK3sPropertyTask
type SchedulerK3sPropertyTaskExample struct {
	// Name is the task name holding the SchedulerK3sPropertyTask description
	Name string `yaml:"-"`

	// SchedulerK3sPropertyTask is the SchedulerK3sPropertyTask configuration
	SchedulerK3sPropertyTask SchedulerK3sPropertyTask `yaml:"dokku_scheduler_k3s_property"`
}

// GetName returns the name of the example
func (e SchedulerK3sPropertyTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the scheduler-k3s property task
func (t SchedulerK3sPropertyTask) Doc() string {
	return "Manages the scheduler-k3s configuration for a given dokku application"
}

// Examples returns the examples for the scheduler-k3s property task
func (t SchedulerK3sPropertyTask) Examples() ([]Doc, error) {
	return MarshalExamples([]SchedulerK3sPropertyTaskExample{
		{
			Name: "Setting the deploy timeout for an app",
			SchedulerK3sPropertyTask: SchedulerK3sPropertyTask{
				App:      "node-js-app",
				Property: "deploy-timeout",
				Value:    "300s",
			},
		},
		{
			Name: "Setting the namespace for an app",
			SchedulerK3sPropertyTask: SchedulerK3sPropertyTask{
				App:      "node-js-app",
				Property: "namespace",
				Value:    "production",
			},
		},
		{
			Name: "Setting the letsencrypt prod email globally",
			SchedulerK3sPropertyTask: SchedulerK3sPropertyTask{
				Global:   true,
				Property: "letsencrypt-email-prod",
				Value:    "admin@example.com",
			},
		},
		{
			Name: "Clearing the namespace for an app",
			SchedulerK3sPropertyTask: SchedulerK3sPropertyTask{
				App:      "node-js-app",
				Property: "namespace",
			},
		},
	})
}

// Execute sets or unsets the scheduler-k3s property
func (t SchedulerK3sPropertyTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// schedulerK3sPropertyKeys maps scheduler-k3s property names to the JSON
// keys emitted by `dokku scheduler-k3s:report --format json` on dokku
// 0.38.8+. The `chart.*.*` family is dynamic and handled by
// isDynamicProperty without a map entry.
var schedulerK3sPropertyKeys = map[string]PropertyKeys{
	"deploy-timeout":         {PerApp: "deploy-timeout", Global: "global-deploy-timeout"},
	"image-pull-secrets":     {PerApp: "image-pull-secrets", Global: "global-image-pull-secrets"},
	"ingress-class":          {PerApp: "", Global: "global-ingress-class"},
	"kube-context":           {PerApp: "", Global: "global-kube-context"},
	"kubeconfig-path":        {PerApp: "", Global: "global-kubeconfig-path"},
	"kustomize-root-path":    {PerApp: "kustomize-root-path", Global: "global-kustomize-root-path"},
	"letsencrypt-email-prod": {PerApp: "", Global: "global-letsencrypt-email-prod"},
	"letsencrypt-email-stag": {PerApp: "", Global: "global-letsencrypt-email-stag"},
	"letsencrypt-server":     {PerApp: "letsencrypt-server", Global: "global-letsencrypt-server"},
	"namespace":              {PerApp: "namespace", Global: "global-namespace"},
	"network-interface":      {PerApp: "", Global: "global-network-interface"},
	"rollback-on-failure":    {PerApp: "rollback-on-failure", Global: "global-rollback-on-failure"},
	"shm-size":               {PerApp: "shm-size", Global: "global-shm-size"},
	"token":                  {PerApp: "", Global: "global-token"},
}

// Plan reports the drift the SchedulerK3sPropertyTask would produce.
func (t SchedulerK3sPropertyTask) Plan() PlanResult {
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "scheduler-k3s:set", schedulerK3sPropertyKeys)
}

// init registers the SchedulerK3sPropertyTask with the task registry
func init() {
	RegisterTask(&SchedulerK3sPropertyTask{})
}
