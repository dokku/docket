package tasks

import (
	"fmt"
	"io"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"

	sigil "github.com/gliderlabs/sigil"
	defaults "github.com/mcuadros/go-defaults"
	json5 "github.com/titanous/json5"
	yaml "gopkg.in/yaml.v3"
)

// json5ToYAMLBytes parses data as JSON5 and re-emits it as YAML so the
// rest of the validator (which is yaml.v3 Node-based) can operate
// uniformly. Used at the top of Validate; sigil templates inside string
// values survive verbatim since they are just text to both parsers.
func json5ToYAMLBytes(data []byte) ([]byte, error) {
	var raw interface{}
	if err := json5.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("json5 parse error: %v", err)
	}
	return yaml.Marshal(raw)
}

// Problem is a single validation finding emitted by Validate.
//
// Code is a stable machine-readable identifier so JSON consumers (and the
// follow-on issues that activate the placeholder checks) can branch without
// matching on free-form Message text. Line/Column are 1-based and 0 when the
// position cannot be derived from yaml.v3 anchors or the underlying parser.
type Problem struct {
	Code    string
	Play    string
	Task    string
	Line    int
	Column  int
	Message string
	Hint    string
}

// ValidateOptions controls optional checks. Strict turns on the input-resolution
// audit; InputOverrides is the set of input names whose values were supplied on
// the CLI (the same map registerInputFlags fills in for apply/plan).
//
// PlayName and StartAtTask carry the values of `validate --play` and
// `validate --start-at-task` respectively. They power the cross-reference
// audit that runs under `--strict`: each non-empty value must resolve to a
// real play / task name in the recipe.
//
// Format selects the on-disk surface syntax: "yaml" (default) or "json5".
// JSON5 input is normalised to YAML bytes once at the top of Validate so
// the rest of the pipeline (sigil render, yaml.Node walk, line/column
// reporting) keeps a single implementation. Line / column numbers in
// problems for a JSON5 file therefore index into the normalised form,
// not the original JSON5 source.
type ValidateOptions struct {
	Strict         bool
	InputOverrides map[string]bool
	PlayName       string
	StartAtTask    string
	Format         string
}

// validatePlaceholder is substituted for any input that has no default during
// the sigil-render check. It is unique enough that no real recipe would
// produce it accidentally; downstream type-decoding sees it as a normal
// string, which is the right behaviour for offline validation since we do
// not know what the runtime value will be.
const validatePlaceholder = "__docket_validate_placeholder__"

// reservedEnvelopeKeys lists the per-task keys that are recognised at the
// envelope level but not yet activated. They live here so a typo in a real
// task-type name (e.g. dokku_appp) is reported as "unknown task type" rather
// than getting silently swallowed by an envelope allowlist. Empty today
// because #211 promoted block / rescue / always to active; kept around so
// future issues can reserve names without revisiting validateTaskEntry.
var reservedEnvelopeKeys = map[string]string{}

// activeEnvelopeKeys are the envelope keys the validator recognises and
// passes through to the loader without generating an "envelope key
// reserved" diagnostic. Keep in sync with envelopeAllowlistKeys in
// tasks/main.go.
var activeEnvelopeKeys = map[string]bool{
	"name":          true,
	"tags":          true,
	"when":          true,
	"loop":          true,
	"register":      true,
	"changed_when":  true,
	"failed_when":   true,
	"ignore_errors": true,
	"block":         true,
	"rescue":        true,
	"always":        true,
}

// groupClauseKeys is the subset of envelope keys that carry a list of
// nested task entries for a try/catch/finally group (#211). Used by the
// loader and validator to recognise group entries.
var groupClauseKeys = []string{"block", "rescue", "always"}

// allowedPlayKeys is the set of play-level mapping keys the validator
// admits without flagging as unexpected. #208 extends the legacy
// inputs/tasks pair with name/tags/when at the play envelope.
var allowedPlayKeys = map[string]bool{
	"name":   true,
	"tags":   true,
	"when":   true,
	"inputs": true,
	"tasks":  true,
}

// allowedPlayKeysList is the comma-separated form rendered into the
// "unexpected play key" diagnostic so users see the full allowlist.
const allowedPlayKeysList = "name, tags, when, inputs, tasks"

// Validate parses data as a docket recipe and returns every problem it finds.
// It is offline by contract: the implementation must never invoke
// subprocess.CallExecCommand.
//
// The validator first parses the raw bytes to extract input definitions, then
// renders the recipe through sigil with a context built from those defaults
// (plus a placeholder for any required input without a default) so structural
// checks operate on the same form `apply` would see. yaml.v3 line/column
// anchors from the rendered tree are used for error reporting. Source line
// numbers align with rendered line numbers as long as templates render
// inline, which is the case for typical recipes.
func Validate(data []byte, opts ValidateOptions) []Problem {
	if opts.InputOverrides == nil {
		opts.InputOverrides = map[string]bool{}
	}

	// Retained before normalization so a post-render parse failure can be
	// re-diagnosed against the original recipe (see diagnoseUnsafeInputValue).
	rawData := data

	// An input name that is not a valid template variable (e.g. a hyphen)
	// would otherwise make the render below fail with a cryptic "bad
	// character" error. Check names first so the clearer invalid_input_name
	// diagnostic wins.
	if nameProblems := checkInputNames(data, opts.Format); len(nameProblems) > 0 {
		return nameProblems
	}

	// Sigil renders templates, so a malformed `{{ .x` is caught here even
	// when the rest of the file is otherwise unparseable as YAML (the YAML
	// parser would otherwise misread `{{` as a broken flow mapping). The
	// initial render uses an empty context so input-default substitution is
	// not yet applied; text/template's `missingkey=default` mode renders
	// missing keys as `<no value>` rather than failing, so this pass only
	// surfaces real template syntax errors.
	if _, renderProblem := renderForValidate(data, map[string]interface{}{}); renderProblem != nil {
		return []Problem{*renderProblem}
	}

	// JSON5 is normalised to YAML bytes once so the AST walk below stays
	// yaml.v3-only. The JSON5 parse runs after the sigil syntax check so
	// a broken template surfaces as a template_error rather than a
	// confusing json5 parse error.
	normalized, normalizeProblems := normalizeRecipeBytes(data, opts.Format)
	if len(normalizeProblems) > 0 {
		return normalizeProblems
	}
	data = normalized

	var rawRoot yaml.Node
	if err := yaml.Unmarshal(data, &rawRoot); err != nil {
		line, col := parseYAMLErrorPosition(err.Error())
		return []Problem{{
			Code:    "yaml_parse",
			Line:    line,
			Column:  col,
			Message: err.Error(),
		}}
	}

	rawDoc := documentBody(&rawRoot)
	if rawDoc == nil {
		return []Problem{{
			Code:    "recipe_shape",
			Message: "no recipe found in tasks file",
		}}
	}
	if rawDoc.Kind != yaml.SequenceNode {
		return []Problem{{
			Code:    "recipe_shape",
			Line:    rawDoc.Line,
			Column:  rawDoc.Column,
			Message: "recipe must be a yaml list of plays",
		}}
	}

	playsInputs := extractPlayInputs(rawDoc)
	context := buildSigilContext(playsInputs)

	rendered, renderProblem := renderForValidate(data, context)
	if renderProblem != nil {
		// Without rendered bytes the structural walk cannot run; return the
		// template error alongside whatever input-strict findings can still
		// be derived from the raw tree.
		problems := []Problem{*renderProblem}
		if opts.Strict {
			for i, inputs := range playsInputs {
				label := fmt.Sprintf("play #%d", i+1)
				problems = append(problems, validateStrictInputs(inputs, opts.InputOverrides, label)...)
			}
		}
		return problems
	}

	// The shared structural parser walks the rendered tree once, collecting
	// shape / envelope / task-type problems with source positions. The
	// loader (GetPlaysWithFormat) runs the same parser, so apply, plan, and
	// validate agree on structural validity by construction. The passes
	// below this point are validate-only: they need placeholder-aware body
	// decoding or offline-only cross-reference auditing.
	ast := parseRecipe(rendered)
	// A yaml_parse problem after rendering with real input values may be an
	// input value that broke its surrounding scalar (#371). Re-diagnose so the
	// operator gets an input-named message instead of a cryptic YAML error;
	// parseRecipe short-circuits on yaml_parse, so ast carries nothing else.
	for _, p := range ast.Problems {
		if p.Code == "yaml_parse" {
			if diag := diagnoseUnsafeInputValue(rawData, opts.Format, context); diag != nil {
				return []Problem{*diag}
			}
			break
		}
	}
	problems := append([]Problem(nil), ast.Problems...)

	// registerSeen tracks register names across the whole recipe so a
	// duplicate `register: foo` in two different plays surfaces as a
	// single register_duplicate problem. The registered map is run-wide
	// at apply / plan time, so the validator's uniqueness check is
	// recipe-wide too.
	registerSeen := map[string]registerHit{}
	for i, play := range ast.Plays {
		var inputs []inputWithNode
		if i < len(playsInputs) {
			inputs = playsInputs[i]
		}
		problems = append(problems, validatePlay(play, inputs, opts, registerSeen)...)
	}

	problems = append(problems, validateCLIReferences(ast.Doc, opts)...)

	return problems
}

// validateCLIReferences checks that the values of `validate --play` and
// `validate --start-at-task` resolve to real play / task names in the
// recipe. The audit runs only under --strict so casual `validate` runs
// (without --strict) never error solely on missing CLI references.
//
// --start-at-task is checked against task `name:` fields walked through
// every play's `tasks:` and through the block / rescue / always
// children of group entries (#211). Loop expansions are not enumerated
// here because their per-iteration suffix is only resolved at apply
// time; the runtime check in commands/apply.go is authoritative.
func validateCLIReferences(doc *yaml.Node, opts ValidateOptions) []Problem {
	if doc == nil || !opts.Strict {
		return nil
	}
	if opts.PlayName == "" && opts.StartAtTask == "" {
		return nil
	}

	var problems []Problem
	var playNames []string
	taskNamesByPlay := map[string][]string{}
	var allTaskNames []string
	seen := map[string]bool{}

	for i, play := range doc.Content {
		if play.Kind != yaml.MappingNode {
			continue
		}
		playLabel := scalarChild(play, "name")
		if playLabel == "" {
			playLabel = fmt.Sprintf("play #%d", i+1)
		}
		playNames = append(playNames, playLabel)

		tasksNode := mappingValue(play, "tasks")
		if tasksNode == nil || tasksNode.Kind != yaml.SequenceNode {
			continue
		}
		var names []string
		walkGroupEntries(tasksNode, playLabel, func(task *yaml.Node, _ string) {
			n := scalarChild(task, "name")
			if n == "" {
				return
			}
			names = append(names, n)
			if !seen[n] {
				seen[n] = true
				allTaskNames = append(allTaskNames, n)
			}
		})
		taskNamesByPlay[playLabel] = names
	}

	if opts.PlayName != "" {
		found := false
		for _, n := range playNames {
			if n == opts.PlayName {
				found = true
				break
			}
		}
		if !found {
			problems = append(problems, Problem{
				Code:    "unknown_play_reference",
				Message: fmt.Sprintf("--play %q does not match any play in the recipe", opts.PlayName),
				Hint:    fmt.Sprintf("available plays: %s", quotedNamesOrNone(playNames)),
			})
		}
	}

	if opts.StartAtTask != "" {
		candidates := allTaskNames
		if opts.PlayName != "" {
			if names, ok := taskNamesByPlay[opts.PlayName]; ok {
				candidates = names
			}
		}
		found := false
		for _, n := range candidates {
			if n == opts.StartAtTask {
				found = true
				break
			}
		}
		if !found {
			problems = append(problems, Problem{
				Code:    "unknown_start_at_task",
				Message: fmt.Sprintf("--start-at-task %q does not match any task in the recipe", opts.StartAtTask),
				Hint:    fmt.Sprintf("available tasks: %s", quotedNamesOrNone(candidates)),
			})
		}
	}

	return problems
}

// quotedNamesOrNone formats a slice of names as a comma-separated list
// of quoted strings, or "(none)" when the slice is empty. Used to build
// the Hint text for unknown_play_reference and unknown_start_at_task
// problems.
func quotedNamesOrNone(names []string) string {
	if len(names) == 0 {
		return "(none)"
	}
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = fmt.Sprintf("%q", n)
	}
	return strings.Join(out, ", ")
}

// registerHit records the first occurrence of a `register: <name>` so
// later occurrences can quote the prior site in their diagnostic.
type registerHit struct {
	Play string
	Task string
	Line int
}

// validatePlay runs the validate-only passes over one parsed play: the
// play-level when: compile check, the strict input audit, expr predicate
// compilation, register uniqueness, loop-var placement, and per-entry body
// validation. The structural checks (play shape, allowed keys, envelope
// typing, task-type resolution) were already collected by parseRecipe and
// live on the parsedPlay / parsedTaskEntry problems.
func validatePlay(play *parsedPlay, inputs []inputWithNode, opts ValidateOptions, registerSeen map[string]registerHit) []Problem {
	problems := append([]Problem(nil), play.Problems...)

	// Compile the play-level when: predicate so a typo surfaces at validate
	// time. The play's when: sees the file-level merged context only - the
	// play's own inputs are not visible to its own when - but for parse
	// validation we only care that the source compiles.
	if whenNode := mappingValue(play.Node, "when"); whenNode != nil && whenNode.Kind == yaml.ScalarNode && whenNode.Value != "" {
		if _, err := CompilePredicate(whenNode.Value); err != nil {
			problems = append(problems, Problem{
				Code:    "expr_compile",
				Play:    play.Label,
				Line:    whenNode.Line,
				Column:  whenNode.Column,
				Message: fmt.Sprintf("play when expression compile error: %v", err),
			})
		}
	}

	if opts.Strict {
		problems = append(problems, validateStrictInputs(inputs, opts.InputOverrides, play.Label)...)
	}

	problems = append(problems, validateExprPredicates(play.TasksNode, play.Label)...)
	problems = append(problems, validateRegisterReferences(play.TasksNode, play.Label, registerSeen)...)
	problems = append(problems, validateTargetReferences(play.TasksNode, play.Label)...)

	for _, entry := range play.Entries {
		problems = append(problems, validateEntry(entry, play.Label)...)
	}

	return problems
}

// validateEntry emits one parsed entry's structural problems followed by
// its body validation (required fields plus the task's InputValidator),
// recursing through group children so nested entries surface the same
// diagnostics. Entries whose structure was rejected skip body validation:
// the structural problem is the primary finding and the body checks
// depend on a resolved task type.
func validateEntry(entry *parsedTaskEntry, playLabel string) []Problem {
	problems := append([]Problem(nil), entry.Problems...)

	if entry.IsGroup {
		for _, group := range [][]*parsedTaskEntry{entry.Block, entry.Rescue, entry.Always} {
			for _, child := range group {
				problems = append(problems, validateEntry(child, playLabel)...)
			}
		}
		return problems
	}

	if !entry.Valid || entry.TypeKey == "" {
		return problems
	}

	registered, ok := RegisteredTasks[entry.TypeKey]
	if !ok {
		return problems
	}

	problems = append(problems, validateTaskBody(registered, entry.TypeKey, entry.BodyNode, playLabel, entry.Label)...)
	return problems
}

// validateTaskBody decodes the task body into the registered struct, applies
// defaults, and reports any required:"true" field whose value is still the
// zero value of its type.
func validateTaskBody(registered Task, typeName string, body *yaml.Node, playLabel, taskLabel string) []Problem {
	var problems []Problem

	marshaled, err := yaml.Marshal(body)
	if err != nil {
		return append(problems, Problem{
			Code:    "task_body_decode",
			Play:    playLabel,
			Task:    taskLabel,
			Line:    body.Line,
			Column:  body.Column,
			Message: fmt.Sprintf("failed to re-marshal task body: %v", err),
		})
	}

	v := reflect.New(reflect.TypeOf(registered).Elem())
	if err := yaml.Unmarshal(marshaled, v.Interface()); err != nil {
		return append(problems, Problem{
			Code:    "task_body_decode",
			Play:    playLabel,
			Task:    taskLabel,
			Line:    body.Line,
			Column:  body.Column,
			Message: fmt.Sprintf("failed to decode body to %s: %v", typeName, err),
		})
	}

	defaults.SetDefaults(v.Interface())

	elem := v.Elem()
	t := elem.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.Tag.Get("required") != "true" {
			continue
		}
		// The placeholder string used for required-no-default inputs at
		// render time looks like a real value to the field zero-check, so
		// any field whose only value came from a placeholder substitution
		// is considered satisfied — the actual missing value will surface
		// at runtime as a missing CLI flag, which apply already enforces.
		if !elem.Field(i).IsZero() {
			continue
		}
		yamlName := field.Tag.Get("yaml")
		if comma := strings.Index(yamlName, ","); comma >= 0 {
			yamlName = yamlName[:comma]
		}
		if yamlName == "" {
			yamlName = strings.ToLower(field.Name)
		}
		problems = append(problems, Problem{
			Code:    "missing_required_field",
			Play:    playLabel,
			Task:    taskLabel,
			Line:    body.Line,
			Column:  body.Column,
			Message: fmt.Sprintf("missing required field %q on %s", yamlName, typeName),
		})
	}

	// Enforce the task's conditional/semantic input rules offline, but only
	// when the body is fully concrete. Skip if a required field is missing
	// (that primary problem is already reported and the conditional checks
	// depend on it) or if a required-no-default input rendered to the
	// validatePlaceholder sentinel, whose real value is unknown offline and
	// could otherwise trip a "must not be set" check into a false positive.
	if len(problems) == 0 && !strings.Contains(string(marshaled), validatePlaceholder) {
		if validator, ok := v.Interface().(InputValidator); ok {
			if err := validator.Validate(); err != nil {
				problems = append(problems, Problem{
					Code:    "invalid_task_input",
					Play:    playLabel,
					Task:    taskLabel,
					Line:    body.Line,
					Column:  body.Column,
					Message: err.Error(),
				})
			}
		}
	}

	return problems
}

// renderForValidate runs the recipe through sigil with the given context and
// returns the rendered bytes. A non-nil Problem return signals a render
// failure; the caller is responsible for surfacing it.
//
// Loop-variable references (`{{ .item ... }}`, `{{ .index ... }}`) are
// hidden from sigil before the render and restored afterwards so non-
// scalar item access (`{{ .item.app }}`) does not blow up the file-level
// pass. The loop_var_outside_loop check then walks the rendered tree to
// flag any reference in a non-loop task body.
func renderForValidate(data []byte, context map[string]interface{}) ([]byte, *Problem) {
	escaped, captured := escapeLoopVars(data)
	rendered, err := sigil.Execute(escaped, context, "tasks.yml")
	if err != nil {
		line, col := parseSigilErrorPosition(err.Error())
		return nil, &Problem{
			Code:    "template_render",
			Line:    line,
			Column:  col,
			Message: fmt.Sprintf("template render error: %v", err),
		}
	}
	out, err := io.ReadAll(&rendered)
	if err != nil {
		return nil, &Problem{
			Code:    "template_render",
			Message: fmt.Sprintf("template render read error: %v", err),
		}
	}
	return unescapeLoopVars(out, captured), nil
}

func validateStrictInputs(inputs []inputWithNode, overrides map[string]bool, label string) []Problem {
	var problems []Problem
	for _, in := range inputs {
		if !in.Required {
			continue
		}
		if in.Default != "" {
			continue
		}
		if overrides[in.Name] {
			continue
		}
		problems = append(problems, Problem{
			Code:    "input_missing",
			Play:    label,
			Line:    in.Line,
			Column:  in.Column,
			Message: fmt.Sprintf("input %q is required and has no default; pass --%s to override", in.Name, in.Name),
		})
	}
	return problems
}

// walkGroupEntries calls visit on every task entry mapping reachable
// from tasksNode, including children inside block / rescue / always
// clauses. Children inherit the parent's task label (no per-child
// breadcrumb) so diagnostics align with the on-disk source position.
func walkGroupEntries(tasksNode *yaml.Node, playLabel string, visit func(task *yaml.Node, label string)) {
	if tasksNode == nil || tasksNode.Kind != yaml.SequenceNode {
		return
	}
	for i, task := range tasksNode.Content {
		if task.Kind != yaml.MappingNode {
			continue
		}
		label := taskLabelForNode(task, i+1)
		visit(task, label)
		for _, clause := range groupClauseKeys {
			if child := mappingValue(task, clause); child != nil && child.Kind == yaml.SequenceNode {
				walkGroupEntries(child, playLabel, visit)
			}
		}
	}
}

// validateExprPredicates compiles each scalar `when:` / `changed_when:` /
// `failed_when:` value and the scalar form of `loop:` on every task entry.
// Compile errors are reported with the source line/column from the
// rendered yaml node so editors can jump straight to the problem.
//
// Loop-list literals (sequence form) are skipped here - they are not expr
// programs. `register` / `changed_when` / `failed_when` are reserved at
// the loader level for #210 but the syntax check is wired now so that
// issue does not have to revisit the validator.
func validateExprPredicates(tasksNode *yaml.Node, label string) []Problem {
	if tasksNode == nil || tasksNode.Kind != yaml.SequenceNode {
		return nil
	}
	var problems []Problem
	walkGroupEntries(tasksNode, label, func(task *yaml.Node, taskLabel string) {
		problems = append(problems, compileExprNode(mappingValue(task, "when"), "when", label, taskLabel)...)
		problems = append(problems, compileExprNode(mappingValue(task, "changed_when"), "changed_when", label, taskLabel)...)
		problems = append(problems, compileExprNode(mappingValue(task, "failed_when"), "failed_when", label, taskLabel)...)
		if loop := mappingValue(task, "loop"); loop != nil && loop.Kind == yaml.ScalarNode {
			problems = append(problems, compileExprNode(loop, "loop", label, taskLabel)...)
		}
	})
	return problems
}

// taskLabelForNode mirrors the labelling validateTaskEntry uses so plan /
// expr / target diagnostics align in human output.
func taskLabelForNode(task *yaml.Node, idx int) string {
	name := scalarChild(task, "name")
	if name != "" {
		return fmt.Sprintf("task #%d %q", idx, name)
	}
	return fmt.Sprintf("task #%d", idx)
}

// compileExprNode runs expr.Compile on the scalar value of node and
// returns a Problem when the source does not parse. nil node or
// non-scalar / empty value is a no-op.
func compileExprNode(node *yaml.Node, key, playLabel, taskLabel string) []Problem {
	if node == nil || node.Kind != yaml.ScalarNode || node.Value == "" {
		return nil
	}
	if _, err := CompilePredicate(node.Value); err != nil {
		return []Problem{{
			Code:    "expr_compile",
			Play:    playLabel,
			Task:    taskLabel,
			Line:    node.Line,
			Column:  node.Column,
			Message: fmt.Sprintf("%s expression compile error: %v", key, err),
		}}
	}
	return nil
}

// validateRegisterReferences enforces uniqueness of `register: <name>`
// across the whole recipe. registerSeen is the recipe-wide map of names
// that have already been claimed; each call updates it with the names
// declared in this play. Re-using a name (within the same play, or
// across plays) is reported as `register_duplicate`.
//
// Cross-reference checking (every `.registered.foo` reference must have
// a prior `register: foo`) is intentionally out of scope: predicates
// reference dotted paths like `.registered.foo.Results[0].Stderr`,
// which would require a tiny expr AST walker per envelope, and the
// AllowUndefinedVariables compile mode means an unknown reference
// degrades to nil at runtime rather than blowing up.
func validateRegisterReferences(tasksNode *yaml.Node, playLabel string, registerSeen map[string]registerHit) []Problem {
	if tasksNode == nil || tasksNode.Kind != yaml.SequenceNode {
		return nil
	}
	var problems []Problem
	walkGroupEntries(tasksNode, playLabel, func(task *yaml.Node, taskLabel string) {
		regNode := mappingValue(task, "register")
		if regNode == nil || regNode.Kind != yaml.ScalarNode || regNode.Value == "" {
			return
		}
		name := regNode.Value
		if prior, ok := registerSeen[name]; ok {
			hint := fmt.Sprintf("first declared in %s at line %d", prior.Play, prior.Line)
			if prior.Task != "" {
				hint = fmt.Sprintf("first declared in %s on %s (line %d)", prior.Play, prior.Task, prior.Line)
			}
			problems = append(problems, Problem{
				Code:    "register_duplicate",
				Play:    playLabel,
				Task:    taskLabel,
				Line:    regNode.Line,
				Column:  regNode.Column,
				Message: fmt.Sprintf("register name %q is already declared", name),
				Hint:    hint,
			})
			return
		}
		registerSeen[name] = registerHit{
			Play: playLabel,
			Task: taskLabel,
			Line: regNode.Line,
		}
	})
	return problems
}

// validateTargetReferences guards `.item` / `.index` references. They
// are loop-iteration variables and are only meaningful inside a task
// entry that carries a `loop:` key, or inside a child of a group entry
// (#211) whose ancestor carries a `loop:`. Any reference outside such a
// scope is reported here.
func validateTargetReferences(tasksNode *yaml.Node, label string) []Problem {
	if tasksNode == nil || tasksNode.Kind != yaml.SequenceNode {
		return nil
	}
	var problems []Problem
	for i, task := range tasksNode.Content {
		if task.Kind != yaml.MappingNode {
			continue
		}
		taskLabel := taskLabelForNode(task, i+1)
		problems = append(problems, walkLoopVarScope(task, label, taskLabel, false)...)
	}
	return problems
}

// walkLoopVarScope walks one task entry, tracking whether an ancestor
// has `loop:` so descendants inside a group's block / rescue / always
// see the parent loop's `.item` / `.index`. When loopActive is false at
// a given task, scanForLoopVars flags any `.item` / `.index` reference
// in the task's envelope and body. When loopActive is true (or the
// current task carries its own `loop:`), the scan is skipped for that
// entry's own envelope and body, and the recursion into nested group
// children continues with loopActive set.
func walkLoopVarScope(task *yaml.Node, playLabel, taskLabel string, loopActive bool) []Problem {
	if task == nil || task.Kind != yaml.MappingNode {
		return nil
	}
	var problems []Problem
	thisLoop := mappingValue(task, "loop") != nil
	scopeActive := loopActive || thisLoop

	if !scopeActive {
		problems = append(problems, scanForLoopVars(task, playLabel, taskLabel, true)...)
	}

	for _, clause := range groupClauseKeys {
		clauseNode := mappingValue(task, clause)
		if clauseNode == nil || clauseNode.Kind != yaml.SequenceNode {
			continue
		}
		for j, child := range clauseNode.Content {
			if child.Kind != yaml.MappingNode {
				continue
			}
			childLabel := taskLabelForNode(child, j+1)
			problems = append(problems, walkLoopVarScope(child, playLabel, childLabel, scopeActive)...)
		}
	}
	return problems
}

// scanForLoopVars walks every scalar value reachable from node and
// reports a Problem for each `{{ .item }}` / `{{ .index }}` reference.
// Scalars are matched against the placeholder substrings the loader
// injects at file-level render time. When skipGroupChildren is true,
// recursion stops at block / rescue / always sequence values so the
// caller can scan the envelope and leaf body without re-scanning child
// task entries (which walkLoopVarScope handles separately so per-child
// loop scope is honored).
func scanForLoopVars(node *yaml.Node, playLabel, taskLabel string, skipGroupChildren bool) []Problem {
	if node == nil {
		return nil
	}
	var problems []Problem
	switch node.Kind {
	case yaml.ScalarNode:
		if containsLoopVar(node.Value) {
			problems = append(problems, Problem{
				Code:    "loop_var_outside_loop",
				Play:    playLabel,
				Task:    taskLabel,
				Line:    node.Line,
				Column:  node.Column,
				Message: ".item / .index are only available inside a loop body",
				Hint:    "wrap the task with `loop:` or remove the reference",
			})
		}
	case yaml.MappingNode:
		for i := 0; i < len(node.Content); i += 2 {
			keyNode := node.Content[i]
			valueNode := node.Content[i+1]
			if skipGroupChildren {
				if keyNode.Value == "block" || keyNode.Value == "rescue" || keyNode.Value == "always" {
					continue
				}
			}
			problems = append(problems, scanForLoopVars(valueNode, playLabel, taskLabel, skipGroupChildren)...)
		}
	case yaml.SequenceNode, yaml.DocumentNode:
		for _, child := range node.Content {
			problems = append(problems, scanForLoopVars(child, playLabel, taskLabel, skipGroupChildren)...)
		}
	}
	return problems
}

// containsLoopVar reports whether s contains a `{{ .item ... }}` or
// `{{ .index ... }}` reference, including sub-field (`{{ .item.name }}`)
// and pipelined (`{{ .item | f }}`) forms, while leaving real inputs that
// merely start with those letters (`{{ .items }}`, `{{ .item_name }}`)
// alone. It shares loopVarSentinelPattern with the loader so both agree
// on what counts as a loop-variable reference.
func containsLoopVar(s string) bool {
	if s == "" {
		return false
	}
	return loopVarSentinelPattern.MatchString(s)
}

// inputWithNode is the validate-time projection of an Input that also carries
// the source position of the input's mapping node so strict-mode problems can
// anchor at the right line.
type inputWithNode struct {
	Name     string
	Default  string
	Required bool
	Type     string
	Line     int
	Column   int
}

// extractPlayInputs walks the raw recipe and returns the input definitions
// for each play (slice index = play index). Inputs do not contain templates,
// so the source positions on inputWithNode are reliable.
func extractPlayInputs(recipe *yaml.Node) [][]inputWithNode {
	plays := make([][]inputWithNode, 0, len(recipe.Content))
	for _, play := range recipe.Content {
		if play.Kind != yaml.MappingNode {
			plays = append(plays, nil)
			continue
		}
		inputsNode := mappingValue(play, "inputs")
		if inputsNode == nil || inputsNode.Kind != yaml.SequenceNode {
			plays = append(plays, nil)
			continue
		}
		var inputs []inputWithNode
		for _, node := range inputsNode.Content {
			if node.Kind != yaml.MappingNode {
				continue
			}
			in := inputWithNode{Line: node.Line, Column: node.Column}
			in.Name = scalarChild(node, "name")
			in.Default = scalarChild(node, "default")
			in.Type = scalarChild(node, "type")
			if reqStr := scalarChild(node, "required"); reqStr != "" {
				if v, err := strconv.ParseBool(reqStr); err == nil {
					in.Required = v
				}
			}
			inputs = append(inputs, in)
		}
		plays = append(plays, inputs)
	}
	return plays
}

// buildSigilContext assembles the variable map sigil renders against. Inputs
// with a non-empty Default contribute their default value; inputs without a
// default contribute validatePlaceholder so the render does not error on
// missing keys. Names collide cleanly across plays since sigil receives a
// single flat namespace.
//
// `.item` and `.index` references in loop-body templates are hidden from
// the file-level render via escapeLoopVars / unescapeLoopVars, so they
// do not need a placeholder entry here.
func buildSigilContext(plays [][]inputWithNode) map[string]interface{} {
	context := map[string]interface{}{}
	for _, inputs := range plays {
		for _, in := range inputs {
			if in.Name == "" {
				continue
			}
			// A reserved input name (e.g. no-color) is rejected as
			// reserved_input_name by the parser; keep it out of the sigil
			// context so a dash in the name cannot break the template
			// render before that clearer diagnostic is reported (#302).
			if ReservedInputNames[in.Name] {
				continue
			}
			if in.Default != "" {
				context[in.Name] = in.Default
			} else if _, ok := context[in.Name]; !ok {
				context[in.Name] = validatePlaceholder
			}
		}
	}
	return context
}

// inputNameRe matches an input name usable with the documented `{{ .name }}`
// syntax. Go text/template lexes a field as a letter or underscore followed by
// letters, digits, or underscores; any other character (notably a hyphen)
// makes `{{ .name }}` fail at lex time with "bad character".
var inputNameRe = regexp.MustCompile(`^[\p{L}_][\p{L}\p{N}_]*$`)

// checkInputNames flags any declared input name that cannot be referenced with
// the documented `{{ .name }}` syntax, so a hyphenated name surfaces as a clear
// invalid_input_name diagnostic instead of a cryptic template render error. It
// parses tolerantly: on any normalize/parse failure it returns nil so the
// caller's existing parse-error path reports the underlying issue. Reserved
// names are skipped here so they keep surfacing as the more specific
// reserved_input_name diagnostic. Shared by Validate and the loader
// (GetPlaysWithFormat), both of which render before any name check would
// otherwise run.
func checkInputNames(data []byte, format string) []Problem {
	normalized, normProblems := normalizeRecipeBytes(data, format)
	if len(normProblems) > 0 {
		return nil
	}
	var root yaml.Node
	if err := yaml.Unmarshal(normalized, &root); err != nil {
		return nil
	}
	doc := documentBody(&root)
	if doc == nil || doc.Kind != yaml.SequenceNode {
		return nil
	}
	var problems []Problem
	for _, inputs := range extractPlayInputs(doc) {
		for _, in := range inputs {
			if in.Name == "" || ReservedInputNames[in.Name] {
				continue
			}
			if inputNameRe.MatchString(in.Name) {
				continue
			}
			problems = append(problems, Problem{
				Code:    "invalid_input_name",
				Line:    in.Line,
				Column:  in.Column,
				Message: fmt.Sprintf("input name %q is not a valid template variable name", in.Name),
				Hint:    `use only letters, digits, and underscores (for example "my_app")`,
			})
		}
	}
	return problems
}

// diagnoseUnsafeInputValue attributes a post-render YAML parse failure to an
// input whose resolved value breaks its surrounding scalar once substituted as
// raw text (#371). It is called only on the parse-failure path and returns nil
// when the failure is not caused by an input value, so the caller keeps its
// original yaml_parse diagnostic.
//
// The culprit is isolated by re-rendering: first with every input replaced by a
// known-safe placeholder (which must then parse), then with one input at a time
// restored to its real value; the inputs that reintroduce the failure are the
// culprits. data is the raw recipe bytes, format its surface syntax, and
// context maps input name -> resolved value.
//
// The message never echoes an input's value: an input may be marked sensitive,
// and the value is not needed to act on the problem.
func diagnoseUnsafeInputValue(data []byte, format string, context map[string]interface{}) *Problem {
	if len(context) == 0 {
		return nil
	}

	parses := func(ctx map[string]interface{}) bool {
		rendered, err := renderRecipeBytes(data, ctx)
		if err != nil {
			return false
		}
		normalized, probs := normalizeRecipeBytes(rendered, format)
		if len(probs) > 0 {
			return false
		}
		var node yaml.Node
		return yaml.Unmarshal(normalized, &node) == nil
	}

	safeContext := func(real string) map[string]interface{} {
		ctx := make(map[string]interface{}, len(context))
		for k := range context {
			ctx[k] = validatePlaceholder
		}
		if real != "" {
			ctx[real] = context[real]
		}
		return ctx
	}

	// The real context must fail and the all-safe context must parse, or the
	// failure is structural rather than a value-injection problem.
	if parses(context) || !parses(safeContext("")) {
		return nil
	}

	var culprits []string
	for name := range context {
		if !parses(safeContext(name)) {
			culprits = append(culprits, name)
		}
	}
	if len(culprits) == 0 {
		// The values break the render only in combination; name them all so
		// the operator still gets an actionable, non-cryptic message.
		for name := range context {
			culprits = append(culprits, name)
		}
	}
	sort.Strings(culprits)

	line, column := unsafeInputPosition(data, format, culprits[0])
	return &Problem{
		Code:    "unsafe_input_value",
		Line:    line,
		Column:  column,
		Message: unsafeInputMessage(culprits),
		Hint:    unsafeInputHint(culprits[0]),
	}
}

// unsafeInputPosition returns the source position of an input's declaration so
// the diagnostic anchors at the right line. Input declarations are
// template-free, so the raw recipe parses even when its rendered form does not.
// Returns 0, 0 when the position cannot be derived.
func unsafeInputPosition(data []byte, format, name string) (int, int) {
	normalized, probs := normalizeRecipeBytes(data, format)
	if len(probs) > 0 {
		return 0, 0
	}
	var root yaml.Node
	if err := yaml.Unmarshal(normalized, &root); err != nil {
		return 0, 0
	}
	doc := documentBody(&root)
	if doc == nil || doc.Kind != yaml.SequenceNode {
		return 0, 0
	}
	for _, inputs := range extractPlayInputs(doc) {
		for _, in := range inputs {
			if in.Name == name {
				return in.Line, in.Column
			}
		}
	}
	return 0, 0
}

// unsafeInputMessage renders the unsafe_input_value message for one or more
// culprit input names, without echoing any value.
func unsafeInputMessage(names []string) string {
	if len(names) == 1 {
		return fmt.Sprintf("input %q has a value that breaks the surrounding scalar after template substitution", names[0])
	}
	quoted := make([]string, len(names))
	for i, n := range names {
		quoted[i] = fmt.Sprintf("%q", n)
	}
	return fmt.Sprintf("inputs %s have values that break the surrounding scalar after template substitution", strings.Join(quoted, ", "))
}

// unsafeInputHint points at the safe interpolation patterns, using name as the
// example.
func unsafeInputHint(name string) string {
	return fmt.Sprintf("escape it inside the quotes with `\"{{ .%s | dq }}\"`, or use a quote style that does not clash with the value (a single-quoted `'{{ .%s }}'` tolerates a double quote)", name, name)
}

// documentBody returns the inner content node when root is a DocumentNode,
// or root itself otherwise. nil when the document is empty.
func documentBody(root *yaml.Node) *yaml.Node {
	if root == nil || root.Kind == 0 || len(root.Content) == 0 {
		return nil
	}
	if root.Kind == yaml.DocumentNode {
		return root.Content[0]
	}
	return root
}

// mappingValue returns the value node for the given key in a MappingNode, or
// nil if absent. Mapping content is laid out as [k1, v1, k2, v2, ...].
func mappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// scalarChild returns the scalar string at node[key], or "" if the key is
// absent or the value is not a scalar.
func scalarChild(node *yaml.Node, key string) string {
	v := mappingValue(node, key)
	if v == nil || v.Kind != yaml.ScalarNode {
		return ""
	}
	return v.Value
}

// levenshtein returns the edit distance between a and b. Small strings only;
// a 2D allocation is fine.
func levenshtein(a, b string) int {
	if a == b {
		return 0
	}
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

// yamlPositionRe matches the "line N: ..." style errors yaml.v3 emits. The
// optional "column M" group is only present in some templates so we capture
// what we can and leave 0 otherwise.
var yamlPositionRe = regexp.MustCompile(`line (\d+)(?::|, column (\d+))`)

func parseYAMLErrorPosition(msg string) (int, int) {
	m := yamlPositionRe.FindStringSubmatch(msg)
	if m == nil {
		return 0, 0
	}
	line, _ := strconv.Atoi(m[1])
	col := 0
	if m[2] != "" {
		col, _ = strconv.Atoi(m[2])
	}
	return line, col
}

// sigilPositionRe matches the "template: name:LINE:COL" prefix emitted by
// text/template (which sigil delegates to).
var sigilPositionRe = regexp.MustCompile(`template:\s*[^:]+:(\d+)(?::(\d+))?`)

func parseSigilErrorPosition(msg string) (int, int) {
	m := sigilPositionRe.FindStringSubmatch(msg)
	if m == nil {
		return 0, 0
	}
	line, _ := strconv.Atoi(m[1])
	col := 0
	if len(m) > 2 && m[2] != "" {
		col, _ = strconv.Atoi(m[2])
	}
	return line, col
}
