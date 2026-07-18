package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dokku/docket/subprocess"
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
func getResources(subcommand string, rctx ResourceContext) (map[string]string, error) {
	args := []string{
		"--quiet",
		subcommand,
	}

	if rctx.ProcessType != "" {
		args = append(args, "--process-type", rctx.ProcessType)
	}

	args = append(args, rctx.App)

	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
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
func exportResourceTasks(app, kind string, factory func(app, processType string, resources map[string]string) interface{}) ([]interface{}, error) {
	response, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
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

// planResource is the shared Plan() implementation for resource tasks. The
// probe runs once; the apply closure consumes the diff.
// validateResourceInput checks a resource task's inputs without probing the
// server. planResource and each resource task's Validate() call it so plan and
// validate report the same error. The unknown-resource-key check stays in
// planSetResource because it needs the live resource report.
func validateResourceInput(state State, resources map[string]string) error {
	if state == StatePresent && len(resources) == 0 {
		return errors.New("resources are required when state is present")
	}
	return nil
}

func planResource(state State, app, processType string, resources map[string]string, clearBefore bool, subcommand string) PlanResult {
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
		StatePresent: func() PlanResult { return planSetResource(subcommand, rctx) },
		StateAbsent:  func() PlanResult { return planClearResource(subcommand, rctx) },
	})
}

// planSetResource reports drift for a present-state resource set.
func planSetResource(subcommand string, rctx ResourceContext) PlanResult {
	currentResources, err := getResources(subcommand, rctx)
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

	if len(mutations) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	inputs := resourceSetInputs(subcommand, effective)
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusModify,
		Reason:    fmt.Sprintf("%d resource(s) to set", len(mutations)),
		Mutations: mutations,
		Commands:  resolveCommands(inputs),
		apply:     applyResourceSet(subcommand, effective),
	}
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

// planClearResource reports drift for an absent-state resource clear.
func planClearResource(subcommand string, rctx ResourceContext) PlanResult {
	currentResources, err := getResources(subcommand, rctx)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	hasResources := false
	for _, v := range currentResources {
		if resourceValueIsSet(v) {
			hasResources = true
			break
		}
	}

	if !hasResources {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	inputs := []subprocess.ExecCommandInput{resourceClearInput(subcommand, rctx)}
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    "would clear all resources",
		Mutations: []string{fmt.Sprintf("clear resources via %s-clear", subcommand)},
		Commands:  resolveCommands(inputs),
		apply:     applyResourceClear(subcommand, rctx),
	}
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
func applyResourceSet(subcommand string, rctx ResourceContext) func() TaskOutputState {
	inputs := resourceSetInputs(subcommand, rctx)
	return func() TaskOutputState {
		return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
	}
}

// applyResourceClear returns a closure that runs the underlying resource
// clear command (subcommand + "-clear").
func applyResourceClear(subcommand string, rctx ResourceContext) func() TaskOutputState {
	inputs := []subprocess.ExecCommandInput{resourceClearInput(subcommand, rctx)}
	return func() TaskOutputState {
		return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
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
