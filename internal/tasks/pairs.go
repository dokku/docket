package tasks

import (
	"context"
	"fmt"

	"github.com/dokku/docket/internal/subprocess"
)

// pairsCurrentFunc returns the pairs currently stored at the task's scope.
// A non-nil error short-circuits the plan with PlanStatusError. It takes the
// context rather than capturing one, for the same reason PlanResult.apply
// does: the probe runs under the caller's context, not one baked in wherever
// the closure happened to be built.
type pairsCurrentFunc func(ctx context.Context) (map[string]string, error)

// pairsCommandFunc builds one dokku subprocess invocation that sets or
// clears a single key. Pass an empty value to clear (dokku interprets a
// missing value on the subcommands this helper drives as a delete).
type pairsCommandFunc func(key, value string) subprocess.ExecCommandInput

// pairsObserveFunc builds the calling task's own type from the pairs the probe
// read and the state they amount to, for PlanResult.Observed.
type pairsObserveFunc func(pairs map[string]string, state State) Task

// observedPairs attaches the pairs the probe read to r. An additive plan
// (present, absent) reads only the keys it declares, and reports them as
// present or absent; a plan that owns the collection (set, clear) reads every
// key, and reports them as set, or as clear when there are none.
func observedPairs(r PlanResult, observe pairsObserveFunc, current, declared map[string]string, whole bool) PlanResult {
	var held map[string]string
	for k, v := range current {
		if !whole {
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
	case whole && len(held) > 0:
		state = StateSet
	case whole:
		state = StateClear
	case len(held) > 0:
		state = StatePresent
	}
	return observed(r, observe(held, state), len(held) > 0)
}

// planPairsSet probes current pairs, diffs against desired, and returns a
// PlanResult whose apply runs one command per drifted key. kind is the
// singular noun the helper substitutes into the user-facing reason
// ("label", "annotation", "chart value").
func planPairsSet(ctx context.Context, observe pairsObserveFunc, kind string, desired map[string]string, currentFn pairsCurrentFunc, commandFn pairsCommandFunc) PlanResult {
	current, err := currentFn(ctx)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	drifted, allNew := driftedKeys(desired, current)
	if len(drifted) == 0 {
		return observedPairs(PlanResult{InSync: true, Status: PlanStatusOK}, observe, current, desired, false)
	}

	status := PlanStatusModify
	if allNew {
		status = PlanStatusCreate
	}

	inputs := make([]subprocess.ExecCommandInput, 0, len(drifted))
	for _, key := range drifted {
		inputs = append(inputs, commandFn(key, desired[key]))
	}

	return observedPairs(PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%d %s(s) to set", len(drifted), kind),
		Mutations: formatSetMutations(drifted, desired, current),
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
		},
	}, observe, current, desired, false)
}

// planPairsUnset probes current pairs and returns a PlanResult whose apply
// clears each desired key that exists in current. Keys absent from the
// server are skipped silently (no-op).
func planPairsUnset(ctx context.Context, observe pairsObserveFunc, kind string, desired map[string]string, currentFn pairsCurrentFunc, commandFn pairsCommandFunc) PlanResult {
	current, err := currentFn(ctx)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	toClear := intersectingKeys(desired, current)
	if len(toClear) == 0 {
		return observedPairs(PlanResult{InSync: true, Status: PlanStatusOK}, observe, current, desired, false)
	}

	inputs := make([]subprocess.ExecCommandInput, 0, len(toClear))
	for _, key := range toClear {
		inputs = append(inputs, commandFn(key, ""))
	}

	return observedPairs(PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("%d %s(s) to unset", len(toClear), kind),
		Mutations: formatClearMutations(toClear, current),
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateAbsent, inputs)
		},
	}, observe, current, desired, false)
}

// pairsWholeCommandFunc builds the single dokku invocation that writes a
// collection's complete desired map, or the one that empties it. Unlike
// pairsCommandFunc it takes no key, because the whole map travels in one call.
type pairsWholeCommandFunc func() subprocess.ExecCommandInput

// planPairsReplace probes current pairs, diffs against desired in both
// directions, and returns a PlanResult whose apply runs the single command
// carrying the complete declared map. It is what separates an authoritative
// `set` from the additive `present` planPairsSet drives: a key stored on the
// server that the recipe does not name is removed rather than left alone, and
// it goes in the same call that writes the declared keys, so the collection is
// never left carrying a mixture of the old and new maps (issue #527).
func planPairsReplace(ctx context.Context, observe pairsObserveFunc, kind string, desired map[string]string, currentFn pairsCurrentFunc, commandFn pairsWholeCommandFunc) PlanResult {
	current, err := currentFn(ctx)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	drifted, _ := driftedKeys(desired, current)
	mutations := formatSetMutations(drifted, desired, current)
	mutations = append(mutations, formatClearMutations(removedKeys(desired, current), current)...)
	if len(mutations) == 0 {
		return observedPairs(PlanResult{InSync: true, Status: PlanStatusOK}, observe, current, desired, true)
	}

	status := PlanStatusModify
	if len(current) == 0 {
		status = PlanStatusCreate
	}

	inputs := []subprocess.ExecCommandInput{commandFn()}
	return observedPairs(PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%d %s change(s)", len(mutations), kind),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StateSet, inputs)
		},
	}, observe, current, desired, true)
}

// planPairsClear probes current pairs and returns a PlanResult whose apply runs
// the single command that empties the collection. The whole-map write cannot
// stand in for it: dokku rejects an empty pair list rather than reading it as
// "remove everything", so that a generated list which expands to nothing cannot
// silently drop a collection.
func planPairsClear(ctx context.Context, observe pairsObserveFunc, kind string, currentFn pairsCurrentFunc, commandFn pairsWholeCommandFunc) PlanResult {
	current, err := currentFn(ctx)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	if len(current) == 0 {
		return observedPairs(PlanResult{InSync: true, Status: PlanStatusOK}, observe, current, nil, true)
	}

	inputs := []subprocess.ExecCommandInput{commandFn()}
	return observedPairs(PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("clear %d %s(s)", len(current), kind),
		Mutations: formatClearMutations(sortedPairKeys(current), current),
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateClear, inputs)
		},
	}, observe, current, nil, true)
}
