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
// predicate evaluates against the iteration value.
func envelopeExprContext(base map[string]interface{}, env *tasks.TaskEnvelope) map[string]interface{} {
	if env == nil || !env.IsLoopExpansion {
		return base
	}
	out := make(map[string]interface{}, len(base)+2)
	for k, v := range base {
		out[k] = v
	}
	out["item"] = env.LoopItem
	out["index"] = env.LoopIndex
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
