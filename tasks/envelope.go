package tasks

import (
	"github.com/expr-lang/expr/vm"
)

// TaskEnvelope wraps a single Task with the cross-cutting fields that the
// task entry envelope admits. The envelope-key allowlist is the union of:
//
//   - the keys carrying values on this struct (name, tags, when, loop,
//     register, changed_when, failed_when, ignore_errors), and
//   - the registered task-type key (e.g. dokku_app) which is decoded into
//     Task and identified by TypeName.
//
// register / changed_when / failed_when / ignore_errors are reserved by
// #205 but their semantics are activated in #210; the loader recognises
// the keys today so #210 does not need to revisit the cap.
type TaskEnvelope struct {
	// Name is the user-supplied human label for the task. Auto-generated
	// when the entry omits a name (see GetTasks). Loop-expansions append
	// an `(item=<value>)` suffix to keep ordered-map keys unique.
	Name string

	// Tags is the list of tag strings on the entry. The --tags / --skip-tags
	// CLI flags filter against this set.
	Tags []string

	// When is the raw expr-lang/expr source for the per-task conditional.
	// An empty string means "always run". When non-empty, whenProgram caches
	// the compiled form so loop iterations re-evaluate cheaply.
	When string

	// Loop is the per-task iteration source. Either a list literal
	// ([]interface{}) or an expr-lang/expr source string returning a list.
	// nil means no loop. The loader resolves this into N expanded envelopes
	// and replaces the original entry; downstream consumers see the
	// expansions, not Loop itself.
	Loop interface{}

	// Register, ChangedWhen, FailedWhen, IgnoreErrors are reserved for #210.
	// The loader recognises and decodes them so that issue does not have to
	// revisit the envelope-key allowlist.
	Register     string
	ChangedWhen  string
	FailedWhen   string
	IgnoreErrors bool

	// Block / Rescue / Always carry the nested child envelopes for a
	// try/catch/finally group entry (#211). When Block is non-empty the
	// envelope is a group: Task is nil, TypeName is empty, and the
	// executor walks the three child slices instead of dispatching to a
	// task body. The wrapping envelope's `name`, `tags`, `when`, `loop`,
	// `register`, `changed_when`, `failed_when`, and `ignore_errors`
	// apply to the whole group.
	Block  []*TaskEnvelope
	Rescue []*TaskEnvelope
	Always []*TaskEnvelope

	// Task is the decoded task body. Non-nil for leaf envelopes the
	// loader produces; nil for group envelopes (#211) where Block /
	// Rescue / Always carry the nested children instead.
	Task Task

	// TypeName is the registered task name (e.g. "dokku_app") that
	// dispatched this envelope's body decode. Used for diagnostics.
	TypeName string

	// LoopItem and LoopIndex are populated on envelopes produced by loop
	// expansion. LoopItem is the iterator value (expr context `.item`);
	// LoopIndex is the zero-based position. Non-loop envelopes leave both
	// at their zero value.
	LoopItem  interface{}
	LoopIndex int

	// IsLoopExpansion distinguishes a single-iteration envelope (e.g. when
	// LoopIndex == 0 and LoopItem == nil) from one that came from loop
	// expansion. Useful for predicate context construction.
	IsLoopExpansion bool

	// whenProgram is the pre-compiled expr program for When. Set by the
	// loader so loop iterations re-use the same compiled bytecode.
	whenProgram *vm.Program

	// changedWhenProgram / failedWhenProgram are the pre-compiled expr
	// programs for ChangedWhen and FailedWhen. Set by the loader so the
	// apply / plan executor does not re-compile per task.
	changedWhenProgram *vm.Program
	failedWhenProgram  *vm.Program
}

// HasWhen reports whether the envelope carries a non-empty `when:`
// predicate that must be evaluated before execution.
func (e *TaskEnvelope) HasWhen() bool {
	return e != nil && e.When != ""
}

// IsGroup reports whether the envelope is a try/catch/finally group
// entry (#211). Group envelopes carry their children in Block / Rescue
// / Always; their Task is nil. The executor uses this flag as the
// dispatch boundary between the leaf-task code path and the group
// walker.
func (e *TaskEnvelope) IsGroup() bool {
	return e != nil && len(e.Block) > 0
}

// WhenProgram returns the pre-compiled expr program for When, or nil if
// no `when:` predicate is present.
func (e *TaskEnvelope) WhenProgram() *vm.Program {
	if e == nil {
		return nil
	}
	return e.whenProgram
}

// HasChangedWhen reports whether the envelope carries a non-empty
// `changed_when:` predicate.
func (e *TaskEnvelope) HasChangedWhen() bool {
	return e != nil && e.ChangedWhen != ""
}

// ChangedWhenProgram returns the pre-compiled expr program for
// ChangedWhen, or nil when no `changed_when:` predicate is present.
func (e *TaskEnvelope) ChangedWhenProgram() *vm.Program {
	if e == nil {
		return nil
	}
	return e.changedWhenProgram
}

// HasFailedWhen reports whether the envelope carries a non-empty
// `failed_when:` predicate.
func (e *TaskEnvelope) HasFailedWhen() bool {
	return e != nil && e.FailedWhen != ""
}

// FailedWhenProgram returns the pre-compiled expr program for
// FailedWhen, or nil when no `failed_when:` predicate is present.
func (e *TaskEnvelope) FailedWhenProgram() *vm.Program {
	if e == nil {
		return nil
	}
	return e.failedWhenProgram
}

// HasTag reports whether the envelope's tag set contains tag.
func (e *TaskEnvelope) HasTag(tag string) bool {
	if e == nil {
		return false
	}
	for _, t := range e.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// IntersectsTags reports whether the envelope's tag set intersects with
// any of the given tags. Returns false for an envelope with no tags.
func (e *TaskEnvelope) IntersectsTags(tags []string) bool {
	if e == nil || len(tags) == 0 {
		return false
	}
	for _, t := range tags {
		if e.HasTag(t) {
			return true
		}
	}
	return false
}

// FilterByTags returns the subset of m's keys that satisfy the include
// (--tags) and skip (--skip-tags) filters. Rules:
//
//   - No flags supplied: every key.
//   - --tags only: keep iff tag set intersects includes; untagged tasks
//     are excluded.
//   - --skip-tags only: drop iff tag set intersects skips; untagged tasks
//     are kept.
//   - Both: --tags narrows first, then --skip-tags drops from the result.
//
// The order of the returned keys mirrors m.Keys() (insertion order).
func FilterByTags(m OrderedStringEnvelopeMap, includes, skips []string) []string {
	keys := m.Keys()
	if len(includes) == 0 && len(skips) == 0 {
		return keys
	}

	out := make([]string, 0, len(keys))
	for _, k := range keys {
		env := m.GetEnvelope(k)
		if len(includes) > 0 {
			if !env.IntersectsTags(includes) {
				continue
			}
		}
		if len(skips) > 0 && env.IntersectsTags(skips) {
			continue
		}
		out = append(out, k)
	}
	return out
}

// EnvelopeContainsName reports whether env or any of its block / rescue
// / always descendants has Name == target. Used by --start-at-task
// gating so the executor enters a group whose child matches the target
// rather than skipping the entire group.
func EnvelopeContainsName(env *TaskEnvelope, target string) bool {
	if env == nil {
		return false
	}
	if env.Name == target {
		return true
	}
	for _, child := range env.Block {
		if EnvelopeContainsName(child, target) {
			return true
		}
	}
	for _, child := range env.Rescue {
		if EnvelopeContainsName(child, target) {
			return true
		}
	}
	for _, child := range env.Always {
		if EnvelopeContainsName(child, target) {
			return true
		}
	}
	return false
}

// CollectEnvelopeNames returns every leaf and group envelope name
// reachable from envs, walking block / rescue / always recursively.
// Names appear in source order and are de-duplicated. Used by the
// --start-at-task validators (apply.go and tasks/validate.go) to build
// the "available names" hint shown when no task matched.
func CollectEnvelopeNames(envs []*TaskEnvelope) []string {
	seen := map[string]bool{}
	var out []string
	var walk func(env *TaskEnvelope)
	walk = func(env *TaskEnvelope) {
		if env == nil {
			return
		}
		if env.Name != "" && !seen[env.Name] {
			seen[env.Name] = true
			out = append(out, env.Name)
		}
		for _, child := range env.Block {
			walk(child)
		}
		for _, child := range env.Rescue {
			walk(child)
		}
		for _, child := range env.Always {
			walk(child)
		}
	}
	for _, env := range envs {
		walk(env)
	}
	return out
}
