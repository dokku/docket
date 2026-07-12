package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// schedulerK3sAutoscalingAuthSpec captures the inputs the trigger-auth
// planners need. The task layer adapts SchedulerK3sAutoscalingAuthTask into
// one of these and delegates to the helpers below; keeping the spec separate
// keeps the planners independently testable.
type schedulerK3sAutoscalingAuthSpec struct {
	App      string
	Global   bool
	Trigger  string
	Metadata map[string]string
}

// validateSchedulerK3sAutoscalingAuth checks the common fields prior to any
// subprocess call. Both present and absent states require Metadata: present
// sets the listed keys, absent names which keys to clear.
func validateSchedulerK3sAutoscalingAuth(spec schedulerK3sAutoscalingAuthSpec) error {
	if err := validateAppGlobalExclusive(spec.App, spec.Global); err != nil {
		return err
	}
	if spec.Trigger == "" {
		return errors.New("trigger is required")
	}
	if len(spec.Metadata) == 0 {
		return errors.New("'metadata' must not be empty")
	}
	for key := range spec.Metadata {
		if key == "" {
			return errors.New("metadata keys must not be empty")
		}
	}
	return nil
}

// planSchedulerK3sAutoscalingAuthSet probes the trigger's current metadata,
// computes the drifted keys, and emits one bulk `:set` call carrying a
// `--metadata k=v` flag per drifted key. Dokku's `:set` is an additive merge,
// so extra keys not in the spec are left alone.
func planSchedulerK3sAutoscalingAuthSet(spec schedulerK3sAutoscalingAuthSpec) PlanResult {
	current, err := getSchedulerK3sAutoscalingAuth(spec)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	registerSensitiveMapValues(current)

	drifted, allNew := driftedKeys(spec.Metadata, current)
	if len(drifted) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}

	status := PlanStatusModify
	if allNew {
		status = PlanStatusCreate
	}

	inputs := []subprocess.ExecCommandInput{
		schedulerK3sAutoscalingAuthSetCommand(spec, drifted),
	}
	return PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%d metadata key(s) to set", len(drifted)),
		Mutations: formatSetMutations(drifted, spec.Metadata, current),
		Commands:  resolveCommands(inputs),
		apply: func() TaskOutputState {
			return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
		},
	}
}

// planSchedulerK3sAutoscalingAuthUnset probes the trigger's current metadata
// and emits a wipe-and-restore: dokku's `:autoscaling-auth:set` has no
// per-key delete (unlike labels/annotations, where empty value deletes), so
// the only public-CLI route to clearing specific keys is to wipe the whole
// trigger and re-set the keys the task does NOT name. Any key not in the
// current map is skipped (already absent).
func planSchedulerK3sAutoscalingAuthUnset(spec schedulerK3sAutoscalingAuthSpec) PlanResult {
	current, err := getSchedulerK3sAutoscalingAuth(spec)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	registerSensitiveMapValues(current)

	toClear := intersectingKeys(spec.Metadata, current)
	if len(toClear) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}

	clearSet := make(map[string]struct{}, len(toClear))
	for _, k := range toClear {
		clearSet[k] = struct{}{}
	}
	survivors := map[string]string{}
	survivorKeys := []string{}
	for k, v := range current {
		if _, found := clearSet[k]; found {
			continue
		}
		survivors[k] = v
		survivorKeys = append(survivorKeys, k)
	}
	sort.Strings(survivorKeys)

	mutations := formatClearMutations(toClear, current)
	for _, k := range survivorKeys {
		mutations = append(mutations, fmt.Sprintf("restore %s=%s", k, survivors[k]))
	}

	inputs := []subprocess.ExecCommandInput{
		schedulerK3sAutoscalingAuthClearCommand(spec),
	}
	if len(survivorKeys) > 0 {
		inputs = append(inputs, schedulerK3sAutoscalingAuthSetCommandWithMap(spec, survivorKeys, survivors))
	}

	return PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("%d metadata key(s) to unset", len(toClear)),
		Mutations: mutations,
		Commands:  resolveCommands(inputs),
		apply: func() TaskOutputState {
			return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
		},
	}
}

// schedulerK3sAutoscalingAuthSetCommand builds a single
// `dokku scheduler-k3s:autoscaling-auth:set <app|--global> <trigger>
// --metadata k=v ...` call carrying one `--metadata` flag per key in keys.
func schedulerK3sAutoscalingAuthSetCommand(spec schedulerK3sAutoscalingAuthSpec, keys []string) subprocess.ExecCommandInput {
	return schedulerK3sAutoscalingAuthSetCommandWithMap(spec, keys, spec.Metadata)
}

// schedulerK3sAutoscalingAuthSetCommandWithMap is the underlying builder used
// by both the present-state set and the absent-state restore call. The
// restore path passes the survivors map instead of spec.Metadata.
func schedulerK3sAutoscalingAuthSetCommandWithMap(spec schedulerK3sAutoscalingAuthSpec, keys []string, values map[string]string) subprocess.ExecCommandInput {
	args := []string{"--quiet", "scheduler-k3s:autoscaling-auth:set"}
	if spec.Global {
		args = append(args, "--global", spec.Trigger)
	} else {
		args = append(args, spec.App, spec.Trigger)
	}
	for _, k := range keys {
		args = append(args, "--metadata", fmt.Sprintf("%s=%s", k, values[k]))
	}
	return subprocess.ExecCommandInput{Command: "dokku", Args: args}
}

// schedulerK3sAutoscalingAuthClearCommand builds the bare `:set <app|--global>
// <trigger>` call dokku interprets as "wipe every metadata key under this
// trigger".
func schedulerK3sAutoscalingAuthClearCommand(spec schedulerK3sAutoscalingAuthSpec) subprocess.ExecCommandInput {
	args := []string{"--quiet", "scheduler-k3s:autoscaling-auth:set"}
	if spec.Global {
		args = append(args, "--global", spec.Trigger)
	} else {
		args = append(args, spec.App, spec.Trigger)
	}
	return subprocess.ExecCommandInput{Command: "dokku", Args: args}
}

// getSchedulerK3sAutoscalingAuth reads the metadata currently stored for the
// spec's trigger. It calls
// `dokku scheduler-k3s:autoscaling-auth:report <app|--global> --format json`,
// which returns a flat map keyed `<trigger>.<metadata_key>` carrying the real
// metadata values (dokku only masks stdout and single-flag output, never the
// JSON payload). We keep the entries under our trigger and strip the
// `<trigger>.` prefix to recover the original metadata keys.
func getSchedulerK3sAutoscalingAuth(spec schedulerK3sAutoscalingAuthSpec) (map[string]string, error) {
	args := []string{"--quiet", "scheduler-k3s:autoscaling-auth:report"}
	if spec.Global {
		args = append(args, "--global")
	} else {
		args = append(args, spec.App)
	}
	args = append(args, "--format", "json")

	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    args,
	})
	if err != nil {
		return nil, err
	}

	return parseSchedulerK3sAutoscalingAuthReport(result.StdoutBytes(), spec.Trigger)
}

// parseSchedulerK3sAutoscalingAuthReport decodes the flat
// `scheduler-k3s:autoscaling-auth:report --format json` payload and returns the
// metadata for a single trigger keyed by the original metadata key. The
// composed keys are `<trigger>.<metadata_key>`; the trigger is dokku's first
// segment and never contains a dot, but a metadata key may, so we strip the
// `<trigger>.` prefix rather than splitting. Kept separate from the subprocess
// call so the parse path is unit-testable without a fake executor.
func parseSchedulerK3sAutoscalingAuthReport(raw []byte, trigger string) (map[string]string, error) {
	payload := map[string]string{}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("parse scheduler-k3s:autoscaling-auth:report json: %w", err)
	}

	prefix := trigger + "."
	metadata := map[string]string{}
	for composedKey, value := range payload {
		if !strings.HasPrefix(composedKey, prefix) {
			continue
		}
		metadata[strings.TrimPrefix(composedKey, prefix)] = value
	}
	return metadata, nil
}

// exportSchedulerK3sAutoscalingAuth reconstructs the trigger-auth task bodies
// for one scope (a single app, or the global scope when global is true) from
// `scheduler-k3s:autoscaling-auth:report --format json`. The flat payload is
// grouped by trigger and build turns each (trigger, metadata) group into a task
// body. Non-SSH errors and unparseable output are swallowed (return nil) so a
// host without scheduler-k3s state does not fail the whole export, mirroring
// the profile and chart exporters.
func exportSchedulerK3sAutoscalingAuth(app string, global bool, build func(trigger string, metadata map[string]string) interface{}) ([]interface{}, error) {
	target := app
	if global {
		target = "--global"
	}

	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "scheduler-k3s:autoscaling-auth:report", target, "--format", "json"},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return nil, nil
	}

	payload := map[string]string{}
	if err := json.Unmarshal(result.StdoutBytes(), &payload); err != nil {
		return nil, nil
	}

	byTrigger := map[string]map[string]string{}
	for composed, value := range payload {
		parts := strings.SplitN(composed, ".", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			continue
		}
		trigger, key := parts[0], parts[1]
		if byTrigger[trigger] == nil {
			byTrigger[trigger] = map[string]string{}
		}
		byTrigger[trigger][key] = value
	}

	triggers := make([]string, 0, len(byTrigger))
	for trigger := range byTrigger {
		triggers = append(triggers, trigger)
	}
	sort.Strings(triggers)

	var out []interface{}
	for _, trigger := range triggers {
		out = append(out, build(trigger, byTrigger[trigger]))
	}
	return out, nil
}
