package tasks

import "context"

// SchedulerK3sLabelsTask manages a group of scheduler-k3s labels scoped to a
// (process_type, resource_type) pair on a dokku application or globally.
type SchedulerK3sLabelsTask struct {
	// App is the name of the app. Exactly one of App and Global is set.
	App string `required:"false" identity:"key" yaml:"app" description:"Name of the app"`

	// Global is a flag indicating if the labels should be applied globally
	Global bool `required:"false" identity:"key" yaml:"global,omitempty" description:"Flag indicating if the labels should be applied globally"`

	// ProcessType narrows the labels to a specific process type. When empty,
	// dokku stores the labels under its default global process type.
	ProcessType string `required:"false" identity:"key" yaml:"process_type,omitempty" description:"Process type to scope the labels to. Defaults to the global process type when empty."`

	// ResourceType narrows the labels to a specific kubernetes resource type
	// (e.g. deployment, ingress, service). Required, mirroring dokku's own
	// scheduler-k3s:labels:set rejection of empty resource types.
	ResourceType string `required:"true" identity:"key" yaml:"resource_type" description:"Kubernetes resource type to scope the labels to (e.g. deployment, ingress)."`

	// Labels is the desired set of label key/value pairs to apply at the
	// (process_type, resource_type) scope.
	Labels map[string]string `required:"false" required_when:"present,absent" forbidden_when:"clear" identity:"collection" yaml:"labels,omitempty" description:"Map of label key to value to apply at the scope. Under state 'set' a key must not contain '=', and an empty value is stored rather than clearing the key."`

	// State is the desired state of the labels. 'present' and 'absent' are
	// additive, naming keys to write or clear; 'set' declares the complete map
	// for the scope and 'clear' empties it.
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent,set,clear" description:"Desired state of the labels. 'set' declares the complete map for this (process_type, resource_type) scope, removing any label the recipe does not name; 'clear' empties that scope alone, leaving other scopes on the app untouched."`
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
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t SchedulerK3sLabelsTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// MinimumVersions reports the oldest Dokku this task's probe parses.
// The floor is 0.38.31, the release the integration suite runs; the probe is `scheduler-k3s:labels:report`.
func (t SchedulerK3sLabelsTask) MinimumVersions() VersionSupport {
	return VersionSupport{Dokku: "0.38.31"}
}

// examples returns the examples for the scheduler-k3s labels task
func (t SchedulerK3sLabelsTask) examples() ([]Doc, error) {
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
		{
			Name: "Replace the deployment labels on an app's web process",
			SchedulerK3sLabelsTask: SchedulerK3sLabelsTask{
				App:          "node-js-app",
				ProcessType:  "web",
				ResourceType: "deployment",
				Labels: map[string]string{
					"managed-by": "docket",
				},
				State: StateSet,
			},
		},
		{
			Name: "Clear the deployment labels from an app's web process",
			SchedulerK3sLabelsTask: SchedulerK3sLabelsTask{
				App:          "node-js-app",
				ProcessType:  "web",
				ResourceType: "deployment",
				State:        StateClear,
			},
		},
	})
}

// Execute sets or clears the scheduler-k3s labels for the configured scope
func (t SchedulerK3sLabelsTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// Validate checks the SchedulerK3sLabelsTask's inputs without contacting the server.
func (t SchedulerK3sLabelsTask) Validate() error {
	if err := checkConstraints(t); err != nil {
		return err
	}
	return validateSchedulerK3sScopedPairs(t.spec(), t.State)
}

// Constraints declares that the labels belong to an app or are global.
func (t SchedulerK3sLabelsTask) Constraints() []Constraint {
	return appOrGlobalConstraints
}

// Plan reports the drift the SchedulerK3sLabelsTask would produce.
func (t SchedulerK3sLabelsTask) Plan(ctx context.Context) PlanResult {
	if r, ok := gatePlan(ctx, t); !ok {
		return r
	}
	spec := t.spec()
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planSchedulerK3sScopedPairsPresent(ctx, spec) },
		StateAbsent:  func() PlanResult { return planSchedulerK3sScopedPairsAbsent(ctx, spec) },
		StateSet:     func() PlanResult { return planSchedulerK3sScopedPairsSet(ctx, spec) },
		StateClear:   func() PlanResult { return planSchedulerK3sScopedPairsClear(ctx, spec) },
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
		Observe: func(pairs map[string]string, state State) Task {
			return SchedulerK3sLabelsTask{App: t.App, Global: t.Global, ProcessType: t.ProcessType, ResourceType: t.ResourceType, Labels: pairs, State: state}
		},
	}
}

// ExportApp reconstructs the app's labels, one task per
// (process_type, resource_type) scope, from scheduler-k3s:labels:report.
// state:set replaces each scope's whole map, so a re-applied export reproduces
// the exact set rather than merging into whatever the target already carries.
func (t SchedulerK3sLabelsTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	return exportSchedulerK3sScopedPairs(ctx, "labels", app, false, func(processType, resourceType string, pairs map[string]string) interface{} {
		return SchedulerK3sLabelsTask{
			App:          app,
			ProcessType:  processType,
			ResourceType: resourceType,
			Labels:       pairs,
			State:        StateSet,
		}
	})
}

// ExportGlobal reconstructs the global-scope labels, one task per
// (process_type, resource_type) scope, from scheduler-k3s:labels:report.
func (t SchedulerK3sLabelsTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	return exportSchedulerK3sScopedPairs(ctx, "labels", "", true, func(processType, resourceType string, pairs map[string]string) interface{} {
		return SchedulerK3sLabelsTask{
			Global:       true,
			ProcessType:  processType,
			ResourceType: resourceType,
			Labels:       pairs,
			State:        StateSet,
		}
	})
}

// init registers the SchedulerK3sLabelsTask with the task registry
func init() {
	RegisterTask(&SchedulerK3sLabelsTask{})
}
