package tasks

import (
	"crypto/rand"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"

	sigil "github.com/gliderlabs/sigil"
	"github.com/gobuffalo/flect"
	defaults "github.com/mcuadros/go-defaults"
	yaml "gopkg.in/yaml.v3"
)

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

// Recipe represents a recipe for a task
type Recipe []struct {
	// Inputs are the inputs for the task
	Inputs []Input `yaml:"inputs,omitempty"`

	// Tasks are the tasks for the recipe
	Tasks []map[string]interface{} `yaml:"tasks,omitempty"`
}

// Input represents an input for a task
type Input struct {
	// Name is the name of the input
	Name string `yaml:"name"`

	// Default is the default value of the input
	Default string `yaml:"default"`

	// Description is the description of the input
	Description string `yaml:"description"`

	// Required is a flag indicating if the input is required
	Required bool `yaml:"required"`

	// Sensitive marks the input's resolved value as a secret. When true,
	// the value is masked as `***` anywhere it would otherwise appear in
	// user-facing output (apply --verbose echoes, plan output, error
	// messages, and the DOKKU_TRACE debug log).
	Sensitive bool `yaml:"sensitive"`

	// Type is the type of the input
	Type string `yaml:"type"`

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

	// Message is the message of the task
	Message string

	// Meta is the meta of the task
	Meta struct{}

	// State is the state of the task
	State State
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

// GetTasks parses data as a docket recipe and returns the per-task
// envelopes the executor consumes. The pipeline is:
//
//  1. Inject `.item` / `.index` self-reference placeholders into the
//     sigil context so loop-body templates pass through the file-level
//     render intact.
//  2. Sigil-render the file with the augmented context.
//  3. YAML-unmarshal into a Recipe.
//  4. For each task entry: partition envelope keys vs the task-type key,
//     reject unknown keys with a "did you mean" hint, decode envelope
//     fields, decode task body, pre-compile any `when:` predicate.
//  5. If `loop:` is present, expand into N envelopes via expandLoop;
//     otherwise emit one envelope.
//  6. Reject any envelope whose final body still contains
//     `{{ .item }}` / `{{ .index }}` (i.e. the user referenced a loop
//     variable from a non-loop task).
func GetTasks(data []byte, context map[string]interface{}) (OrderedStringEnvelopeMap, error) {
	tasks := OrderedStringEnvelopeMap{}

	escaped, captured := escapeLoopVars(data)

	render, err := sigil.Execute(escaped, context, "tasks")
	if err != nil {
		return tasks, fmt.Errorf("re-render error: %v", err.Error())
	}

	rendered, err := io.ReadAll(&render)
	if err != nil {
		return tasks, fmt.Errorf("read error: %v", err.Error())
	}

	out := unescapeLoopVars(rendered, captured)

	recipe := Recipe{}
	if err := yaml.Unmarshal([]byte(out), &recipe); err != nil {
		return tasks, fmt.Errorf("unmarshal error: %v", err.Error())
	}

	if len(recipe) == 0 {
		return tasks, fmt.Errorf("parse error: no recipe found in tasks file")
	}

	exprContext := buildExprContext(context)

	for i, t := range recipe[0].Tasks {
		envelopes, err := buildEnvelopesForEntry(i+1, t, context, exprContext)
		if err != nil {
			return tasks, err
		}
		for _, env := range envelopes {
			tasks.Set(env.Name, env)
		}
	}

	return tasks, nil
}

// buildEnvelopesForEntry walks a single task entry, partitions envelope
// keys vs the task-type key, decodes the body, pre-compiles `when:`, and
// expands `loop:` if present. Returns one or more envelopes ready for
// insertion into the ordered map.
func buildEnvelopesForEntry(index int, entry map[string]interface{}, sigilContext, exprContext map[string]interface{}) ([]*TaskEnvelope, error) {
	envelope := &TaskEnvelope{}

	var (
		taskTypeKey  string
		taskBody     interface{}
		taskTypeKeys []string
		unknownKeys  []string
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

	if len(taskTypeKeys) == 0 {
		return nil, fmt.Errorf("task parse error: task #%d %q was not a valid task - valid_tasks=%v", index, envelope.Name, registeredTaskNamesSorted())
	}
	if len(taskTypeKeys) > 1 {
		return nil, fmt.Errorf("task parse error: task #%d %q has %d task-type keys (%s); exactly one is allowed", index, envelope.Name, len(taskTypeKeys), strings.Join(taskTypeKeys, ", "))
	}

	envelope.TypeName = taskTypeKey
	registered := RegisteredTasks[taskTypeKey]

	bodyBytes, err := yaml.Marshal(taskBody)
	if err != nil {
		return nil, fmt.Errorf("task parse error: task #%d %q failed to marshal config to yaml - %s", index, envelope.Name, err)
	}

	if envelope.When != "" {
		prog, err := CompilePredicate(envelope.When)
		if err != nil {
			return nil, fmt.Errorf("task parse error: task #%d %q: when compile error: %s", index, envelope.Name, err)
		}
		envelope.whenProgram = prog
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
