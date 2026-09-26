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
	effective := state
	if effective == "" {
		effective = StatePresent
	}
	singular := singularizeSchedulerK3sKind(spec.Kind)

	// :clear takes no pairs, so a map supplied alongside it would be silently
	// discarded rather than removed. Every other state consumes one.
	if effective == StateClear {
		if len(spec.Pairs) > 0 {
			return fmt.Errorf("'%s' must not be set for state 'clear'", spec.Kind)
		}
		return nil
	}
	if len(spec.Pairs) == 0 {
		return fmt.Errorf("'%s' must not be empty for state '%s'", spec.Kind, effective)
	}

	for _, key := range sortedPairKeys(spec.Pairs) {
		if key == "" {
			return fmt.Errorf("%s keys must not be empty", singular)
		}
		// The per-key form passes the key and the value as separate arguments,
		// so an "=" in a key is harmless there. State 'set' goes through
		// :set --replace, which splits every pair on its first "=", and would
		// store a truncated key and a mangled value instead.
		if effective == StateSet && strings.Contains(key, "=") {
			return fmt.Errorf("%s keys must not contain '=' for state 'set', got %q", singular, key)
		}
		// dokku interprets an empty value on the :set subcommand as a clear, so
		// a present-state empty value can never be stored and would drift on
		// every run (issue #358). Clearing is expressed with state 'absent'.
		// State 'set' is exempt: :set --replace writes the declared map
		// wholesale, so an empty value is stored and reads back out of the
		// report.
		if effective == StatePresent && spec.Pairs[key] == "" {
			return fmt.Errorf("%s values must not be empty for state 'present'; use state 'absent' to clear a %s", singular, singular)
		}
	}
	return nil
}

// planSchedulerK3sScopedPairsPresent delegates to planPairsSet with a
// current-state reader and a per-key command builder bound to the spec's
// scope. Present is additive: a key the server carries and the recipe does not
// is left alone, which is what state 'set' exists to override.
func planSchedulerK3sScopedPairsPresent(ctx context.Context, spec schedulerK3sScopedPairsSpec) PlanResult {
	return planPairsSet(ctx,
		singularizeSchedulerK3sKind(spec.Kind),
		spec.Pairs,
		func(ctx context.Context) (map[string]string, error) { return getSchedulerK3sScopedPairs(ctx, spec) },
		func(key, value string) subprocess.ExecCommandInput {
			return schedulerK3sScopedPairsCommand(spec, key, value)
		},
	)
}

// planSchedulerK3sScopedPairsAbsent delegates to planPairsUnset; the command
// builder passes an empty value, which dokku's `:labels:set` / `:annotations:set`
// interpret as "clear this key".
func planSchedulerK3sScopedPairsAbsent(ctx context.Context, spec schedulerK3sScopedPairsSpec) PlanResult {
	return planPairsUnset(ctx,
		singularizeSchedulerK3sKind(spec.Kind),
		spec.Pairs,
		func(ctx context.Context) (map[string]string, error) { return getSchedulerK3sScopedPairs(ctx, spec) },
		func(key, value string) subprocess.ExecCommandInput {
			return schedulerK3sScopedPairsCommand(spec, key, value)
		},
	)
}

// planSchedulerK3sScopedPairsSet delegates to planPairsReplace with a
// whole-map command builder bound to the spec's scope, so `:set --replace`
// removes the keys stored at the scope that the recipe does not declare.
func planSchedulerK3sScopedPairsSet(ctx context.Context, spec schedulerK3sScopedPairsSpec) PlanResult {
	return planPairsReplace(ctx,
		singularizeSchedulerK3sKind(spec.Kind),
		spec.Pairs,
		func(ctx context.Context) (map[string]string, error) { return getSchedulerK3sScopedPairs(ctx, spec) },
		func() subprocess.ExecCommandInput { return schedulerK3sScopedPairsReplaceCommand(spec) },
	)
}

// planSchedulerK3sScopedPairsClear delegates to planPairsClear; the command
// builder empties this spec's (process_type, resource_type) scope alone, not
// every scope on the app.
func planSchedulerK3sScopedPairsClear(ctx context.Context, spec schedulerK3sScopedPairsSpec) PlanResult {
	return planPairsClear(ctx,
		singularizeSchedulerK3sKind(spec.Kind),
		func(ctx context.Context) (map[string]string, error) { return getSchedulerK3sScopedPairs(ctx, spec) },
		func() subprocess.ExecCommandInput { return schedulerK3sScopedPairsClearCommand(spec) },
	)
}

// schedulerK3sScopedPairsScopeArgs returns the flags and the positional target
// that address one scope. Every scheduler-k3s pair command shares them, and the
// process type is always spelled out: dokku defaults an omitted --process-type
// to the same global sentinel on the :set commands, but reads it as "no filter"
// on :clear, where leaving it off would empty every process type's map for the
// resource type rather than just this scope's.
func schedulerK3sScopedPairsScopeArgs(spec schedulerK3sScopedPairsSpec) []string {
	args := []string{
		"--resource-type", spec.ResourceType,
		"--process-type", schedulerK3sEffectiveProcessType(spec.ProcessType),
	}
	if spec.Global {
		return append(args, "--global")
	}
	return append(args, spec.App)
}

// schedulerK3sScopedPairsCommand builds one `dokku scheduler-k3s:<kind>:set`
// call. An empty value is forwarded verbatim; dokku interprets it as a clear.
func schedulerK3sScopedPairsCommand(spec schedulerK3sScopedPairsSpec, key, value string) subprocess.ExecCommandInput {
	args := append([]string{"--quiet", "scheduler-k3s:" + spec.Kind + ":set"}, schedulerK3sScopedPairsScopeArgs(spec)...)
	return subprocess.ExecCommandInput{Command: "dokku", Args: append(args, key, value)}
}

// schedulerK3sScopedPairsReplaceCommand builds the single
// `dokku scheduler-k3s:<kind>:set --replace` call that swaps the scope's whole
// map for the declared one. Dokku reads the app from the first positional
// argument and the key=value pairs from the rest, so the pairs follow the
// target; under --global there is no positional target and they start the list.
// They are sorted so the rendered command does not change between runs.
func schedulerK3sScopedPairsReplaceCommand(spec schedulerK3sScopedPairsSpec) subprocess.ExecCommandInput {
	args := append([]string{"--quiet", "scheduler-k3s:" + spec.Kind + ":set", "--replace"}, schedulerK3sScopedPairsScopeArgs(spec)...)
	for _, key := range sortedPairKeys(spec.Pairs) {
		args = append(args, key+"="+spec.Pairs[key])
	}
	return subprocess.ExecCommandInput{Command: "dokku", Args: args}
}

// schedulerK3sScopedPairsClearCommand builds the `dokku
// scheduler-k3s:<kind>:clear` call that empties one scope. `:set --replace`
// cannot express this: dokku rejects an empty pair list rather than reading it
// as "remove everything", so that a generated list which expands to nothing
// cannot silently drop an app's metadata.
func schedulerK3sScopedPairsClearCommand(spec schedulerK3sScopedPairsSpec) subprocess.ExecCommandInput {
	args := append([]string{"--quiet", "scheduler-k3s:" + spec.Kind + ":clear"}, schedulerK3sScopedPairsScopeArgs(spec)...)
	return subprocess.ExecCommandInput{Command: "dokku", Args: args}
}

// getSchedulerK3sScopedPairs reads the pairs currently stored at the spec's
// (app|global, process_type, resource_type) scope. It calls
// `dokku scheduler-k3s:<kind>:report ... --format json`, which returns a flat
// map keyed by `<rendered_process_type>.<resource_type>.<key>`, and strips the
// prefix to recover the original keys.
func getSchedulerK3sScopedPairs(ctx context.Context, spec schedulerK3sScopedPairsSpec) (map[string]string, error) {
	args := []string{"--quiet", "scheduler-k3s:" + spec.Kind + ":report"}
	if spec.Global {
		args = append(args, "--global")
	} else {
		args = append(args, spec.App)
	}
	args = append(args, "--resource-type", spec.ResourceType)

	args = append(args, "--process-type", schedulerK3sEffectiveProcessType(spec.ProcessType))
	args = append(args, "--format", "json")

	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
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

// schedulerK3sEffectiveProcessType returns the process type a command should
// name for a scope. dokku stores a scope whose process type is omitted under
// the sentinel "--global", and the sentinel is spelled out on every command
// rather than left to dokku's default, because :clear reads an omitted
// --process-type as "no filter" instead of as that default.
func schedulerK3sEffectiveProcessType(processType string) string {
	if processType == "" {
		return "--global"
	}
	return processType
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
func exportSchedulerK3sScopedPairs(ctx context.Context, kind, app string, global bool, build func(processType, resourceType string, pairs map[string]string) interface{}) ([]interface{}, error) {
	target := app
	if global {
		target = "--global"
	}

	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
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
