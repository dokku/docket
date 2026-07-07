package tasks

import (
	"fmt"
	"strings"
)

// SchedulerK3sPropertyTask manages the scheduler-k3s configuration for a given dokku application
type SchedulerK3sPropertyTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the scheduler-k3s configuration should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the scheduler-k3s configuration should be applied globally"`

	// Property is the name of the scheduler-k3s property to set
	Property string `required:"true" yaml:"property" description:"Name of the scheduler-k3s property to set"`

	// Value is the value to set for the scheduler-k3s property
	Value string `required:"false" yaml:"value,omitempty" description:"Value to set for the scheduler-k3s property"`

	// State is the desired state of the scheduler-k3s configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the scheduler-k3s configuration"`
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
	return "Manages the scheduler-k3s configuration for a given dokku application. chart.* properties are managed by dokku_scheduler_k3s_chart and rejected here, since dokku's scheduler-k3s:set path is deprecated for chart values."
}

// ExportSupport reports how docket export handles this task.
func (t SchedulerK3sPropertyTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
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
// 0.38.8+. The `chart.*.*` family is intentionally absent: chart value
// overrides are managed by dokku_scheduler_k3s_chart through dokku's
// dedicated scheduler-k3s:charts:set surface, and Plan() rejects any
// chart.* property before reaching this map.
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
	if strings.HasPrefix(t.Property, "chart.") {
		return PlanResult{
			Status: PlanStatusError,
			Error:  fmt.Errorf("chart.* properties are managed by dokku_scheduler_k3s_chart; the scheduler-k3s:set path for chart values is deprecated in dokku"),
		}
	}
	return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "scheduler-k3s:set", schedulerK3sPropertyKeys)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t SchedulerK3sPropertyTask) ExportApp(app string) ([]interface{}, error) {
	return exportProperties(app, "scheduler-k3s:set", schedulerK3sPropertyKeys, func(app, property, value string) interface{} {
		return SchedulerK3sPropertyTask{App: app, Property: property, Value: value}
	})
}

// init registers the SchedulerK3sPropertyTask with the task registry
func init() {
	RegisterTask(&SchedulerK3sPropertyTask{})
}
