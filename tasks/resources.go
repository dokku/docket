package tasks

import (
	"errors"
	"fmt"
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

// planResource is the shared Plan() implementation for resource tasks. The
// probe runs once; the apply closure consumes the diff.
func planResource(state State, app, processType string, resources map[string]string, clearBefore bool, subcommand string) PlanResult {
	if state == StatePresent && len(resources) == 0 {
		return PlanResult{
			Status: PlanStatusError,
			Error:  errors.New("resources are required when state is present"),
		}
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

	for k := range rctx.Resources {
		if _, ok := currentResources[k]; !ok {
			return PlanResult{
				Status: PlanStatusError,
				Error:  fmt.Errorf("unknown resource %s, valid resources: %v", k, mapKeys(currentResources)),
			}
		}
	}

	mutations := []string{}
	if rctx.ClearBefore {
		mutations = append(mutations, "clear before set")
	}
	for k, v := range rctx.Resources {
		if currentResources[k] != v {
			mutations = append(mutations, fmt.Sprintf("set %s=%s (was %q)", k, v, currentResources[k]))
		}
	}

	if len(mutations) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	inputs := resourceSetInputs(subcommand, rctx)
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusModify,
		Reason:    fmt.Sprintf("%d resource(s) to set", len(mutations)),
		Mutations: mutations,
		Commands:  resolveCommands(inputs),
		apply:     applyResourceSet(subcommand, rctx),
	}
}

// planClearResource reports drift for an absent-state resource clear.
func planClearResource(subcommand string, rctx ResourceContext) PlanResult {
	currentResources, err := getResources(subcommand, rctx)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	hasResources := false
	for _, v := range currentResources {
		if v != "" && v != "0" {
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
	for key, value := range rctx.Resources {
		args = append(args, fmt.Sprintf("--%s", key), value)
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
