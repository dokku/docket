package tasks

import (
	"fmt"

	"github.com/dokku/docket/subprocess"
)

// pairsCurrentFunc returns the pairs currently stored at the task's scope.
// A non-nil error short-circuits the plan with PlanStatusError.
type pairsCurrentFunc func() (map[string]string, error)

// pairsCommandFunc builds one dokku subprocess invocation that sets or
// clears a single key. Pass an empty value to clear (dokku interprets a
// missing value on the subcommands this helper drives as a delete).
type pairsCommandFunc func(key, value string) subprocess.ExecCommandInput

// planPairsSet probes current pairs, diffs against desired, and returns a
// PlanResult whose apply runs one command per drifted key. kind is the
// singular noun the helper substitutes into the user-facing reason
// ("label", "annotation", "chart value").
func planPairsSet(kind string, desired map[string]string, currentFn pairsCurrentFunc, commandFn pairsCommandFunc) PlanResult {
	current, err := currentFn()
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	drifted, allNew := driftedKeys(desired, current)
	if len(drifted) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}

	status := PlanStatusModify
	if allNew {
		status = PlanStatusCreate
	}

	inputs := make([]subprocess.ExecCommandInput, 0, len(drifted))
	for _, key := range drifted {
		inputs = append(inputs, commandFn(key, desired[key]))
	}

	return PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%d %s(s) to set", len(drifted), kind),
		Mutations: formatSetMutations(drifted, desired, current),
		Commands:  resolveCommands(inputs),
		apply: func() TaskOutputState {
			return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
		},
	}
}

// planPairsUnset probes current pairs and returns a PlanResult whose apply
// clears each desired key that exists in current. Keys absent from the
// server are skipped silently (no-op).
func planPairsUnset(kind string, desired map[string]string, currentFn pairsCurrentFunc, commandFn pairsCommandFunc) PlanResult {
	current, err := currentFn()
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	toClear := intersectingKeys(desired, current)
	if len(toClear) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}

	inputs := make([]subprocess.ExecCommandInput, 0, len(toClear))
	for _, key := range toClear {
		inputs = append(inputs, commandFn(key, ""))
	}

	return PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("%d %s(s) to unset", len(toClear), kind),
		Mutations: formatClearMutations(toClear, current),
		Commands:  resolveCommands(inputs),
		apply: func() TaskOutputState {
			return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
		},
	}
}
