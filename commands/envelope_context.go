package commands

import (
	"github.com/dokku/docket/tasks"
)

// buildEnvelopeExprContext returns the base expr context the apply / plan
// path uses to evaluate envelope predicates (`when:` etc.). Today this is
// just the file-level inputs map; #208 / #210 will add timestamp / host /
// play / result / registered keys.
func buildEnvelopeExprContext(inputs map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(inputs))
	for k, v := range inputs {
		out[k] = v
	}
	return out
}

// envelopeExprContext returns a per-envelope expr context. Loop-expansion
// envelopes inject `.item` / `.index` so a `when: 'item != "web"'`
// predicate evaluates against the iteration value. The run-wide
// registered map is exposed under `.registered` whenever it carries
// any entries so a predicate can reference `.registered.<name>` once
// a prior task has registered. result is non-nil only inside the
// post-execute `changed_when` / `failed_when` evaluation phase; pass
// nil from `when:` call sites so predicates running before the task
// fires do not see a stale result.
//
// failedTask is non-nil only inside a #211 rescue child's predicate
// evaluation; it is bound under `.failed_task` so a rescue handler can
// inspect the failing block child's TaskOutputState (e.g.
// `failed_task.Stderr contains "..."`). Outside rescue scope the key is
// absent.
func envelopeExprContext(
	base map[string]interface{},
	env *tasks.TaskEnvelope,
	result interface{},
	registered map[string]tasks.RegisteredValue,
	failedTask interface{},
) map[string]interface{} {
	hasLoopVars := env != nil && env.IsLoopExpansion
	hasResult := result != nil
	hasRegistered := len(registered) > 0
	hasFailedTask := failedTask != nil
	if !hasLoopVars && !hasResult && !hasRegistered && !hasFailedTask {
		return base
	}
	out := make(map[string]interface{}, len(base)+4)
	for k, v := range base {
		out[k] = v
	}
	if hasLoopVars {
		out["item"] = env.LoopItem
		out["index"] = env.LoopIndex
	}
	if hasResult {
		out["result"] = result
	}
	if hasRegistered {
		// Copy the registered map per call so a downstream call cannot
		// mutate the executor's working map by writing into the context.
		registeredCopy := make(map[string]tasks.RegisteredValue, len(registered))
		for k, v := range registered {
			registeredCopy[k] = v
		}
		out["registered"] = registeredCopy
	}
	if hasFailedTask {
		out["failed_task"] = failedTask
	}
	return out
}

// buildPlayWhenContext returns the expr context the play-level `when:`
// predicate evaluates against. Per the spec, a play's own `inputs:`
// are NOT visible to its own when (the issue body calls this circular),
// and other plays' play-local inputs are also not visible. Only
// file-level input defaults and user-provided overrides reach this
// context.
//
// The base context is the apply / plan command's merged map (file-level
// + vars-file + CLI). fileLevelKeys is the set of input names declared
// on inputs-only plays. userSet is the union of CLI-set and
// vars-file-set keys. A key passes through when it is file-level OR the
// user has explicitly overridden it.
func buildPlayWhenContext(base map[string]interface{}, fileLevelKeys, userSet map[string]bool) map[string]interface{} {
	out := make(map[string]interface{}, len(base))
	for k, v := range base {
		if fileLevelKeys[k] || userSet[k] {
			out[k] = v
		}
	}
	return out
}
