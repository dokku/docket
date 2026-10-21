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

// ResourceContext represents the context for a resource operation
type ResourceContext struct {
	// App is the name of the app
	App string

	// ProcessType is the process type to filter resources for
	ProcessType string

	// Resources is a map of resource type to quantity
	Resources map[string]string

	// ClearBefore clears all resources before applying new ones
	ClearBefore bool
}

// getResources retrieves the current resources for a given dokku application
func getResources(ctx context.Context, subcommand string, rctx ResourceContext) (map[string]string, error) {
	args := []string{
		"--quiet",
		subcommand,
	}

	if rctx.ProcessType != "" {
		args = append(args, "--process-type", rctx.ProcessType)
	}

	args = append(args, rctx.App)

	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    args,
	})
	if err != nil {
		return nil, err
	}

	resources := map[string]string{}
	for _, line := range strings.Split(result.StdoutContents(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.Contains(line, ":") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key != "" {
			resources[key] = value
		}
	}

	return resources, nil
}

// exportResourceTasks reconstructs resource tasks of the given kind ("limit" or
// "reserve") from resource:report, one task per process type (in sorted order),
// carrying only the explicitly-set (non-empty) resources. factory builds the
// concrete task body for a (process type, resources) pair.
func exportResourceTasks(ctx context.Context, app, kind string, factory func(app, processType string, resources map[string]string) interface{}) ([]interface{}, error) {
	response, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "resource:report", app, "--format", "json"},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return nil, nil
	}

	payload := map[string]string{}
	if err := json.Unmarshal(response.StdoutBytes(), &payload); err != nil {
		return nil, nil
	}

	// Keys look like "<process>.<kind>.<resource>"; the legacy "resource-"
	// prefixed duplicates are skipped. The "_default_" process maps to the
	// empty process type.
	byProcess := map[string]map[string]string{}
	for key, value := range payload {
		if value == "" || strings.HasPrefix(key, "resource-") {
			continue
		}
		parts := strings.SplitN(key, ".", 3)
		if len(parts) != 3 || parts[1] != kind {
			continue
		}
		process := parts[0]
		if process == "_default_" {
			process = ""
		}
		if byProcess[process] == nil {
			byProcess[process] = map[string]string{}
		}
		byProcess[process][parts[2]] = value
	}

	processes := make([]string, 0, len(byProcess))
	for process := range byProcess {
		processes = append(processes, process)
	}
	sort.Strings(processes)

	var out []interface{}
	for _, process := range processes {
		out = append(out, factory(app, process, byProcess[process]))
	}
	return out, nil
}

// validateResourceInput checks a resource task's values without probing the
// server, past the constraints its fields declare: resources are required
// when state is present, and forbidden when it is clear because the clear
// command takes none, so a map supplied alongside it would be silently
// discarded rather than removed; clear_before is forbidden with set and
// clear, which already own every resource, so it would only restate them.
// gatePlan checks those before planResource runs, and each resource task's
// Validate() checks them before calling this. The unknown-resource-key check
// stays in planSetResource and planReplaceResource because it needs the live
// resource report.
func validateResourceInput(state State, resources map[string]string) error {
	// The set command marks a resource for removal with the literal value
	// "clear", so a declared value of "clear" would remove the resource rather
	// than store it.
	if state == StateSet {
		for _, k := range sortedResourceKeys(resources) {
			if resources[k] == "clear" {
				return fmt.Errorf("resource %s must not be set to %q when state is set; omit it to clear it", k, "clear")
			}
		}
	}
	return nil
}

// planResource plans a resource task. planned is the task itself, which an
// in-sync plan observes. observe builds the calling task's own type from the
// resources the probe read and the state they amount to. A present plan
// without clear_before reads only the resources it declares; a
// clear-before-set and the absent, set and clear states own every resource, so
// they read them all.
func planResource(ctx context.Context, planned Task, observe func(resources map[string]string, state State) Task, state State, app, processType string, resources map[string]string, clearBefore bool, subcommand string) PlanResult {
	if err := validateResourceInput(state, resources); err != nil {
		return planErr(err)
	}

	rctx := ResourceContext{
		App:         app,
		ProcessType: processType,
		Resources:   resources,
		ClearBefore: clearBefore,
	}

	return DispatchPlan(state, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planSetResource(ctx, planned, observe, subcommand, rctx) },
		StateAbsent:  func() PlanResult { return planClearResource(ctx, planned, observe, subcommand, rctx, StateAbsent) },
		StateSet: func() PlanResult {
			if len(resources) == 0 {
				return planSetAsClear(planClearResource(ctx, planned, observe, subcommand, rctx, StateClear))
			}
			return planReplaceResource(ctx, planned, observe, subcommand, rctx)
		},
		StateClear: func() PlanResult { return planClearResource(ctx, planned, observe, subcommand, rctx, StateClear) },
	})
}

// planSetResource reports drift for a present-state resource set.
func planSetResource(ctx context.Context, planned Task, observe func(map[string]string, State) Task, subcommand string, rctx ResourceContext) PlanResult {
	currentResources, err := getResources(ctx, subcommand, rctx)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	for _, k := range sortedResourceKeys(rctx.Resources) {
		if _, ok := currentResources[k]; !ok {
			return PlanResult{
				Status: PlanStatusError,
				Error:  fmt.Errorf("unknown resource %s, valid resources: %v", k, sortedResourceKeys(currentResources)),
			}
		}
	}

	// A clear-before-set only changes anything when the server still holds a
	// resource outside the desired map that is not already empty; otherwise the
	// clear is a no-op and must not force perpetual drift (issue #333). Fold the
	// decision into an effective context so the plan Commands and the apply drop
	// the redundant clear identically.
	effective := rctx
	effective.ClearBefore = rctx.ClearBefore && clearBeforeChanges(currentResources, rctx.Resources)

	mutations := []string{}
	if effective.ClearBefore {
		mutations = append(mutations, "clear before set")
	}
	for _, k := range sortedResourceKeys(rctx.Resources) {
		v := rctx.Resources[k]
		if currentResources[k] != v {
			mutations = append(mutations, fmt.Sprintf("set %s=%s (was %q)", k, v, currentResources[k]))
		}
	}

	var declared map[string]string
	if !rctx.ClearBefore {
		declared = rctx.Resources
	}
	seen := func(r PlanResult) PlanResult {
		return observedResources(r, planned, observe, currentResources, declared, false)
	}

	if len(mutations) == 0 {
		return seen(PlanResult{InSync: true, Status: PlanStatusOK})
	}
	inputs := resourceSetInputs(subcommand, effective)
	return seen(PlanResult{
		InSync:    false,
		Status:    PlanStatusModify,
		Reason:    fmt.Sprintf("%d resource(s) to set", len(mutations)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply:     applyResourceSet(subcommand, effective),
	})
}

// observedResources attaches the resources the probe read to r: the set ones
// among declared, or every set one when declared is nil. authoritative marks a
// set or clear plan, which reports what it read as set, or as clear when
// nothing is set; the other plans report present or absent.
func observedResources(r PlanResult, planned Task, observe func(map[string]string, State) Task, current, declared map[string]string, authoritative bool) PlanResult {
	var held map[string]string
	for k, v := range current {
		if !resourceValueIsSet(v) {
			continue
		}
		if declared != nil {
			if _, ok := declared[k]; !ok {
				continue
			}
		}
		if held == nil {
			held = map[string]string{}
		}
		held[k] = v
	}
	state := StateAbsent
	switch {
	case authoritative && len(held) > 0:
		state = StateSet
	case authoritative:
		state = StateClear
	case len(held) > 0:
		state = StatePresent
	}
	return observed(r, planned, observe(held, state), len(held) > 0)
}

// planReplaceResource reports drift for a set-state resource replacement:
// after applying, the resources set at the task's scope are exactly the
// declared ones. It runs a single resource command, since dokku removes a
// resource whose value is the literal "clear": every declared resource is
// written and every other resource still set is cleared in the same call, so
// the scope is never left carrying a mixture of the old and new maps.
func planReplaceResource(ctx context.Context, planned Task, observe func(map[string]string, State) Task, subcommand string, rctx ResourceContext) PlanResult {
	currentResources, err := getResources(ctx, subcommand, rctx)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	for _, k := range sortedResourceKeys(rctx.Resources) {
		if _, ok := currentResources[k]; !ok {
			return PlanResult{
				Status: PlanStatusError,
				Error:  fmt.Errorf("unknown resource %s, valid resources: %v", k, sortedResourceKeys(currentResources)),
			}
		}
	}

	mutations := []string{}
	for _, k := range sortedResourceKeys(rctx.Resources) {
		v := rctx.Resources[k]
		if currentResources[k] != v {
			mutations = append(mutations, fmt.Sprintf("set %s=%s (was %q)", k, v, currentResources[k]))
		}
	}
	effective := rctx
	effective.Resources = map[string]string{}
	for k, v := range rctx.Resources {
		effective.Resources[k] = v
	}
	for _, k := range sortedResourceKeys(currentResources) {
		if _, ok := rctx.Resources[k]; ok || !resourceValueIsSet(currentResources[k]) {
			continue
		}
		effective.Resources[k] = "clear"
		mutations = append(mutations, fmt.Sprintf("clear %s (was %q)", k, currentResources[k]))
	}

	seen := func(r PlanResult) PlanResult {
		return observedResources(r, planned, observe, currentResources, nil, true)
	}
	if len(mutations) == 0 {
		return seen(PlanResult{InSync: true, Status: PlanStatusOK})
	}
	status := PlanStatusModify
	if !hasSetResource(currentResources) {
		status = PlanStatusCreate
	}
	inputs := resourceSetInputs(subcommand, effective)
	return seen(PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%d resource change(s)", len(mutations)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StateSet, inputs)
		},
	})
}

// hasSetResource reports whether any resource in a report holds a set value.
func hasSetResource(current map[string]string) bool {
	for _, v := range current {
		if resourceValueIsSet(v) {
			return true
		}
	}
	return false
}

// resourceValueIsSet reports whether a resource report value represents an
// actually-set limit/reservation. dokku reports an unset resource as "" or "0".
func resourceValueIsSet(v string) bool {
	return v != "" && v != "0"
}

// clearBeforeChanges reports whether a clear-before-set would actually remove
// anything. The clear wipes every current resource that is not in the desired
// map, so it is a no-op unless one of those keys still holds a set value.
func clearBeforeChanges(current, desired map[string]string) bool {
	for k, v := range current {
		if _, ok := desired[k]; ok {
			continue
		}
		if resourceValueIsSet(v) {
			return true
		}
	}
	return false
}

// planClearResource reports drift for clearing every resource at the task's
// scope, for state absent or clear. finalState is the state the apply reports.
func planClearResource(ctx context.Context, planned Task, observe func(map[string]string, State) Task, subcommand string, rctx ResourceContext, finalState State) PlanResult {
	currentResources, err := getResources(ctx, subcommand, rctx)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	authoritative := finalState == StateClear
	if !hasSetResource(currentResources) {
		return observedResources(PlanResult{InSync: true, Status: PlanStatusOK}, planned, observe, currentResources, nil, authoritative)
	}
	inputs := []subprocess.ExecCommandInput{resourceClearInput(subcommand, rctx)}
	return observedResources(PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    "would clear all resources",
		Mutations: []string{fmt.Sprintf("clear resources via %s-clear", subcommand)},
		Commands:  resolveCommands(ctx, inputs),
		apply:     applyResourceClear(subcommand, rctx, finalState),
	}, planned, observe, currentResources, nil, authoritative)
}

// resourceClearInput returns the subprocess input that runs the resource
// clear command (`<subcommand>-clear`).
func resourceClearInput(subcommand string, rctx ResourceContext) subprocess.ExecCommandInput {
	args := []string{subcommand + "-clear"}
	if rctx.ProcessType != "" {
		args = append(args, "--process-type", rctx.ProcessType)
	}
	args = append(args, rctx.App)
	return subprocess.ExecCommandInput{Command: "dokku", Args: args}
}

// resourceSetInputs returns the subprocess inputs that set resource limits.
// When ClearBefore is true, the first input clears before the set runs.
func resourceSetInputs(subcommand string, rctx ResourceContext) []subprocess.ExecCommandInput {
	inputs := []subprocess.ExecCommandInput{}
	if rctx.ClearBefore {
		inputs = append(inputs, resourceClearInput(subcommand, rctx))
	}
	args := []string{subcommand}
	for _, key := range sortedResourceKeys(rctx.Resources) {
		args = append(args, fmt.Sprintf("--%s", key), rctx.Resources[key])
	}
	if rctx.ProcessType != "" {
		args = append(args, "--process-type", rctx.ProcessType)
	}
	args = append(args, rctx.App)
	inputs = append(inputs, subprocess.ExecCommandInput{Command: "dokku", Args: args})
	return inputs
}

// applyResourceSet returns a closure that runs the underlying resource
// set command. ClearBefore is honored by clearing before setting.
func applyResourceSet(subcommand string, rctx ResourceContext) func(ctx context.Context) TaskOutputState {
	inputs := resourceSetInputs(subcommand, rctx)
	return func(ctx context.Context) TaskOutputState {
		return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
	}
}

// applyResourceClear returns a closure that runs the underlying resource
// clear command (subcommand + "-clear"), reporting finalState.
func applyResourceClear(subcommand string, rctx ResourceContext, finalState State) func(ctx context.Context) TaskOutputState {
	inputs := []subprocess.ExecCommandInput{resourceClearInput(subcommand, rctx)}
	return func(ctx context.Context) TaskOutputState {
		return runExecInputs(ctx, TaskOutputState{State: StatePresent}, finalState, inputs)
	}
}

// mapKeys returns the keys of a map as a slice
func mapKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// sortedResourceKeys returns the resource keys in deterministic (sorted) order so
// plan and apply build byte-identical command args and mutation lists (issue #341).
func sortedResourceKeys(m map[string]string) []string {
	keys := mapKeys(m)
	sort.Strings(keys)
	return keys
}
