package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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

// schedulerK3sReportProcessTypeToTask is the inverse of
// renderedSchedulerK3sProcessType: it maps a report-rendered process type back
// to the value the task carries. The rendered global sentinel "global" becomes
// an empty ProcessType (the task's default/global-process form); real process
// types pass through unchanged. Like the forward mapping, this shares dokku's
// pathological ambiguity - a real process literally named "global" is
// indistinguishable from the sentinel and round-trips to an empty ProcessType.
func schedulerK3sReportProcessTypeToTask(rendered string) string {
	if rendered == "global" {
		return ""
	}
	return rendered
}

// exportSchedulerK3sScopedPairs reconstructs the annotations or labels task
// bodies for one scope (a single app, or the global scope when global is true)
// from `scheduler-k3s:<kind>:report --format json`. The report is called
// without --process-type/--resource-type filters so it returns every scope; the
// flat `<processType>.<resourceType>.<key>` payload is grouped into one task
// body per (process_type, resource_type) pair and build turns each group into
// the task struct. Non-SSH errors and unparseable output are swallowed (return
// nil) so a host without scheduler-k3s state does not fail the whole export,
// mirroring the profile and chart exporters.
func exportSchedulerK3sScopedPairs(kind, app string, global bool, build func(processType, resourceType string, pairs map[string]string) interface{}) ([]interface{}, error) {
	target := app
	if global {
		target = "--global"
	}

	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "scheduler-k3s:" + kind + ":report", target, "--format", "json"},
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

	type scope struct{ processType, resourceType string }
	groups := map[scope]map[string]string{}
	for composed, value := range payload {
		// processType and resourceType never contain dots, but an annotation or
		// label key frequently does (e.g. "prometheus.io/scrape"), so split off
		// exactly the first two segments and keep the remainder as the key.
		parts := strings.SplitN(composed, ".", 3)
		if len(parts) != 3 || parts[0] == "" || parts[1] == "" || parts[2] == "" {
			continue
		}
		s := scope{
			processType:  schedulerK3sReportProcessTypeToTask(parts[0]),
			resourceType: parts[1],
		}
		if groups[s] == nil {
			groups[s] = map[string]string{}
		}
		groups[s][parts[2]] = value
	}

	scopes := make([]scope, 0, len(groups))
	for s := range groups {
		scopes = append(scopes, s)
	}
	sort.Slice(scopes, func(i, j int) bool {
		if scopes[i].resourceType != scopes[j].resourceType {
			return scopes[i].resourceType < scopes[j].resourceType
		}
		return scopes[i].processType < scopes[j].processType
	})

	var out []interface{}
	for _, s := range scopes {
		out = append(out, build(s.processType, s.resourceType, groups[s]))
	}
	return out, nil
}
