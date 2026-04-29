package tasks

import (
	"github.com/expr-lang/expr/vm"
)

// Play is one entry in a docket recipe's top-level YAML list. A recipe may
// declare any number of plays; the executor walks them in order and bounds
// per-task state to the play's own context.
//
// The envelope keys (Name, Tags, When, Inputs) sit alongside the per-play
// Tasks. Per-play Inputs override file-level defaults within their play but
// not across plays; CLI --name=value and --vars-file always win. Per-play
// Tags are inherited by every task in the play (additive with per-task tags
// already on the envelope).
//
// The play's own When predicate is evaluated against the file-level merged
// context only - the play's own Inputs are not visible to its own When (the
// spec calls this circular). Per-task When inside the play does see the
// play's Inputs.
type Play struct {
	// Name is the user-supplied label for the play. Auto-generated as
	// "play #N" when the entry omits a name (see GetPlays).
	Name string

	// Tags is the play's tag list. Folded into every task envelope's Tags
	// at parse time so the existing FilterByTags helper keeps working
	// without a per-play branch.
	Tags []string

	// When is the raw expr-lang/expr source for the play-level conditional.
	// An empty string means "always run". When non-empty, whenProgram caches
	// the compiled form so the executor can re-evaluate cheaply.
	When string

	// Inputs is the play's own inputs declaration. Defaults layer above
	// file-level defaults but below --vars-file / CLI overrides.
	Inputs []Input

	// Tasks is the per-play envelope map populated by GetPlays. Insertion
	// order mirrors the play's tasks: source order.
	Tasks OrderedStringEnvelopeMap

	// whenProgram is the pre-compiled expr program for When. Set by
	// GetPlays so the executor does not re-compile per evaluation.
	whenProgram *vm.Program
}

// HasWhen reports whether the play carries a non-empty `when:` predicate
// that must be evaluated before the play's tasks run.
func (p *Play) HasWhen() bool {
	return p != nil && p.When != ""
}

// WhenProgram returns the pre-compiled expr program for the play's When,
// or nil when no `when:` predicate is present.
func (p *Play) WhenProgram() *vm.Program {
	if p == nil {
		return nil
	}
	return p.whenProgram
}

// IsFileLevel reports whether this play is an inputs-only entry rather
// than a task play. The convention: a play with no tasks acts purely as
// a file-level inputs declaration (its `inputs:` are visible to every
// play's `when:` and to every task body); a play with tasks treats its
// inputs as play-local (visible to its tasks only).
func (p *Play) IsFileLevel() bool {
	return p != nil && len(p.Tasks.Keys()) == 0
}

// FileLevelInputNames returns the set of input names declared on
// "inputs-only" plays (plays with no tasks). Inputs on plays with
// tasks are play-local: their defaults are visible to that play's
// tasks but not to any play's `when:` and not to other plays' tasks.
//
// Used by the apply / plan executors to build the file-level context
// the spec requires for per-play `when:` evaluation: file-level input
// defaults plus user overrides only, without play-local defaults
// leaking in.
func FileLevelInputNames(plays []*Play) map[string]bool {
	out := map[string]bool{}
	for _, p := range plays {
		if !p.IsFileLevel() {
			continue
		}
		for _, in := range p.Inputs {
			if in.Name != "" {
				out[in.Name] = true
			}
		}
	}
	return out
}
