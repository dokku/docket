package tasks

// SchedulerK3sAutoscalingAuthTask manages the KEDA TriggerAuthentication
// metadata stored under a single trigger for a dokku application or globally.
type SchedulerK3sAutoscalingAuthTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the trigger authentication should be
	// applied globally instead of on a single app.
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the trigger authentication should be applied globally"`

	// Trigger is the name of the KEDA trigger authentication resource that
	// groups the metadata keys.
	Trigger string `required:"true" yaml:"trigger" description:"Name of the KEDA trigger authentication resource"`

	// Metadata is the set of metadata key/value pairs to apply to the
	// trigger. On present, these keys are written and merged with any other
	// keys dokku already stores under the trigger. On absent, only the listed
	// keys are cleared; their values are ignored.
	Metadata map[string]string `required:"false" yaml:"metadata,omitempty" description:"Map of metadata key to value for the trigger authentication. On absent, only the keys are read."`

	// State is the desired state of the trigger authentication metadata.
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the trigger authentication metadata"`
}

// SchedulerK3sAutoscalingAuthTaskExample contains an example of a SchedulerK3sAutoscalingAuthTask
type SchedulerK3sAutoscalingAuthTaskExample struct {
	// Name is the task name holding the SchedulerK3sAutoscalingAuthTask description
	Name string `yaml:"-"`

	// SchedulerK3sAutoscalingAuthTask is the SchedulerK3sAutoscalingAuthTask configuration
	SchedulerK3sAutoscalingAuthTask SchedulerK3sAutoscalingAuthTask `yaml:"dokku_scheduler_k3s_autoscaling_auth"`
}

// GetName returns the name of the example
func (e SchedulerK3sAutoscalingAuthTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the scheduler-k3s autoscaling-auth task
func (t SchedulerK3sAutoscalingAuthTask) Doc() string {
	return "Manages KEDA TriggerAuthentication metadata grouped under a single trigger for a dokku application or globally"
}

// Examples returns the examples for the scheduler-k3s autoscaling-auth task
func (t SchedulerK3sAutoscalingAuthTask) Examples() ([]Doc, error) {
	return MarshalExamples([]SchedulerK3sAutoscalingAuthTaskExample{
		{
			Name: "Set AWS secret manager trigger metadata on an app",
			SchedulerK3sAutoscalingAuthTask: SchedulerK3sAutoscalingAuthTask{
				App:     "node-js-app",
				Trigger: "aws-secret-manager",
				Metadata: map[string]string{
					"awsRegion":  "us-east-1",
					"secretName": "my-secret",
				},
			},
		},
		{
			Name: "Set a global trigger authentication shared across apps",
			SchedulerK3sAutoscalingAuthTask: SchedulerK3sAutoscalingAuthTask{
				Global:  true,
				Trigger: "aws-secret-manager",
				Metadata: map[string]string{
					"awsRegion": "us-east-1",
					"roleArn":   "arn:aws:iam::123456789012:role/keda",
				},
			},
		},
		{
			Name: "Clear specific metadata keys from an app's trigger",
			SchedulerK3sAutoscalingAuthTask: SchedulerK3sAutoscalingAuthTask{
				App:     "node-js-app",
				Trigger: "aws-secret-manager",
				Metadata: map[string]string{
					"secretName": "",
				},
				State: StateAbsent,
			},
		},
	})
}

// Execute sets or clears the scheduler-k3s autoscaling trigger authentication metadata
func (t SchedulerK3sAutoscalingAuthTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the SchedulerK3sAutoscalingAuthTask would produce.
func (t SchedulerK3sAutoscalingAuthTask) Plan() PlanResult {
	spec := t.spec()
	if err := validateSchedulerK3sAutoscalingAuth(spec); err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planSchedulerK3sAutoscalingAuthSet(spec) },
		StateAbsent:  func() PlanResult { return planSchedulerK3sAutoscalingAuthUnset(spec) },
	})
}

// spec adapts the task to the trigger-auth spec the planners consume.
func (t SchedulerK3sAutoscalingAuthTask) spec() schedulerK3sAutoscalingAuthSpec {
	return schedulerK3sAutoscalingAuthSpec{
		App:      t.App,
		Global:   t.Global,
		Trigger:  t.Trigger,
		Metadata: t.Metadata,
	}
}

// init registers the SchedulerK3sAutoscalingAuthTask with the task registry
func init() {
	RegisterTask(&SchedulerK3sAutoscalingAuthTask{})
}
