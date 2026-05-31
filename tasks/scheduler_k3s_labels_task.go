package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dokku/docket/subprocess"
)

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
	if err := t.validate(); err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planSchedulerK3sLabelsSet(t) },
		StateAbsent:  func() PlanResult { return planSchedulerK3sLabelsUnset(t) },
	})
}

// validate checks the task fields prior to any subprocess call.
func (t SchedulerK3sLabelsTask) validate() error {
	if !t.Global && t.App == "" {
		return errors.New("app is required when global is false")
	}
	if t.Global && t.App != "" {
		return fmt.Errorf("'app' must not be set when 'global' is set to true")
	}
	if t.ResourceType == "" {
		return errors.New("resource_type is required")
	}
	if len(t.Labels) == 0 {
		state := t.State
		if state == "" {
			state = StatePresent
		}
		return fmt.Errorf("'labels' must not be empty for state '%s'", state)
	}
	for key := range t.Labels {
		if key == "" {
			return errors.New("label keys must not be empty")
		}
	}
	return nil
}

// planSchedulerK3sLabelsSet probes current labels once, computes the diff, and
// embeds an apply closure that runs `dokku scheduler-k3s:labels:set` once per
// drifted key.
func planSchedulerK3sLabelsSet(t SchedulerK3sLabelsTask) PlanResult {
	current, err := getSchedulerK3sLabels(t)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	driftedKeys := []string{}
	for k, v := range t.Labels {
		if cur, ok := current[k]; !ok || cur != v {
			driftedKeys = append(driftedKeys, k)
		}
	}
	if len(driftedKeys) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	sort.Strings(driftedKeys)

	mutations := make([]string, 0, len(driftedKeys))
	allNew := true
	for _, k := range driftedKeys {
		if _, ok := current[k]; ok {
			mutations = append(mutations, fmt.Sprintf("set %s=%s (was %q)", k, t.Labels[k], current[k]))
			allNew = false
		} else {
			mutations = append(mutations, fmt.Sprintf("set %s=%s (new)", k, t.Labels[k]))
		}
	}

	status := PlanStatusModify
	if allNew {
		status = PlanStatusCreate
	}

	inputs := schedulerK3sLabelsSetInputs(t, driftedKeys)
	return PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%d label(s) to set", len(driftedKeys)),
		Mutations: mutations,
		Commands:  resolveCommands(inputs),
		apply: func() TaskOutputState {
			return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
		},
	}
}

// planSchedulerK3sLabelsUnset probes current labels once, computes the diff,
// and embeds an apply closure that clears each listed key that exists.
func planSchedulerK3sLabelsUnset(t SchedulerK3sLabelsTask) PlanResult {
	current, err := getSchedulerK3sLabels(t)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	toClear := []string{}
	for k := range t.Labels {
		if _, ok := current[k]; ok {
			toClear = append(toClear, k)
		}
	}
	if len(toClear) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	sort.Strings(toClear)

	mutations := make([]string, 0, len(toClear))
	for _, k := range toClear {
		mutations = append(mutations, fmt.Sprintf("unset %s (was %q)", k, current[k]))
	}

	inputs := schedulerK3sLabelsClearInputs(t, toClear)
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("%d label(s) to unset", len(toClear)),
		Mutations: mutations,
		Commands:  resolveCommands(inputs),
		apply: func() TaskOutputState {
			return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
		},
	}
}

// schedulerK3sLabelsSetInputs returns one subprocess input per drifted key,
// each invoking `dokku scheduler-k3s:labels:set` with the desired value.
func schedulerK3sLabelsSetInputs(t SchedulerK3sLabelsTask, keys []string) []subprocess.ExecCommandInput {
	inputs := make([]subprocess.ExecCommandInput, 0, len(keys))
	for _, key := range keys {
		inputs = append(inputs, schedulerK3sLabelsCommand(t, key, t.Labels[key]))
	}
	return inputs
}

// schedulerK3sLabelsClearInputs returns one subprocess input per key to clear.
// Dokku interprets an empty value as a clear-this-label operation.
func schedulerK3sLabelsClearInputs(t SchedulerK3sLabelsTask, keys []string) []subprocess.ExecCommandInput {
	inputs := make([]subprocess.ExecCommandInput, 0, len(keys))
	for _, key := range keys {
		inputs = append(inputs, schedulerK3sLabelsCommand(t, key, ""))
	}
	return inputs
}

// schedulerK3sLabelsCommand builds one `dokku scheduler-k3s:labels:set` call.
func schedulerK3sLabelsCommand(t SchedulerK3sLabelsTask, key, value string) subprocess.ExecCommandInput {
	args := []string{"--quiet", "scheduler-k3s:labels:set"}
	args = append(args, "--resource-type", t.ResourceType)
	if t.ProcessType != "" {
		args = append(args, "--process-type", t.ProcessType)
	}
	if t.Global {
		args = append(args, "--global", key, value)
	} else {
		args = append(args, t.App, key, value)
	}
	return subprocess.ExecCommandInput{Command: "dokku", Args: args}
}

// getSchedulerK3sLabels reads the labels currently stored at the task's
// (app|global, process_type, resource_type) scope. It calls
// `dokku scheduler-k3s:labels:report ... --format json`, which returns a
// flat map keyed by `<rendered_process_type>.<resource_type>.<label_key>`,
// and strips the prefix to recover the original label keys.
func getSchedulerK3sLabels(t SchedulerK3sLabelsTask) (map[string]string, error) {
	args := []string{"--quiet", "scheduler-k3s:labels:report"}
	if t.Global {
		args = append(args, "--global")
	} else {
		args = append(args, t.App)
	}
	args = append(args, "--resource-type", t.ResourceType)

	effectiveProcessType := t.ProcessType
	if effectiveProcessType == "" {
		effectiveProcessType = "--global"
	}
	args = append(args, "--process-type", effectiveProcessType)
	args = append(args, "--format", "json")

	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    args,
	})
	if err != nil {
		return nil, err
	}

	payload := map[string]string{}
	if err := json.Unmarshal(result.StdoutBytes(), &payload); err != nil {
		return nil, fmt.Errorf("parse scheduler-k3s:labels:report json: %w", err)
	}

	prefix := renderedSchedulerK3sProcessType(t.ProcessType) + "." + t.ResourceType + "."
	labels := map[string]string{}
	for composedKey, value := range payload {
		if !strings.HasPrefix(composedKey, prefix) {
			continue
		}
		labels[strings.TrimPrefix(composedKey, prefix)] = value
	}
	return labels, nil
}

// renderedSchedulerK3sProcessType mirrors dokku's report-side rendering of the
// in-storage process type: the global sentinel "--global" (used when the task
// omits process_type) is rendered as "global"; explicit process types pass
// through unchanged.
func renderedSchedulerK3sProcessType(processType string) string {
	if processType == "" || processType == "--global" {
		return "global"
	}
	return processType
}

// init registers the SchedulerK3sLabelsTask with the task registry
func init() {
	RegisterTask(&SchedulerK3sLabelsTask{})
}
