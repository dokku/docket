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
// labels and annotations tasks both build one of these; only Kind differs (it
// is plugged into both the dokku subcommand name and the user-facing
// pluralization in plan messages).
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

// planSchedulerK3sScopedPairsSet delegates to planPairsSet with a current-state
// reader and a per-key command builder bound to the spec's scope.
func planSchedulerK3sScopedPairsSet(spec schedulerK3sScopedPairsSpec) PlanResult {
	return planPairsSet(
		singularizeSchedulerK3sKind(spec.Kind),
		spec.Pairs,
		func() (map[string]string, error) { return getSchedulerK3sScopedPairs(spec) },
		func(key, value string) subprocess.ExecCommandInput {
			return schedulerK3sScopedPairsCommand(spec, key, value)
		},
	)
}

// planSchedulerK3sScopedPairsUnset delegates to planPairsUnset; the command
// builder passes an empty value, which dokku's `:labels:set` / `:annotations:set`
// interpret as "clear this key".
func planSchedulerK3sScopedPairsUnset(spec schedulerK3sScopedPairsSpec) PlanResult {
	return planPairsUnset(
		singularizeSchedulerK3sKind(spec.Kind),
		spec.Pairs,
		func() (map[string]string, error) { return getSchedulerK3sScopedPairs(spec) },
		func(key, value string) subprocess.ExecCommandInput {
			return schedulerK3sScopedPairsCommand(spec, key, value)
		},
	)
}

// schedulerK3sScopedPairsCommand builds one `dokku scheduler-k3s:<kind>:set`
// call. An empty value is forwarded verbatim; dokku interprets it as a clear.
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
