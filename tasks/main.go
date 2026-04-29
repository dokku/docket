package tasks

import (
	"crypto/rand"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"

	"github.com/dokku/docket/subprocess"
	sigil "github.com/gliderlabs/sigil"
	"github.com/gobuffalo/flect"
	defaults "github.com/mcuadros/go-defaults"
	json5 "github.com/titanous/json5"
	yaml "gopkg.in/yaml.v3"
)

// Task file format identifiers shared with the commands package.
//
// The empty string and any unrecognised value are treated as YAML so
// existing call sites that pass no format keep their pre-#218 behaviour.
const (
	FormatYAML       = "yaml"
	FormatNameJSON5  = "json5"
)

// IsJSON5Format returns true when format is one of the JSON5 aliases.
// Centralised so the json/json5 split lives in exactly one place.
func IsJSON5Format(format string) bool {
	return format == FormatNameJSON5 || format == "json"
}

// UnmarshalRecipe decodes data as a Recipe using the parser keyed by
// format. Exposed because the commands package's input-extraction path
// (parseInputDocument) needs the same dispatch and there is no benefit
// to duplicating the switch.
func UnmarshalRecipe(data []byte, format string) (Recipe, error) {
	recipe := Recipe{}
	if IsJSON5Format(format) {
		if err := json5.Unmarshal(data, &recipe); err != nil {
			return nil, fmt.Errorf("json5 unmarshal error: %v", err.Error())
		}
		return recipe, nil
	}
	if err := yaml.Unmarshal(data, &recipe); err != nil {
		return nil, fmt.Errorf("unmarshal error: %v", err.Error())
	}
	return recipe, nil
}

// State represents the desired state of a task
type State string

// State constants
const (
	// StatePresent represents the present state
	StatePresent State = "present"
	// StateAbsent represents the absent state
	StateAbsent State = "absent"
	// StateDeployed represents the deployed state
	StateDeployed State = "deployed"
	// StateSet represents the set state
	StateSet State = "set"
	// StateClear represents the clear state
	StateClear State = "clear"
	// StateSkipped is the sentinel value the apply / plan path emits when
	// a task's `when:` predicate is false. Both State and DesiredState are
	// set to this so the equality check in commands/apply.go does not flag
	// a skipped task as a state mismatch.
	StateSkipped State = "skipped"
)

// Recipe represents a docket recipe: a YAML list of plays. Each entry is
// a play envelope carrying the play-level metadata (name, tags, when,
// inputs) and the per-play tasks list. Single-play files are simply a
// one-element list and require no special handling.
type Recipe []RecipeEntry

// RecipeEntry is the on-disk shape of one play. The yaml-unmarshalled
// form; the runtime-facing Play struct (in play.go) is built from this
// by GetPlays.
type RecipeEntry struct {
	// Name is the play's user-facing label. Auto-generated as
	// "play #N" by GetPlays when omitted.
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Tags accepts either a YAML list (`tags: [a, b]`) or a scalar
	// (`tags: a`). Decoded via decodeTags into the Play.Tags slice.
	Tags interface{} `yaml:"tags,omitempty" json:"tags,omitempty"`

	// When is the raw expr source for the play-level conditional. Empty
	// means "always run". Compiled into the Play.whenProgram by GetPlays.
	When string `yaml:"when,omitempty" json:"when,omitempty"`

	// Inputs are the play-local input defaults. Layer above file-level
	// defaults but below --vars-file / CLI overrides (per-play merge
	// happens in GetPlays).
	Inputs []Input `yaml:"inputs,omitempty" json:"inputs,omitempty"`

	// Tasks is the raw per-play task list, decoded into envelopes by
	// GetPlays via buildEnvelopesForEntry.
	Tasks []map[string]interface{} `yaml:"tasks,omitempty" json:"tasks,omitempty"`
}

// Input represents an input for a task
type Input struct {
	// Name is the name of the input
	Name string `yaml:"name" json:"name"`

	// Default is the default value of the input
	Default string `yaml:"default" json:"default,omitempty"`

	// Description is the description of the input
	Description string `yaml:"description" json:"description,omitempty"`

	// Required is a flag indicating if the input is required
	Required bool `yaml:"required" json:"required,omitempty"`

	// Sensitive marks the input's resolved value as a secret. When true,
	// the value is masked as `***` anywhere it would otherwise appear in
	// user-facing output (apply --verbose echoes, plan output, error
	// messages, and the DOKKU_TRACE debug log).
	Sensitive bool `yaml:"sensitive" json:"sensitive,omitempty"`

	// Type is the type of the input
	Type string `yaml:"type" json:"type,omitempty"`

	// value is the value of the input
	value string
}

// TaskOutputState represents the output of a task
type TaskOutputState struct {
	// Changed is a flag indicating if the task was changed
	Changed bool

	// Commands records every resolved Dokku subprocess command line the
	// task's apply path executed, in invocation order. Used by
	// `docket apply --verbose` to echo one `→` continuation line per
	// command. Empty for tasks that did not invoke any subprocess.
	Commands []string

	// DesiredState is the desired state of the task
	DesiredState State

	// Error is the error of the task
	Error error

	// ExitCode is the exit code of the last subprocess command the task
	// executed. Zero when the call succeeded or no subprocess ran.
	ExitCode int

	// Message is the message of the task
	Message string

	// Meta is the meta of the task
	Meta struct{}

	// State is the state of the task
	State State

	// Stderr is the captured stderr of the last subprocess command the
	// task executed. Empty when no subprocess ran. Tasks that issue
	// multiple subprocess calls record only the final call's stderr;
	// per-call output, when needed, lives on Commands.
	Stderr string

	// Stdout is the captured stdout of the last subprocess command the
	// task executed. Empty when no subprocess ran. Same last-call-wins
	// rule as Stderr.
	Stdout string
}

// WithExecResult returns a copy of s with Stdout/Stderr/ExitCode populated
// from r. Callers use it from the success path so the returned state
// mirrors the underlying subprocess.ExecCommandResponse without having to
// assign each field by hand.
func (s TaskOutputState) WithExecResult(r subprocess.ExecCommandResponse) TaskOutputState {
	s.Stdout = r.Stdout
	s.Stderr = r.Stderr
	s.ExitCode = r.ExitCode
	return s
}

// PlanStatus is the short marker that summarizes a planned change.
type PlanStatus string

const (
	// PlanStatusOK indicates the task is in sync; no change would be made.
	PlanStatusOK PlanStatus = "ok"
	// PlanStatusModify indicates the task would modify existing state.
	PlanStatusModify PlanStatus = "~"
	// PlanStatusCreate indicates the task would create new state.
	PlanStatusCreate PlanStatus = "+"
	// PlanStatusDestroy indicates the task would remove existing state.
	PlanStatusDestroy PlanStatus = "-"
	// PlanStatusError indicates the read-state probe itself failed.
	PlanStatusError PlanStatus = "!"
)

// PlanResult is the read-only drift report for a task.
//
// Plan() never mutates server state. The unexported apply closure carries
// any state probed during planning so the apply path does not re-probe;
// ExecutePlan is the only consumer. When InSync is true, apply is nil.
type PlanResult struct {
	// InSync is true when the task would not change anything.
	InSync bool

	// Status is the short marker for the drift kind.
	Status PlanStatus

	// Reason is human-readable detail (e.g. "ref drift", "2 keys to set").
	Reason string

	// Mutations optionally itemizes per-mutation drift for tasks that
	// perform multiple operations (e.g. config setting and unsetting
	// individual keys). One entry per atomic change.
	Mutations []string

	// Commands is the resolved dokku command line(s) that ExecutePlan
	// would invoke if Plan reported drift, in invocation order. Tasks
	// populate it via subprocess.ResolveCommandString from the same
	// ExecCommandInput values the apply closure executes, so plan and
	// apply render byte-identical strings for the same operation.
	//
	// Contract: non-empty whenever Status is "+", "~", or "-" (drift);
	// empty when InSync is true or when Status is "!" (probe error).
	// Sensitive values are already masked because ResolveCommandString
	// runs MaskString on the rendered form.
	Commands []string

	// DesiredState mirrors TaskOutputState.DesiredState so plan output can
	// render the same context as apply output.
	DesiredState State

	// Error is non-nil when the read-state probe itself failed. A non-nil
	// Error implies Status == PlanStatusError.
	Error error

	// Stdout / Stderr / ExitCode capture the underlying subprocess
	// response that produced a probe error. Populated by probe call
	// sites that bubble a CallExecCommand failure into a PlanResult so
	// `failed_when` predicates referencing `result.Stderr` work in plan
	// mode the same way they do in apply mode. Empty / zero on the
	// in-sync, drift, and apply-only paths.
	Stdout   string
	Stderr   string
	ExitCode int

	// apply, when non-nil, is the closure ExecutePlan invokes to mutate
	// server state. nil when InSync. Captures any probed state needed for
	// the mutation so the apply path does not re-probe. Unexported so
	// formatters and JSON consumers cannot accidentally invoke it.
	apply func() TaskOutputState
}

// Task represents a task
type Task interface {
	// Doc returns the docblock for the task
	Doc() string

	// Examples returns the examples for the task
	Examples() ([]Doc, error)

	// Plan reports the drift the task would produce against the live server,
	// without mutating it. Plan must never call mutating dokku commands.
	Plan() PlanResult

	// Execute executes the task. Conventionally implemented as
	// ExecutePlan(t.Plan()) so probing happens once and the per-state
	// mutation logic lives only in Plan().
	Execute() TaskOutputState
}

// Global registry for Tasks.
var RegisteredTasks map[string]Task

// envelopeAllowlistKeys are the cross-cutting envelope keys the loader
// admits alongside the single task-type key. name / tags / when / loop
// are activated by #205; register / changed_when / failed_when /
// ignore_errors are reserved for #210 (the loader recognises and decodes
// them so #210 does not need to revisit the cap).
var envelopeAllowlistKeys = []string{
	"name",
	"tags",
	"when",
	"loop",
	"register",
	"changed_when",
	"failed_when",
	"ignore_errors",
	"block",
	"rescue",
	"always",
}

// envelopeAllowlistSet is envelopeAllowlistKeys as a lookup set.
var envelopeAllowlistSet = func() map[string]bool {
	m := make(map[string]bool, len(envelopeAllowlistKeys))
	for _, k := range envelopeAllowlistKeys {
		m[k] = true
	}
	return m
}()

// loopVarPlaceholder is the literal substitution sigil renders for `.item`
// and `.index` during the file-level pass. Keeping `{{ .item }}` /
// `{{ .index }}` intact through the first pass means loop expansion sees
// the original template and can render with real values. The loader
// rejects any task body that still contains these tokens after the
// per-task second pass, so misuse outside a loop is reported as a parse
// error.
const (
	loopItemPlaceholder  = "{{ .item }}"
	loopIndexPlaceholder = "{{ .index }}"
)

// loopVarSentinelPattern catches `{{ .item ... }}` and `{{ .index ... }}`
// references (any whitespace, optional sub-field access, optional
// pipelines) so they can be hidden from the file-level sigil pass and
// restored before loop expansion runs the second pass. The sub-match
// captures the full template token verbatim.
//
// Sub-field access (`{{ .item.app }}`) is the motivating case: with a
// scalar self-referencing placeholder, sigil errors when traversing a
// field on a string. Hiding the whole `{{ ... }}` token sidesteps the
// problem entirely.
var loopVarSentinelPattern = regexp.MustCompile(`\{\{[^}]*?\.(item|index)([^}]*)\}\}`)

// loopVarSentinelOpen / Close wrap escaped loop-var tokens during the
// file-level sigil pass. The pair must be unique enough to never appear
// in a real recipe; the prefix doubles as documentation when one of
// these survives a render error report.
const (
	loopVarSentinelOpen  = "__DOCKET_LOOPVAR<<"
	loopVarSentinelClose = ">>__"
)

// escapeLoopVars hides `{{ .item ... }}` / `{{ .index ... }}` tokens from
// sigil's file-level render. Returns the escaped data and the list of
// captured tokens in encounter order so unescapeLoopVars can restore
// them. Strings that contain no loop-var references round-trip unchanged.
func escapeLoopVars(data []byte) ([]byte, []string) {
	var captured []string
	out := loopVarSentinelPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		idx := len(captured)
		captured = append(captured, string(match))
		return []byte(fmt.Sprintf("%s%d%s", loopVarSentinelOpen, idx, loopVarSentinelClose))
	})
	return out, captured
}

// unescapeLoopVars reverses escapeLoopVars. Each sentinel
// `__DOCKET_LOOPVAR<<N>>__` is replaced with captured[N]. Sentinels that
// reference an out-of-range index are left untouched (defensive against
// upstream code that mangles the sentinel).
func unescapeLoopVars(data []byte, captured []string) []byte {
	if len(captured) == 0 {
		return data
	}
	out := data
	for i, tok := range captured {
		sentinel := fmt.Sprintf("%s%d%s", loopVarSentinelOpen, i, loopVarSentinelClose)
		out = []byte(strings.ReplaceAll(string(out), sentinel, tok))
	}
	return out
}

// RegisterTask registers a task
func RegisterTask(t Task) {
	if len(RegisteredTasks) == 0 {
		RegisteredTasks = make(map[string]Task)
	}

	var name string
	if t := reflect.TypeOf(t); t.Kind() == reflect.Ptr {
		name = "*" + t.Elem().Name()
	} else {
		name = t.Name()
	}

	name = flect.Underscore(name)
	RegisteredTasks[fmt.Sprintf("dokku_%s", strings.TrimSuffix(name, "_task"))] = t
}

// SetValue sets the value of the input
func (i *Input) SetValue(value string) error {
	i.value = value
	return nil
}

// HasValue returns true if the input has a value
func (i Input) HasValue() bool {
	return i.value != ""
}

// GetValue returns the value of the input
func (i Input) GetValue() string {
	return i.value
}

// GetTasks is a back-compat shim that returns the first play's task
// envelopes. New code should call GetPlays. The wrapper is kept because a
// large number of unit tests (tasks/*_test.go) exercise GetTasks directly
// against single-play recipes; those tests inspect the flat ordered map
// without caring about the multi-play envelope.
func GetTasks(data []byte, context map[string]interface{}) (OrderedStringEnvelopeMap, error) {
	plays, err := GetPlays(data, context, nil)
	if err != nil {
		return OrderedStringEnvelopeMap{}, err
	}
	if len(plays) == 0 {
		return OrderedStringEnvelopeMap{}, nil
	}
	return plays[0].Tasks, nil
}

// GetPlays parses data as a docket recipe and returns one Play per
// top-level entry, each carrying its own envelope map. The executor
// (commands/apply.go, commands/plan.go) walks the result in order.
//
// The render pipeline is:
//
//  1. Render the whole file with `context` (file-level inputs + vars-file
//     + CLI overrides) for the structure pass. This catches template
//     syntax errors at the same point GetTasks did before #208 and gives
//     us the play count plus per-play metadata (name/tags/when/inputs).
//  2. For each play, build a per-play context by layering the play's own
//     `inputs:` defaults above file-level defaults, but only for keys the
//     user has not overridden via --vars-file or CLI. The userSet map
//     identifies user-overridden keys; pass nil to disable the layering
//     (the GetTasks shim does this since back-compat tests do not need
//     multi-play context).
//  3. Re-render the whole file with the per-play context so task body
//     templates substitute the play-local values, then walk to that
//     play's tasks and build envelopes through the existing
//     buildEnvelopesForEntry helper.
//  4. Append the play's tags to every envelope (additive with per-task
//     tags) so FilterByTags treats them uniformly.
//
// Per-play `when:` predicates are pre-compiled here; the executor decides
// the evaluation context (file-level only, per the spec - the play's own
// inputs are not visible to its own when).
func GetPlays(data []byte, context map[string]interface{}, userSet map[string]bool) ([]*Play, error) {
	return GetPlaysWithFormat(data, FormatYAML, context, userSet)
}

// GetPlaysWithFormat is the format-aware variant of GetPlays. format is
// one of "yaml" / "json5"; the empty string is treated as YAML. The
// per-format dispatch happens at every parse point inside the function
// so sigil templates render uniformly across both surfaces.
func GetPlaysWithFormat(data []byte, format string, context map[string]interface{}, userSet map[string]bool) ([]*Play, error) {
	baseRendered, err := renderRecipeBytes(data, context)
	if err != nil {
		return nil, err
	}

	baseRecipe, err := UnmarshalRecipe(baseRendered, format)
	if err != nil {
		return nil, err
	}

	if len(baseRecipe) == 0 {
		return nil, fmt.Errorf("parse error: no recipe found in tasks file")
	}

	plays := make([]*Play, 0, len(baseRecipe))
	singleUnnamed := len(baseRecipe) == 1 && baseRecipe[0].Name == ""
	for i, raw := range baseRecipe {
		play := &Play{
			Name:   raw.Name,
			When:   raw.When,
			Inputs: raw.Inputs,
		}
		if play.Name == "" {
			// Single-play recipes without a name keep the legacy
			// "tasks" header so existing recipes do not see a
			// visual diff after #208. Multi-play recipes get
			// numbered auto-names so each play header is distinct.
			if singleUnnamed {
				play.Name = "tasks"
			} else {
				play.Name = fmt.Sprintf("play #%d", i+1)
			}
		}

		if raw.Tags != nil {
			tags, err := decodeTags(raw.Tags)
			if err != nil {
				return nil, fmt.Errorf("play parse error: play #%d %q: %s", i+1, play.Name, err)
			}
			play.Tags = tags
		}

		if play.When != "" {
			prog, err := CompilePredicate(play.When)
			if err != nil {
				return nil, fmt.Errorf("play parse error: play #%d %q: when compile error: %s", i+1, play.Name, err)
			}
			play.whenProgram = prog
		}

		playCtx := BuildPerPlayContext(context, play.Inputs, userSet)
		perPlayRecipe, err := renderRecipeWithFormat(data, format, playCtx)
		if err != nil {
			return nil, err
		}
		if i >= len(perPlayRecipe) {
			return nil, fmt.Errorf("play parse error: play #%d %q: per-play render produced fewer plays than the structure pass", i+1, play.Name)
		}

		exprCtx := buildExprContext(playCtx)
		play.Tasks = OrderedStringEnvelopeMap{}
		for j, t := range perPlayRecipe[i].Tasks {
			envelopes, err := buildEnvelopesForEntry(j+1, t, playCtx, exprCtx)
			if err != nil {
				return nil, err
			}
			for _, env := range envelopes {
				if len(play.Tags) > 0 {
					env.Tags = mergePlayTags(env.Tags, play.Tags)
				}
				play.Tasks.Set(env.Name, env)
			}
		}

		plays = append(plays, play)
	}

	return plays, nil
}

// renderRecipeBytes runs the loop-var-safe sigil render over data with the
// given context and returns the rendered bytes. Pulled out of the legacy
// GetTasks so GetPlays can reuse it across the structure pass and per-play
// passes.
func renderRecipeBytes(data []byte, context map[string]interface{}) ([]byte, error) {
	escaped, captured := escapeLoopVars(data)
	render, err := sigil.Execute(escaped, context, "tasks")
	if err != nil {
		return nil, fmt.Errorf("re-render error: %v", err.Error())
	}
	rendered, err := io.ReadAll(&render)
	if err != nil {
		return nil, fmt.Errorf("read error: %v", err.Error())
	}
	return unescapeLoopVars(rendered, captured), nil
}

// renderRecipe is the convenience wrapper that renders data with context
// and unmarshals the result into a Recipe.
func renderRecipe(data []byte, context map[string]interface{}) (Recipe, error) {
	return renderRecipeWithFormat(data, FormatYAML, context)
}

// renderRecipeWithFormat is renderRecipe's format-aware variant.
func renderRecipeWithFormat(data []byte, format string, context map[string]interface{}) (Recipe, error) {
	rendered, err := renderRecipeBytes(data, context)
	if err != nil {
		return nil, err
	}
	return UnmarshalRecipe(rendered, format)
}

// BuildPerPlayContext layers the play's `inputs:` defaults above the
// file-level base context, but only for keys the user has not explicitly
// overridden via --vars-file or CLI flags. The userSet map carries the
// names of user-overridden inputs; nil disables the layering, which is
// the GetTasks back-compat behaviour. Per-play defaults with an empty
// Default string are skipped so they cannot accidentally shadow a real
// file-level value with "".
//
// Exported because the apply / plan executors need to build the same
// per-play context the loader used so per-task `when:` predicates see
// the same values as the rendered task bodies.
func BuildPerPlayContext(base map[string]interface{}, playInputs []Input, userSet map[string]bool) map[string]interface{} {
	out := make(map[string]interface{}, len(base)+len(playInputs))
	for k, v := range base {
		out[k] = v
	}
	for _, in := range playInputs {
		if in.Name == "" {
			continue
		}
		if userSet[in.Name] {
			continue
		}
		if in.Default == "" {
			continue
		}
		out[in.Name] = in.Default
	}
	return out
}

// mergePlayTags appends playTags onto envTags, dropping duplicates so a
// task that declares the same tag as the enclosing play does not see it
// twice. envTags' original order is preserved; new tags from playTags
// land at the end.
func mergePlayTags(envTags, playTags []string) []string {
	if len(playTags) == 0 {
		return envTags
	}
	seen := make(map[string]bool, len(envTags))
	for _, t := range envTags {
		seen[t] = true
	}
	out := append([]string(nil), envTags...)
	for _, t := range playTags {
		if seen[t] {
			continue
		}
		out = append(out, t)
		seen[t] = true
	}
	return out
}

// buildEnvelopesForEntry walks a single task entry, partitions envelope
// keys vs the task-type key, decodes the body, pre-compiles `when:`, and
// expands `loop:` if present. Returns one or more envelopes ready for
// insertion into the ordered map. When the entry carries `block:`, it is
// decoded as a try/catch/finally group: child envelopes are produced
// recursively from the block / rescue / always lists and the wrapping
// envelope's Task is left nil.
func buildEnvelopesForEntry(index int, entry map[string]interface{}, sigilContext, exprContext map[string]interface{}) ([]*TaskEnvelope, error) {
	envelope := &TaskEnvelope{}

	var (
		taskTypeKey  string
		taskBody     interface{}
		taskTypeKeys []string
		unknownKeys  []string
		blockRaw     interface{}
		rescueRaw    interface{}
		alwaysRaw    interface{}
		hasBlock     bool
		hasRescue    bool
		hasAlways    bool
	)

	for key, value := range entry {
		switch key {
		case "name":
			if s, ok := value.(string); ok {
				envelope.Name = s
			}
		case "tags":
			tags, err := decodeTags(value)
			if err != nil {
				return nil, fmt.Errorf("task parse error: task #%d: %s", index, err)
			}
			envelope.Tags = tags
		case "when":
			s, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("task parse error: task #%d: when must be a string expression, got %T", index, value)
			}
			envelope.When = s
		case "loop":
			envelope.Loop = value
		case "register":
			s, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("task parse error: task #%d: register must be a string, got %T", index, value)
			}
			envelope.Register = s
		case "changed_when":
			s, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("task parse error: task #%d: changed_when must be a string expression, got %T", index, value)
			}
			envelope.ChangedWhen = s
		case "failed_when":
			s, ok := value.(string)
			if !ok {
				return nil, fmt.Errorf("task parse error: task #%d: failed_when must be a string expression, got %T", index, value)
			}
			envelope.FailedWhen = s
		case "ignore_errors":
			b, ok := value.(bool)
			if !ok {
				return nil, fmt.Errorf("task parse error: task #%d: ignore_errors must be a bool, got %T", index, value)
			}
			envelope.IgnoreErrors = b
		case "block":
			blockRaw = value
			hasBlock = true
		case "rescue":
			rescueRaw = value
			hasRescue = true
		case "always":
			alwaysRaw = value
			hasAlways = true
		default:
			if _, registered := RegisteredTasks[key]; registered {
				taskTypeKeys = append(taskTypeKeys, key)
				taskTypeKey = key
				taskBody = value
				continue
			}
			unknownKeys = append(unknownKeys, key)
		}
	}

	if envelope.Name == "" {
		generated, err := generateTaskName(index)
		if err != nil {
			return nil, err
		}
		envelope.Name = generated
	}

	if len(unknownKeys) > 0 {
		return nil, unknownKeyError(index, envelope.Name, unknownKeys)
	}

	isGroup := hasBlock
	if hasRescue && !hasBlock {
		return nil, fmt.Errorf("task parse error: task #%d %q has rescue: without block:", index, envelope.Name)
	}
	if hasAlways && !hasBlock {
		return nil, fmt.Errorf("task parse error: task #%d %q has always: without block:", index, envelope.Name)
	}

	if isGroup {
		if len(taskTypeKeys) > 0 {
			return nil, fmt.Errorf("task parse error: task #%d %q is a block: group and cannot also carry task-type key %q", index, envelope.Name, taskTypeKeys[0])
		}
	} else {
		if len(taskTypeKeys) == 0 {
			return nil, fmt.Errorf("task parse error: task #%d %q was not a valid task - valid_tasks=%v", index, envelope.Name, registeredTaskNamesSorted())
		}
		if len(taskTypeKeys) > 1 {
			return nil, fmt.Errorf("task parse error: task #%d %q has %d task-type keys (%s); exactly one is allowed", index, envelope.Name, len(taskTypeKeys), strings.Join(taskTypeKeys, ", "))
		}
	}

	if envelope.When != "" {
		prog, err := CompilePredicate(envelope.When)
		if err != nil {
			return nil, fmt.Errorf("task parse error: task #%d %q: when compile error: %s", index, envelope.Name, err)
		}
		envelope.whenProgram = prog
	}

	if envelope.ChangedWhen != "" {
		prog, err := CompilePredicate(envelope.ChangedWhen)
		if err != nil {
			return nil, fmt.Errorf("task parse error: task #%d %q: changed_when compile error: %s", index, envelope.Name, err)
		}
		envelope.changedWhenProgram = prog
	}

	if envelope.FailedWhen != "" {
		prog, err := CompilePredicate(envelope.FailedWhen)
		if err != nil {
			return nil, fmt.Errorf("task parse error: task #%d %q: failed_when compile error: %s", index, envelope.Name, err)
		}
		envelope.failedWhenProgram = prog
	}

	if isGroup {
		if envelope.Loop != nil {
			expanded, err := expandLoopGroup(envelope, blockRaw, rescueRaw, alwaysRaw, sigilContext, exprContext)
			if err != nil {
				return nil, fmt.Errorf("task parse error: task #%d %q: %s", index, envelope.Name, err)
			}
			return expanded, nil
		}

		envelope.TypeName = ""
		blockChildren, err := decodeGroupClause(blockRaw, "block", envelope.Name, sigilContext, exprContext)
		if err != nil {
			return nil, fmt.Errorf("task parse error: task #%d %q: %s", index, envelope.Name, err)
		}
		if len(blockChildren) == 0 {
			return nil, fmt.Errorf("task parse error: task #%d %q: block: must contain at least one child task", index, envelope.Name)
		}
		envelope.Block = blockChildren

		if hasRescue {
			rescueChildren, err := decodeGroupClause(rescueRaw, "rescue", envelope.Name, sigilContext, exprContext)
			if err != nil {
				return nil, fmt.Errorf("task parse error: task #%d %q: %s", index, envelope.Name, err)
			}
			envelope.Rescue = rescueChildren
		}
		if hasAlways {
			alwaysChildren, err := decodeGroupClause(alwaysRaw, "always", envelope.Name, sigilContext, exprContext)
			if err != nil {
				return nil, fmt.Errorf("task parse error: task #%d %q: %s", index, envelope.Name, err)
			}
			envelope.Always = alwaysChildren
		}

		return []*TaskEnvelope{envelope}, nil
	}

	envelope.TypeName = taskTypeKey
	registered := RegisteredTasks[taskTypeKey]

	bodyBytes, err := yaml.Marshal(taskBody)
	if err != nil {
		return nil, fmt.Errorf("task parse error: task #%d %q failed to marshal config to yaml - %s", index, envelope.Name, err)
	}

	if envelope.Loop != nil {
		expanded, err := expandLoop(envelope, taskBody, registered, sigilContext, exprContext)
		if err != nil {
			return nil, fmt.Errorf("task parse error: task #%d %q: %s", index, envelope.Name, err)
		}
		for _, exp := range expanded {
			if err := rejectLoopVarsInTask(index, exp.Name, exp.Task); err != nil {
				return nil, err
			}
		}
		return expanded, nil
	}

	taskValue := reflect.New(reflect.TypeOf(registered))
	if err := yaml.Unmarshal(bodyBytes, taskValue.Interface()); err != nil {
		return nil, fmt.Errorf("task parse error: task #%d %q failed to decode to %s - %s", index, envelope.Name, taskTypeKey, err)
	}
	task := taskValue.Elem().Interface().(Task)
	defaults.SetDefaults(task)
	envelope.Task = task

	if err := rejectLoopVarsInTask(index, envelope.Name, task); err != nil {
		return nil, err
	}

	return []*TaskEnvelope{envelope}, nil
}

// decodeGroupClause decodes one of `block:` / `rescue:` / `always:` into
// a flat slice of child envelopes. The clause value must be a sequence
// of YAML mappings (`[]interface{}` of `map[string]interface{}` after
// the YAML unmarshal); each mapping is recursed through
// buildEnvelopesForEntry so child entries themselves may carry envelope
// keys, loop expansions, or nested groups.
func decodeGroupClause(raw interface{}, clause, parentName string, sigilContext, exprContext map[string]interface{}) ([]*TaskEnvelope, error) {
	if raw == nil {
		return nil, nil
	}
	rawList, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%s: must be a list of task entries, got %T", clause, raw)
	}
	out := make([]*TaskEnvelope, 0, len(rawList))
	for i, item := range rawList {
		entry, ok := item.(map[string]interface{})
		if !ok {
			if mapped, mapOk := coerceStringKeyMap(item); mapOk {
				entry = mapped
			} else {
				return nil, fmt.Errorf("%s[%d]: child entry must be a yaml mapping, got %T", clause, i, item)
			}
		}
		childEnvelopes, err := buildEnvelopesForEntry(i+1, entry, sigilContext, exprContext)
		if err != nil {
			return nil, fmt.Errorf("%s[%d]: %s", clause, i, err)
		}
		out = append(out, childEnvelopes...)
	}
	return out, nil
}

// coerceStringKeyMap converts a yaml-decoded map[interface{}]interface{}
// into a map[string]interface{} so nested entries match the shape
// buildEnvelopesForEntry expects. yaml.v3 produces map[string]interface{}
// out of the box, but defensive code keeps the loader robust against
// custom unmarshallers.
func coerceStringKeyMap(item interface{}) (map[string]interface{}, bool) {
	if entry, ok := item.(map[string]interface{}); ok {
		return entry, true
	}
	if entry, ok := item.(map[interface{}]interface{}); ok {
		out := make(map[string]interface{}, len(entry))
		for k, v := range entry {
			ks, ok := k.(string)
			if !ok {
				return nil, false
			}
			out[ks] = v
		}
		return out, true
	}
	return nil, false
}

// decodeTags coerces a yaml-parsed tags value into a []string. Supports
// list-form (`tags: [foo, bar]`) and inline string-form (`tags: foo`).
func decodeTags(value interface{}) ([]string, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case string:
		return []string{v}, nil
	case []interface{}:
		out := make([]string, 0, len(v))
		for i, raw := range v {
			s, ok := raw.(string)
			if !ok {
				return nil, fmt.Errorf("tags[%d] must be a string, got %T", i, raw)
			}
			out = append(out, s)
		}
		return out, nil
	}
	return nil, fmt.Errorf("tags must be a list of strings, got %T", value)
}

// generateTaskName returns a unique task name when the user did not
// supply one. The format mirrors the legacy `task #N XXXX` pattern.
func generateTaskName(index int) (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("task parse error: task #%d had no task name and there was a failure to generate random task name - %s", index, err)
	}
	return fmt.Sprintf("task #%d %X", index, b), nil
}

// unknownKeyError builds a parse error for an entry with one or more
// unknown keys, including a "did you mean" suggestion when the closest
// match is within Levenshtein distance 2.
func unknownKeyError(index int, name string, unknown []string) error {
	primary := unknown[0]
	suggestion := nearestEnvelopeOrTaskKey(primary)
	hint := ""
	if suggestion != "" {
		hint = fmt.Sprintf(" - did you mean %q?", suggestion)
	}
	return fmt.Errorf("task parse error: task #%d %q has unknown envelope key %q (allowed: %s, or any registered task type)%s", index, name, primary, strings.Join(envelopeAllowlistKeys, ", "), hint)
}

// nearestEnvelopeOrTaskKey returns the envelope-allowlist or registered
// task name with the lowest Levenshtein distance to candidate, but only
// if that distance is at most 2.
func nearestEnvelopeOrTaskKey(candidate string) string {
	best := ""
	bestDist := 3
	for _, k := range envelopeAllowlistKeys {
		d := levenshtein(candidate, k)
		if d < bestDist {
			bestDist = d
			best = k
		}
	}
	for k := range RegisteredTasks {
		d := levenshtein(candidate, k)
		if d < bestDist {
			bestDist = d
			best = k
		}
	}
	if bestDist <= 2 {
		return best
	}
	return ""
}

// registeredTaskNamesSorted returns the registered task names sorted
// alphabetically. Used for error messages so the output is stable.
func registeredTaskNamesSorted() []string {
	names := make([]string, 0, len(RegisteredTasks))
	for k := range RegisteredTasks {
		names = append(names, k)
	}
	// Bubble-sort works fine for ~50 entries and avoids the import cost.
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return names
}

// buildExprContext returns the file-level expr context. Today this is
// just the inputs map; later issues add timestamp / host / play / result
// / registered keys (#208 / #210). Keys are reserved here but not yet
// populated.
func buildExprContext(context map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(context))
	for k, v := range context {
		out[k] = v
	}
	return out
}

// rejectLoopVarsInTask scans every string field on task for surviving
// `{{ .item }}` / `{{ .index }}` references and returns an error when
// it finds one. Loop expansions render those tokens to real values, so
// any survivor implies the user referenced a loop variable from a
// non-loop task.
func rejectLoopVarsInTask(index int, name string, task Task) error {
	bytes, err := yaml.Marshal(task)
	if err != nil {
		return nil
	}
	body := string(bytes)
	if strings.Contains(body, ".item") && (strings.Contains(body, "{{ .item }}") || strings.Contains(body, "{{.item}}")) {
		return fmt.Errorf("task parse error: task #%d %q: .item is only available inside a loop body", index, name)
	}
	if strings.Contains(body, ".index") && (strings.Contains(body, "{{ .index }}") || strings.Contains(body, "{{.index}}")) {
		return fmt.Errorf("task parse error: task #%d %q: .index is only available inside a loop body", index, name)
	}
	return nil
}
