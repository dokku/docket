package tasks

import (
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	sigil "github.com/gliderlabs/sigil"
	defaults "github.com/mcuadros/go-defaults"
	yaml "gopkg.in/yaml.v3"
)

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
type ValidateOptions struct {
	Strict         bool
	InputOverrides map[string]bool
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
// than getting silently swallowed by an envelope allowlist.
var reservedEnvelopeKeys = map[string]string{
	"block":  "#211",
	"rescue": "#211",
	"always": "#211",
}

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
}

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

	var renderedRoot yaml.Node
	if err := yaml.Unmarshal(rendered, &renderedRoot); err != nil {
		line, col := parseYAMLErrorPosition(err.Error())
		return []Problem{{
			Code:    "yaml_parse",
			Line:    line,
			Column:  col,
			Message: fmt.Sprintf("rendered recipe parse error: %v", err),
		}}
	}

	doc := documentBody(&renderedRoot)
	if doc == nil || doc.Kind != yaml.SequenceNode {
		return []Problem{{
			Code:    "recipe_shape",
			Message: "rendered recipe is not a yaml list of plays",
		}}
	}

	var problems []Problem
	// registerSeen tracks register names across the whole recipe so a
	// duplicate `register: foo` in two different plays surfaces as a
	// single register_duplicate problem. The registered map is run-wide
	// at apply / plan time, so the validator's uniqueness check is
	// recipe-wide too.
	registerSeen := map[string]registerHit{}
	for i, play := range doc.Content {
		label := fmt.Sprintf("play #%d", i+1)
		var inputs []inputWithNode
		if i < len(playsInputs) {
			inputs = playsInputs[i]
		}
		problems = append(problems, validatePlay(play, label, inputs, opts, registerSeen)...)
	}

	return problems
}

// registerHit records the first occurrence of a `register: <name>` so
// later occurrences can quote the prior site in their diagnostic.
type registerHit struct {
	Play string
	Task string
	Line int
}

// validatePlay walks one play (one MappingNode within the recipe sequence).
func validatePlay(play *yaml.Node, label string, inputs []inputWithNode, opts ValidateOptions, registerSeen map[string]registerHit) []Problem {
	var problems []Problem

	if play.Kind != yaml.MappingNode {
		return append(problems, Problem{
			Code:    "recipe_shape",
			Play:    label,
			Line:    play.Line,
			Column:  play.Column,
			Message: "play must be a yaml mapping with inputs and/or tasks",
		})
	}

	tasksNode := mappingValue(play, "tasks")

	for i := 0; i < len(play.Content); i += 2 {
		key := play.Content[i].Value
		if !allowedPlayKeys[key] {
			problems = append(problems, Problem{
				Code:    "recipe_shape",
				Play:    label,
				Line:    play.Content[i].Line,
				Column:  play.Content[i].Column,
				Message: fmt.Sprintf("unexpected play key %q (expected: %s)", key, allowedPlayKeysList),
			})
		}
	}

	// Compile the play-level when: predicate so a typo surfaces at validate
	// time. The play's when: sees the file-level merged context only - the
	// play's own inputs are not visible to its own when - but for parse
	// validation we only care that the source compiles.
	if whenNode := mappingValue(play, "when"); whenNode != nil && whenNode.Kind == yaml.ScalarNode && whenNode.Value != "" {
		if _, err := CompilePredicate(whenNode.Value); err != nil {
			problems = append(problems, Problem{
				Code:    "expr_compile",
				Play:    label,
				Line:    whenNode.Line,
				Column:  whenNode.Column,
				Message: fmt.Sprintf("play when expression compile error: %v", err),
			})
		}
	}

	if opts.Strict {
		problems = append(problems, validateStrictInputs(inputs, opts.InputOverrides, label)...)
	}

	// Stub checks: present so future PRs (#205/#208/#211/#212) can wire
	// real bodies without touching the call site.
	problems = append(problems, validateBlockStructure(tasksNode, label)...)
	problems = append(problems, validateExprPredicates(tasksNode, label)...)
	problems = append(problems, validateRegisterReferences(tasksNode, label, registerSeen)...)
	problems = append(problems, validateTargetReferences(tasksNode, label)...)

	if tasksNode == nil {
		return problems
	}
	if tasksNode.Kind != yaml.SequenceNode {
		return append(problems, Problem{
			Code:    "recipe_shape",
			Play:    label,
			Line:    tasksNode.Line,
			Column:  tasksNode.Column,
			Message: "tasks must be a yaml list",
		})
	}

	for i, task := range tasksNode.Content {
		problems = append(problems, validateTaskEntry(task, label, i+1)...)
	}

	return problems
}

// validateTaskEntry covers checks 3-5: envelope shape, registered task type,
// and required-field decode.
func validateTaskEntry(task *yaml.Node, playLabel string, idx int) []Problem {
	var problems []Problem
	taskLabel := fmt.Sprintf("task #%d", idx)

	if task.Kind != yaml.MappingNode {
		return []Problem{{
			Code:    "task_entry_shape",
			Play:    playLabel,
			Task:    taskLabel,
			Line:    task.Line,
			Column:  task.Column,
			Message: "task entry must be a yaml mapping",
		}}
	}

	var (
		taskTypeKey  string
		taskTypeNode *yaml.Node
		taskBodyNode *yaml.Node
		taskTypeKeys []string
		nameValue    string
	)

	for i := 0; i < len(task.Content); i += 2 {
		keyNode := task.Content[i]
		valueNode := task.Content[i+1]
		key := keyNode.Value

		if key == "name" {
			if valueNode.Kind == yaml.ScalarNode {
				nameValue = valueNode.Value
			}
			continue
		}

		if activeEnvelopeKeys[key] {
			continue
		}

		if dependentIssue, reserved := reservedEnvelopeKeys[key]; reserved {
			problems = append(problems, Problem{
				Code:    "envelope_key_unsupported",
				Play:    playLabel,
				Task:    taskLabel,
				Line:    keyNode.Line,
				Column:  keyNode.Column,
				Message: fmt.Sprintf("envelope key %q is reserved but not yet supported", key),
				Hint:    fmt.Sprintf("activates with %s", dependentIssue),
			})
			continue
		}

		taskTypeKeys = append(taskTypeKeys, key)
		taskTypeKey = key
		taskTypeNode = keyNode
		taskBodyNode = valueNode
	}

	if nameValue != "" {
		taskLabel = fmt.Sprintf("task #%d %q", idx, nameValue)
	}

	if len(taskTypeKeys) == 0 {
		return append(problems, Problem{
			Code:    "task_entry_shape",
			Play:    playLabel,
			Task:    taskLabel,
			Line:    task.Line,
			Column:  task.Column,
			Message: "task entry must contain exactly one task-type key",
		})
	}
	if len(taskTypeKeys) > 1 {
		return append(problems, Problem{
			Code:    "task_entry_shape",
			Play:    playLabel,
			Task:    taskLabel,
			Line:    task.Line,
			Column:  task.Column,
			Message: fmt.Sprintf("task entry has %d task-type keys (%s); exactly one is allowed", len(taskTypeKeys), strings.Join(taskTypeKeys, ", ")),
		})
	}

	registered, ok := RegisteredTasks[taskTypeKey]
	if !ok {
		hint := ""
		if suggestion := nearestTaskName(taskTypeKey); suggestion != "" {
			hint = fmt.Sprintf("did you mean %q?", suggestion)
		}
		return append(problems, Problem{
			Code:    "unknown_task_type",
			Play:    playLabel,
			Task:    taskLabel,
			Line:    taskTypeNode.Line,
			Column:  taskTypeNode.Column,
			Message: fmt.Sprintf("unknown task type %q", taskTypeKey),
			Hint:    hint,
		})
	}

	problems = append(problems, validateTaskBody(registered, taskTypeKey, taskBodyNode, playLabel, taskLabel)...)
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

// validateBlockStructure is a stub. #211 will populate it with checks that
// block:/rescue:/always: children are valid task entries.
func validateBlockStructure(_ *yaml.Node, _ string) []Problem {
	// TODO(#211): walk block/rescue/always children once they are activated.
	return nil
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
	for i, task := range tasksNode.Content {
		if task.Kind != yaml.MappingNode {
			continue
		}
		taskLabel := taskLabelForNode(task, i+1)
		problems = append(problems, compileExprNode(mappingValue(task, "when"), "when", label, taskLabel)...)
		problems = append(problems, compileExprNode(mappingValue(task, "changed_when"), "changed_when", label, taskLabel)...)
		problems = append(problems, compileExprNode(mappingValue(task, "failed_when"), "failed_when", label, taskLabel)...)
		if loop := mappingValue(task, "loop"); loop != nil && loop.Kind == yaml.ScalarNode {
			problems = append(problems, compileExprNode(loop, "loop", label, taskLabel)...)
		}
	}
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
	for i, task := range tasksNode.Content {
		if task.Kind != yaml.MappingNode {
			continue
		}
		regNode := mappingValue(task, "register")
		if regNode == nil || regNode.Kind != yaml.ScalarNode || regNode.Value == "" {
			continue
		}
		name := regNode.Value
		taskLabel := taskLabelForNode(task, i+1)
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
			continue
		}
		registerSeen[name] = registerHit{
			Play: playLabel,
			Task: taskLabel,
			Line: regNode.Line,
		}
	}
	return problems
}

// validateTargetReferences guards `.item` / `.index` references. They
// are loop-iteration variables and are only meaningful inside a task
// entry that carries a `loop:` key. Any non-loop entry whose body still
// references the placeholder tokens is reported here. #208 / #212 will
// extend this stub for --start-at-task and --play target validation.
func validateTargetReferences(tasksNode *yaml.Node, label string) []Problem {
	if tasksNode == nil || tasksNode.Kind != yaml.SequenceNode {
		return nil
	}
	var problems []Problem
	for i, task := range tasksNode.Content {
		if task.Kind != yaml.MappingNode {
			continue
		}
		if mappingValue(task, "loop") != nil {
			continue
		}
		taskLabel := taskLabelForNode(task, i+1)
		problems = append(problems, scanForLoopVars(task, label, taskLabel)...)
	}
	return problems
}

// scanForLoopVars walks every scalar value reachable from node and
// reports a Problem for each `{{ .item }}` / `{{ .index }}` reference.
// Scalars are matched against the placeholder substrings the loader
// injects at file-level render time.
func scanForLoopVars(node *yaml.Node, playLabel, taskLabel string) []Problem {
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
	case yaml.SequenceNode, yaml.MappingNode, yaml.DocumentNode:
		for _, child := range node.Content {
			problems = append(problems, scanForLoopVars(child, playLabel, taskLabel)...)
		}
	}
	return problems
}

// containsLoopVar reports whether s contains a `{{ .item }}` or
// `{{ .index }}` reference (with or without surrounding whitespace).
func containsLoopVar(s string) bool {
	if s == "" {
		return false
	}
	if strings.Contains(s, "{{ .item }}") || strings.Contains(s, "{{.item}}") {
		return true
	}
	if strings.Contains(s, "{{ .index }}") || strings.Contains(s, "{{.index}}") {
		return true
	}
	return false
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
			if in.Default != "" {
				context[in.Name] = in.Default
			} else if _, ok := context[in.Name]; !ok {
				context[in.Name] = validatePlaceholder
			}
		}
	}
	return context
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

// nearestTaskName returns the registered task name with the lowest Levenshtein
// distance to candidate, but only if that distance is at most 2. Returning ""
// means "no useful suggestion".
func nearestTaskName(candidate string) string {
	best := ""
	bestDist := 3
	for name := range RegisteredTasks {
		d := levenshtein(candidate, name)
		if d < bestDist {
			bestDist = d
			best = name
		}
	}
	if bestDist <= 2 {
		return best
	}
	return ""
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
