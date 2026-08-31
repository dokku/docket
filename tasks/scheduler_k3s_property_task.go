package tasks

import "context"

// SchedulerK3sPropertyTask manages the scheduler-k3s configuration for a given dokku application
type SchedulerK3sPropertyTask PropertyFields

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

// ProbeSupport reports whether Plan() can read this task's current state.
func (t SchedulerK3sPropertyTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
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
				State:    StateAbsent,
			},
		},
	})
}

// Execute sets or unsets the scheduler-k3s property
func (t SchedulerK3sPropertyTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// schedulerK3sPropertyTable maps scheduler-k3s property names to the JSON
// keys emitted by `dokku scheduler-k3s:report --format json` on dokku
// 0.38.8+. The `chart.*.*` family is absent from Keys and declared under
// Rejected instead: chart value overrides are managed by
// dokku_scheduler_k3s_chart through dokku's dedicated
// scheduler-k3s:charts:set surface, so a recipe naming one is answered with
// the task that owns it rather than with the list of names this task supports.
var schedulerK3sPropertyTable = PropertyTable{
	Subcommand: "scheduler-k3s:set",
	Rejected: []RejectedPropertyFamily{
		{
			Prefix:      "chart.",
			Replacement: "dokku_scheduler_k3s_chart",
			Reason:      "the scheduler-k3s:set path for chart values is deprecated in dokku",
		},
	},
	Keys: map[string]PropertyKeys{
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
		"token":                  {PerApp: "", Global: "global-token", Sensitive: true},
	},
}

// PropertyTable returns the property schema this task manages.
func (t SchedulerK3sPropertyTask) PropertyTable() PropertyTable {
	return schedulerK3sPropertyTable
}

// Validate checks the SchedulerK3sPropertyTask's inputs without contacting the server.
func (t SchedulerK3sPropertyTask) Validate() error {
	return validatePropertyInput(t, t.State, t.App, t.Global, t.Property, t.Value)
}

// Plan reports the drift the SchedulerK3sPropertyTask would produce.
func (t SchedulerK3sPropertyTask) Plan(ctx context.Context) PlanResult {
	return planProperty(ctx, t, t.State, t.App, t.Global, t.Property, t.Value)
}

// ExportApp reconstructs the app's explicitly-set properties.
func (t SchedulerK3sPropertyTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	return exportProperties(ctx, t, app, func(app, property, value string) interface{} {
		return SchedulerK3sPropertyTask{App: app, Property: property, Value: value}
	})
}

// ExportGlobal reconstructs the globally-set properties.
func (t SchedulerK3sPropertyTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	return exportGlobalProperties(ctx, t, func(property, value string) interface{} {
		return SchedulerK3sPropertyTask{Global: true, Property: property, Value: value}
	})
}

// init registers the SchedulerK3sPropertyTask with the task registry
func init() {
	RegisterTask(&SchedulerK3sPropertyTask{})
}
