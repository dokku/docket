package tasks

// SchedulerK3sAnnotationsTask manages a group of scheduler-k3s annotations
// scoped to a (process_type, resource_type) pair on a dokku application or
// globally.
type SchedulerK3sAnnotationsTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the annotations should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the annotations should be applied globally"`

	// ProcessType narrows the annotations to a specific process type. When
	// empty, dokku stores the annotations under its default global process
	// type.
	ProcessType string `required:"false" yaml:"process_type,omitempty" description:"Process type to scope the annotations to. Defaults to the global process type when empty."`

	// ResourceType narrows the annotations to a specific kubernetes resource
	// type (e.g. deployment, ingress, service). Required, mirroring dokku's
	// own scheduler-k3s:annotations:set rejection of empty resource types.
	ResourceType string `required:"true" yaml:"resource_type" description:"Kubernetes resource type to scope the annotations to (e.g. deployment, ingress)."`

	// Annotations is the desired set of annotation key/value pairs to apply at
	// the (process_type, resource_type) scope.
	Annotations map[string]string `required:"false" yaml:"annotations,omitempty" description:"Map of annotation key to value to apply at the scope."`

	// State is the desired state of the annotations
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the annotations"`
}

// SchedulerK3sAnnotationsTaskExample contains an example of a SchedulerK3sAnnotationsTask
type SchedulerK3sAnnotationsTaskExample struct {
	// Name is the task name holding the SchedulerK3sAnnotationsTask description
	Name string `yaml:"-"`

	// SchedulerK3sAnnotationsTask is the SchedulerK3sAnnotationsTask configuration
	SchedulerK3sAnnotationsTask SchedulerK3sAnnotationsTask `yaml:"dokku_scheduler_k3s_annotations"`
}

// GetName returns the name of the example
func (e SchedulerK3sAnnotationsTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the scheduler-k3s annotations task
func (t SchedulerK3sAnnotationsTask) Doc() string {
	return "Manages scheduler-k3s annotations scoped to a (process_type, resource_type) pair for a dokku application or globally"
}

// ExportSupport reports how docket export handles this task.
func (t SchedulerK3sAnnotationsTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Examples returns the examples for the scheduler-k3s annotations task
func (t SchedulerK3sAnnotationsTask) Examples() ([]Doc, error) {
	return MarshalExamples([]SchedulerK3sAnnotationsTaskExample{
		{
			Name: "Set deployment annotations on an app's web process",
			SchedulerK3sAnnotationsTask: SchedulerK3sAnnotationsTask{
				App:          "node-js-app",
				ProcessType:  "web",
				ResourceType: "deployment",
				Annotations: map[string]string{
					"prometheus.io/scrape": "true",
					"prometheus.io/port":   "9090",
				},
			},
		},
		{
			Name: "Set ingress annotations on an app at the global process scope",
			SchedulerK3sAnnotationsTask: SchedulerK3sAnnotationsTask{
				App:          "node-js-app",
				ResourceType: "ingress",
				Annotations: map[string]string{
					"nginx.ingress.kubernetes.io/rewrite-target": "/",
				},
			},
		},
		{
			Name: "Set a global deployment annotation across all apps",
			SchedulerK3sAnnotationsTask: SchedulerK3sAnnotationsTask{
				Global:       true,
				ResourceType: "deployment",
				Annotations: map[string]string{
					"managed-by": "docket",
				},
			},
		},
		{
			Name: "Remove specific annotations from an app's deployment",
			SchedulerK3sAnnotationsTask: SchedulerK3sAnnotationsTask{
				App:          "node-js-app",
				ResourceType: "deployment",
				Annotations: map[string]string{
					"prometheus.io/scrape": "",
				},
				State: StateAbsent,
			},
		},
	})
}

// Execute sets or clears the scheduler-k3s annotations for the configured scope
func (t SchedulerK3sAnnotationsTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Validate checks the SchedulerK3sAnnotationsTask's inputs without contacting the server.
func (t SchedulerK3sAnnotationsTask) Validate() error {
	return validateSchedulerK3sScopedPairs(t.spec(), t.State)
}

// Plan reports the drift the SchedulerK3sAnnotationsTask would produce.
func (t SchedulerK3sAnnotationsTask) Plan() PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	spec := t.spec()
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planSchedulerK3sScopedPairsSet(spec) },
		StateAbsent:  func() PlanResult { return planSchedulerK3sScopedPairsUnset(spec) },
	})
}

// spec adapts the task to the kind-agnostic scoped-pairs spec shared with the
// labels task.
func (t SchedulerK3sAnnotationsTask) spec() schedulerK3sScopedPairsSpec {
	return schedulerK3sScopedPairsSpec{
		Kind:         "annotations",
		App:          t.App,
		Global:       t.Global,
		ProcessType:  t.ProcessType,
		ResourceType: t.ResourceType,
		Pairs:        t.Annotations,
	}
}

// ExportApp reconstructs the app's annotations, one task per
// (process_type, resource_type) scope, from scheduler-k3s:annotations:report.
func (t SchedulerK3sAnnotationsTask) ExportApp(app string) ([]interface{}, error) {
	return exportSchedulerK3sScopedPairs("annotations", app, false, func(processType, resourceType string, pairs map[string]string) interface{} {
		return SchedulerK3sAnnotationsTask{
			App:          app,
			ProcessType:  processType,
			ResourceType: resourceType,
			Annotations:  pairs,
		}
	})
}

// ExportGlobal reconstructs the global-scope annotations, one task per
// (process_type, resource_type) scope, from scheduler-k3s:annotations:report.
func (t SchedulerK3sAnnotationsTask) ExportGlobal() ([]interface{}, error) {
	return exportSchedulerK3sScopedPairs("annotations", "", true, func(processType, resourceType string, pairs map[string]string) interface{} {
		return SchedulerK3sAnnotationsTask{
			Global:       true,
			ProcessType:  processType,
			ResourceType: resourceType,
			Annotations:  pairs,
		}
	})
}

// init registers the SchedulerK3sAnnotationsTask with the task registry
func init() {
	RegisterTask(&SchedulerK3sAnnotationsTask{})
}
