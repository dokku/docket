package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// schedulerK3sScopedPairsSpec captures the inputs the shared scheduler-k3s
// "key/value pairs scoped to (process_type, resource_type)" helpers need. The
// labels and annotations tasks both build one of these and delegate to the
// helpers below; only Kind differs (it is plugged into both the dokku
// subcommand name and the user-facing pluralization in plan messages).
type schedulerK3sScopedPairsSpec struct {
	// Kind is the dokku subcommand noun ("labels" or "annotations").
	Kind         string
	App          string
	Global       bool
	ProcessType  string
	ResourceType string
	Pairs        map[string]string
}

// validateSchedulerK3sScopedPairs checks the common fields prior to any
// subprocess call. Error messages substitute the spec's noun so callers see
// "'labels' must not be empty" / "label keys must not be empty" etc.
func validateSchedulerK3sScopedPairs(spec schedulerK3sScopedPairsSpec, state State) error {
	if err := validateAppGlobalExclusive(spec.App, spec.Global); err != nil {
		return err
	}
	if spec.ResourceType == "" {
		return errors.New("resource_type is required")
	}
	if len(spec.Pairs) == 0 {
		effective := state
		if effective == "" {
			effective = StatePresent
		}
		return fmt.Errorf("'%s' must not be empty for state '%s'", spec.Kind, effective)
	}
	singular := singularizeSchedulerK3sKind(spec.Kind)
	for key := range spec.Pairs {
		if key == "" {
			return fmt.Errorf("%s keys must not be empty", singular)
		}
	}
	return nil
}

// planSchedulerK3sScopedPairsSet probes current pairs once, computes the diff,
// and embeds an apply closure that runs `dokku scheduler-k3s:<kind>:set` once
// per drifted key.
func planSchedulerK3sScopedPairsSet(spec schedulerK3sScopedPairsSpec) PlanResult {
	current, err := getSchedulerK3sScopedPairs(spec)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	drifted, allNew := driftedKeys(spec.Pairs, current)
	if len(drifted) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}

	status := PlanStatusModify
	if allNew {
		status = PlanStatusCreate
	}

	inputs := schedulerK3sScopedPairsSetInputs(spec, drifted)
	singular := singularizeSchedulerK3sKind(spec.Kind)
	return PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%d %s(s) to set", len(drifted), singular),
		Mutations: formatSetMutations(drifted, spec.Pairs, current),
		Commands:  resolveCommands(inputs),
		apply: func() TaskOutputState {
			return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
		},
	}
}

// planSchedulerK3sScopedPairsUnset probes current pairs once, computes the
// diff, and embeds an apply closure that clears each listed key that exists.
func planSchedulerK3sScopedPairsUnset(spec schedulerK3sScopedPairsSpec) PlanResult {
	current, err := getSchedulerK3sScopedPairs(spec)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	toClear := intersectingKeys(spec.Pairs, current)
	if len(toClear) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}

	inputs := schedulerK3sScopedPairsClearInputs(spec, toClear)
	singular := singularizeSchedulerK3sKind(spec.Kind)
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("%d %s(s) to unset", len(toClear), singular),
		Mutations: formatClearMutations(toClear, current),
		Commands:  resolveCommands(inputs),
		apply: func() TaskOutputState {
			return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
		},
	}
}

// schedulerK3sScopedPairsSetInputs returns one subprocess input per drifted
// key, each invoking `dokku scheduler-k3s:<kind>:set` with the desired value.
func schedulerK3sScopedPairsSetInputs(spec schedulerK3sScopedPairsSpec, keys []string) []subprocess.ExecCommandInput {
	inputs := make([]subprocess.ExecCommandInput, 0, len(keys))
	for _, key := range keys {
		inputs = append(inputs, schedulerK3sScopedPairsCommand(spec, key, spec.Pairs[key]))
	}
	return inputs
}

// schedulerK3sScopedPairsClearInputs returns one subprocess input per key to
// clear. Dokku interprets an empty value as a clear-this-key operation.
func schedulerK3sScopedPairsClearInputs(spec schedulerK3sScopedPairsSpec, keys []string) []subprocess.ExecCommandInput {
	inputs := make([]subprocess.ExecCommandInput, 0, len(keys))
	for _, key := range keys {
		inputs = append(inputs, schedulerK3sScopedPairsCommand(spec, key, ""))
	}
	return inputs
}

// schedulerK3sScopedPairsCommand builds one `dokku scheduler-k3s:<kind>:set`
// call.
func schedulerK3sScopedPairsCommand(spec schedulerK3sScopedPairsSpec, key, value string) subprocess.ExecCommandInput {
	args := []string{"--quiet", "scheduler-k3s:" + spec.Kind + ":set"}
	args = append(args, "--resource-type", spec.ResourceType)
	if spec.ProcessType != "" {
		args = append(args, "--process-type", spec.ProcessType)
	}
	if spec.Global {
		args = append(args, "--global", key, value)
	} else {
		args = append(args, spec.App, key, value)
	}
	return subprocess.ExecCommandInput{Command: "dokku", Args: args}
}

// getSchedulerK3sScopedPairs reads the pairs currently stored at the spec's
// (app|global, process_type, resource_type) scope. It calls
// `dokku scheduler-k3s:<kind>:report ... --format json`, which returns a flat
// map keyed by `<rendered_process_type>.<resource_type>.<key>`, and strips the
// prefix to recover the original keys.
func getSchedulerK3sScopedPairs(spec schedulerK3sScopedPairsSpec) (map[string]string, error) {
	args := []string{"--quiet", "scheduler-k3s:" + spec.Kind + ":report"}
	if spec.Global {
		args = append(args, "--global")
	} else {
		args = append(args, spec.App)
	}
	args = append(args, "--resource-type", spec.ResourceType)

	effectiveProcessType := spec.ProcessType
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
		return nil, fmt.Errorf("parse scheduler-k3s:%s:report json: %w", spec.Kind, err)
	}

	prefix := renderedSchedulerK3sProcessType(spec.ProcessType) + "." + spec.ResourceType + "."
	pairs := map[string]string{}
	for composedKey, value := range payload {
		if !strings.HasPrefix(composedKey, prefix) {
			continue
		}
		pairs[strings.TrimPrefix(composedKey, prefix)] = value
	}
	return pairs, nil
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

// singularizeSchedulerK3sKind returns the singular form of the kind noun for
// user-facing messages: "labels" -> "label", "annotations" -> "annotation".
func singularizeSchedulerK3sKind(kind string) string {
	return strings.TrimSuffix(kind, "s")
}
