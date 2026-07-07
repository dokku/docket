package tasks

// SchedulerK3sLabelsTask manages a group of scheduler-k3s labels scoped to a
// (process_type, resource_type) pair on a dokku application or globally.
type SchedulerK3sLabelsTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the labels should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the labels should be applied globally"`

	// ProcessType narrows the labels to a specific process type. When empty,
	// dokku stores the labels under its default global process type.
	ProcessType string `required:"false" yaml:"process_type,omitempty" description:"Process type to scope the labels to. Defaults to the global process type when empty."`

	// ResourceType narrows the labels to a specific kubernetes resource type
	// (e.g. deployment, ingress, service). Required, mirroring dokku's own
	// scheduler-k3s:labels:set rejection of empty resource types.
	ResourceType string `required:"true" yaml:"resource_type" description:"Kubernetes resource type to scope the labels to (e.g. deployment, ingress)."`

	// Labels is the desired set of label key/value pairs to apply at the
	// (process_type, resource_type) scope.
	Labels map[string]string `required:"false" yaml:"labels,omitempty" description:"Map of label key to value to apply at the scope."`

	// State is the desired state of the labels
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the labels"`
}

// SchedulerK3sLabelsTaskExample contains an example of a SchedulerK3sLabelsTask
type SchedulerK3sLabelsTaskExample struct {
	// Name is the task name holding the SchedulerK3sLabelsTask description
	Name string `yaml:"-"`

	// SchedulerK3sLabelsTask is the SchedulerK3sLabelsTask configuration
	SchedulerK3sLabelsTask SchedulerK3sLabelsTask `yaml:"dokku_scheduler_k3s_labels"`
}

// GetName returns the name of the example
func (e SchedulerK3sLabelsTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the scheduler-k3s labels task
func (t SchedulerK3sLabelsTask) Doc() string {
	return "Manages scheduler-k3s labels scoped to a (process_type, resource_type) pair for a dokku application or globally"
}

// ExportSupport reports how docket export handles this task.
func (t SchedulerK3sLabelsTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportUnsupported, Caveat: "scheduler-k3s exposes no report for labels, so the current state cannot be read back (docket#287, dokku/dokku#8800)"}
}

// Examples returns the examples for the scheduler-k3s labels task
func (t SchedulerK3sLabelsTask) Examples() ([]Doc, error) {
	return MarshalExamples([]SchedulerK3sLabelsTaskExample{
		{
			Name: "Set deployment labels on an app's web process",
			SchedulerK3sLabelsTask: SchedulerK3sLabelsTask{
				App:          "node-js-app",
				ProcessType:  "web",
				ResourceType: "deployment",
				Labels: map[string]string{
					"app.kubernetes.io/component": "api",
					"tier":                        "edge",
				},
			},
		},
		{
			Name: "Set ingress labels on an app at the global process scope",
			SchedulerK3sLabelsTask: SchedulerK3sLabelsTask{
				App:          "node-js-app",
				ResourceType: "ingress",
				Labels: map[string]string{
					"team": "platform",
				},
			},
		},
		{
			Name: "Set a global deployment label across all apps",
			SchedulerK3sLabelsTask: SchedulerK3sLabelsTask{
				Global:       true,
				ResourceType: "deployment",
				Labels: map[string]string{
					"managed-by": "docket",
				},
			},
		},
		{
			Name: "Remove specific labels from an app's deployment",
			SchedulerK3sLabelsTask: SchedulerK3sLabelsTask{
				App:          "node-js-app",
				ResourceType: "deployment",
				Labels: map[string]string{
					"tier": "",
				},
				State: StateAbsent,
			},
		},
	})
}

// Execute sets or clears the scheduler-k3s labels for the configured scope
func (t SchedulerK3sLabelsTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the SchedulerK3sLabelsTask would produce.
func (t SchedulerK3sLabelsTask) Plan() PlanResult {
	spec := t.spec()
	if err := validateSchedulerK3sScopedPairs(spec, t.State); err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planSchedulerK3sScopedPairsSet(spec) },
		StateAbsent:  func() PlanResult { return planSchedulerK3sScopedPairsUnset(spec) },
	})
}

// spec adapts the task to the kind-agnostic scoped-pairs spec shared with the
// annotations task.
func (t SchedulerK3sLabelsTask) spec() schedulerK3sScopedPairsSpec {
	return schedulerK3sScopedPairsSpec{
		Kind:         "labels",
		App:          t.App,
		Global:       t.Global,
		ProcessType:  t.ProcessType,
		ResourceType: t.ResourceType,
		Pairs:        t.Labels,
	}
}

// init registers the SchedulerK3sLabelsTask with the task registry
func init() {
	RegisterTask(&SchedulerK3sLabelsTask{})
}
