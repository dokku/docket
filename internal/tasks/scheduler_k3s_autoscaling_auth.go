package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dokku/docket/internal/subprocess"
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
// subprocess call. Present, absent and set all require Metadata: present sets
// the listed keys, absent names which keys to clear, and set declares the
// trigger's complete map. Clear takes none.
func validateSchedulerK3sAutoscalingAuth(spec schedulerK3sAutoscalingAuthSpec, state State) error {
	if err := validateAppGlobalExclusive(spec.App, spec.Global); err != nil {
		return err
	}
	if spec.Trigger == "" {
		return errors.New("trigger is required")
	}
	effective := state
	if effective == "" {
		effective = StatePresent
	}

	// The clear state wipes the trigger, so a map supplied alongside it would
	// be silently discarded rather than written.
	if effective == StateClear {
		if len(spec.Metadata) > 0 {
			return errors.New("'metadata' must not be set for state 'clear'")
		}
		return nil
	}
	if len(spec.Metadata) == 0 {
		return fmt.Errorf("'metadata' must not be empty for state '%s'", effective)
	}

	for _, key := range sortedPairKeys(spec.Metadata) {
		if key == "" {
			return errors.New("metadata keys must not be empty")
		}
		// Unlike the annotations and labels commands, every autoscaling-auth
		// state carries its keys in a `--metadata key=value` flag, which dokku
		// splits on the first "=". A key containing one would be truncated and
		// its value mangled, so it is rejected wherever keys are written.
		if (effective == StatePresent || effective == StateSet) && strings.Contains(key, "=") {
			return fmt.Errorf("metadata keys must not contain '=' for state '%s', got %q", effective, key)
		}
		// dokku clears a metadata key set to an empty value, so a present-state
		// empty value can never converge (issue #358); clear via state 'absent'.
		// State 'set' goes through :set --replace, which writes the declared map
		// wholesale, so an empty value is stored there instead.
		if effective == StatePresent && spec.Metadata[key] == "" {
			return errors.New("metadata values must not be empty for state 'present'; use state 'absent' to clear a key")
		}
	}
	return nil
}

// planSchedulerK3sAutoscalingAuthPresent probes the trigger's current metadata,
// computes the drifted keys, and emits one bulk `:set` call carrying a
// `--metadata k=v` flag per drifted key. Dokku's `:set` is an additive merge,
// so extra keys not in the spec are left alone; state 'set' is what overrides
// that.
func planSchedulerK3sAutoscalingAuthPresent(ctx context.Context, spec schedulerK3sAutoscalingAuthSpec) PlanResult {
	current, err := getSchedulerK3sAutoscalingAuth(ctx, spec)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	registerSensitiveMapValues(ctx, current)

	drifted, allNew := driftedKeys(spec.Metadata, current)
	if len(drifted) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}

	status := PlanStatusModify
	if allNew {
		status = PlanStatusCreate
	}

	inputs := []subprocess.ExecCommandInput{
		schedulerK3sAutoscalingAuthSetCommandWithMap(spec, drifted, spec.Metadata, false),
	}
	return PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%d metadata key(s) to set", len(drifted)),
		Mutations: formatSetMutations(drifted, spec.Metadata, current),
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
		},
	}
}

// planSchedulerK3sAutoscalingAuthAbsent probes the trigger's current metadata
// and re-declares it without the keys the task names. Dokku's
// `:autoscaling-auth:set` has no per-key delete (unlike labels and annotations,
// where an empty value deletes), so the survivors have to be restated; what
// `--replace` buys is doing that in the same call that drops the rest, instead
// of a wipe followed by a restore that could fail on its own and take every key
// with it. A key not in the current map is skipped (already absent).
func planSchedulerK3sAutoscalingAuthAbsent(ctx context.Context, spec schedulerK3sAutoscalingAuthSpec) PlanResult {
	current, err := getSchedulerK3sAutoscalingAuth(ctx, spec)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	registerSensitiveMapValues(ctx, current)

	toClear := intersectingKeys(spec.Metadata, current)
	if len(toClear) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}

	clearSet := make(map[string]struct{}, len(toClear))
	for _, k := range toClear {
		clearSet[k] = struct{}{}
	}
	survivors := map[string]string{}
	for k, v := range current {
		if _, found := clearSet[k]; found {
			continue
		}
		survivors[k] = v
	}
	survivorKeys := sortedPairKeys(survivors)

	// With no survivors there is nothing to declare, and `--replace` rejects an
	// empty `--metadata` list, so the bare `:set` that wipes the trigger is the
	// call to make.
	input := schedulerK3sAutoscalingAuthClearCommand(spec)
	if len(survivorKeys) > 0 {
		input = schedulerK3sAutoscalingAuthSetCommandWithMap(spec, survivorKeys, survivors, true)
	}
	inputs := []subprocess.ExecCommandInput{input}

	return PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("%d metadata key(s) to unset", len(toClear)),
		Mutations: formatClearMutations(toClear, current),
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateAbsent, inputs)
		},
	}
}

// schedulerK3sAutoscalingAuthCurrent reads the trigger's stored metadata and
// registers the probed values with the masker, so a secret dokku returned never
// reaches user-facing output. Every planner probes through this.
func schedulerK3sAutoscalingAuthCurrent(spec schedulerK3sAutoscalingAuthSpec) pairsCurrentFunc {
	return func(ctx context.Context) (map[string]string, error) {
		current, err := getSchedulerK3sAutoscalingAuth(ctx, spec)
		if err != nil {
			return nil, err
		}
		registerSensitiveMapValues(ctx, current)
		return current, nil
	}
}

// planSchedulerK3sAutoscalingAuthSet delegates to planPairsReplace. `:set
// --replace` drops the trigger's stored metadata and writes the declared map in
// one call, so the trigger is never left carrying a mixture of the two sets.
func planSchedulerK3sAutoscalingAuthSet(ctx context.Context, spec schedulerK3sAutoscalingAuthSpec) PlanResult {
	return planPairsReplace(ctx,
		"metadata key",
		spec.Metadata,
		schedulerK3sAutoscalingAuthCurrent(spec),
		func() subprocess.ExecCommandInput {
			return schedulerK3sAutoscalingAuthSetCommandWithMap(spec, sortedPairKeys(spec.Metadata), spec.Metadata, true)
		},
	)
}

// planSchedulerK3sAutoscalingAuthClear delegates to planPairsClear; the bare
// `:set` with no `--metadata` is what dokku reads as "wipe this trigger", since
// there is no `:autoscaling-auth:clear` subcommand.
func planSchedulerK3sAutoscalingAuthClear(ctx context.Context, spec schedulerK3sAutoscalingAuthSpec) PlanResult {
	return planPairsClear(ctx,
		"metadata key",
		schedulerK3sAutoscalingAuthCurrent(spec),
		func() subprocess.ExecCommandInput { return schedulerK3sAutoscalingAuthClearCommand(spec) },
	)
}

// schedulerK3sAutoscalingAuthSetCommandWithMap builds a single
// `dokku scheduler-k3s:autoscaling-auth:set [--replace] <app|--global>
// <trigger> --metadata k=v ...` call carrying one `--metadata` flag per key in
// keys. With replace set, dokku drops the trigger's stored metadata before
// writing, making the result exactly what was passed; without it the flags are
// merged into what is already there. The restore path passes the survivors map
// instead of spec.Metadata.
func schedulerK3sAutoscalingAuthSetCommandWithMap(spec schedulerK3sAutoscalingAuthSpec, keys []string, values map[string]string, replace bool) subprocess.ExecCommandInput {
	args := []string{"--quiet", "scheduler-k3s:autoscaling-auth:set"}
	if replace {
		args = append(args, "--replace")
	}
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
// trigger". There is no `:autoscaling-auth:clear` subcommand.
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
func getSchedulerK3sAutoscalingAuth(ctx context.Context, spec schedulerK3sAutoscalingAuthSpec) (map[string]string, error) {
	args := []string{"--quiet", "scheduler-k3s:autoscaling-auth:report"}
	if spec.Global {
		args = append(args, "--global")
	} else {
		args = append(args, spec.App)
	}
	args = append(args, "--format", "json")

	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
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
func exportSchedulerK3sAutoscalingAuth(ctx context.Context, app string, global bool, build func(trigger string, metadata map[string]string) interface{}) ([]interface{}, error) {
	target := app
	if global {
		target = "--global"
	}

	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
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
