package tasks

import (
	"strings"
	"testing"

	_ "github.com/gliderlabs/sigil/builtin"
)

// findProblem returns the first problem whose Code matches code, or nil.
func findProblem(problems []Problem, code string) *Problem {
	for i := range problems {
		if problems[i].Code == code {
			return &problems[i]
		}
	}
	return nil
}

// countProblems returns the number of problems with the given code.
func countProblems(problems []Problem, code string) int {
	n := 0
	for _, p := range problems {
		if p.Code == code {
			n++
		}
	}
	return n
}

func TestValidateValidRecipe(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: create app
      dokku_app:
        app: my-app
`)
	problems := Validate(data, ValidateOptions{})
	if len(problems) != 0 {
		t.Fatalf("expected no problems, got: %+v", problems)
	}
}

func TestValidateYAMLParseError(t *testing.T) {
	// `app: [unclosed` is a non-template parse error so sigil renders fine
	// and the yaml parser is the one that complains.
	data := []byte(`---
- tasks:
    - dokku_app:
        app: [unclosed
`)
	problems := Validate(data, ValidateOptions{})
	if p := findProblem(problems, "yaml_parse"); p == nil {
		t.Fatalf("expected yaml_parse problem, got: %+v", problems)
	}
}

func TestValidateRecipeShapeBareScalar(t *testing.T) {
	data := []byte("foo\n")
	problems := Validate(data, ValidateOptions{})
	if p := findProblem(problems, "recipe_shape"); p == nil {
		t.Fatalf("expected recipe_shape problem, got: %+v", problems)
	}
}

func TestValidateTaskEntryShapeNoTaskType(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: nothing-here
`)
	problems := Validate(data, ValidateOptions{})
	if p := findProblem(problems, "task_entry_shape"); p == nil {
		t.Fatalf("expected task_entry_shape problem, got: %+v", problems)
	}
}

func TestValidateTaskEntryShapeMultipleTaskTypes(t *testing.T) {
	data := []byte(`---
- tasks:
    - dokku_app:
        app: a
      dokku_config:
        app: a
`)
	problems := Validate(data, ValidateOptions{})
	p := findProblem(problems, "task_entry_shape")
	if p == nil {
		t.Fatalf("expected task_entry_shape problem, got: %+v", problems)
	}
	if !strings.Contains(p.Message, "exactly one is allowed") {
		t.Errorf("expected message to mention exactly-one constraint, got: %q", p.Message)
	}
}

func TestValidateUnknownTaskTypeWithSuggestion(t *testing.T) {
	data := []byte(`---
- tasks:
    - dokku_appp:
        app: my-app
`)
	problems := Validate(data, ValidateOptions{})
	p := findProblem(problems, "unknown_task_type")
	if p == nil {
		t.Fatalf("expected unknown_task_type problem, got: %+v", problems)
	}
	if !strings.Contains(p.Hint, "dokku_app") {
		t.Errorf("expected hint to suggest dokku_app, got: %q", p.Hint)
	}
	if p.Line == 0 {
		t.Errorf("expected non-zero line, got: %d", p.Line)
	}
}

func TestValidateUnknownTaskTypeNoSuggestion(t *testing.T) {
	data := []byte(`---
- tasks:
    - completely_unrelated_task:
        foo: bar
`)
	problems := Validate(data, ValidateOptions{})
	p := findProblem(problems, "unknown_task_type")
	if p == nil {
		t.Fatalf("expected unknown_task_type problem, got: %+v", problems)
	}
	if p.Hint != "" {
		t.Errorf("expected no suggestion, got hint: %q", p.Hint)
	}
}

func TestValidateMissingRequiredField(t *testing.T) {
	data := []byte(`---
- tasks:
    - dokku_config:
        restart: false
`)
	problems := Validate(data, ValidateOptions{})
	p := findProblem(problems, "missing_required_field")
	if p == nil {
		t.Fatalf("expected missing_required_field problem, got: %+v", problems)
	}
	if !strings.Contains(p.Message, `"app"`) {
		t.Errorf("expected message to mention app, got: %q", p.Message)
	}
}

func TestValidateRequiredFieldPresent(t *testing.T) {
	// The body provides app; defaults fill State; nothing should flag.
	data := []byte(`---
- tasks:
    - dokku_app:
        app: my-app
`)
	problems := Validate(data, ValidateOptions{})
	if n := countProblems(problems, "missing_required_field"); n != 0 {
		t.Errorf("expected no missing_required_field problems, got %d: %+v", n, problems)
	}
}

func TestValidateSigilRenderBrokenTemplate(t *testing.T) {
	data := []byte(`---
- tasks:
    - dokku_app:
        app: {{ .broken
`)
	problems := Validate(data, ValidateOptions{})
	if p := findProblem(problems, "template_render"); p == nil {
		t.Fatalf("expected template_render problem, got: %+v", problems)
	}
}

func TestValidateSigilRenderWithDefault(t *testing.T) {
	// `default ""` makes missing keys safe; render must succeed.
	data := []byte(`---
- inputs:
    - name: app
      default: foo
  tasks:
    - dokku_app:
        app: {{ .app | default "" }}
`)
	problems := Validate(data, ValidateOptions{})
	if len(problems) != 0 {
		t.Fatalf("expected no problems, got: %+v", problems)
	}
}

func TestValidateInputsStrictMissing(t *testing.T) {
	data := []byte(`---
- inputs:
    - name: app
      required: true
  tasks:
    - dokku_app:
        app: {{ .app | default "" }}
`)
	// Without strict: no problems.
	if problems := Validate(data, ValidateOptions{}); len(problems) != 0 {
		t.Fatalf("expected no problems without strict, got: %+v", problems)
	}
	// With strict: input_missing.
	problems := Validate(data, ValidateOptions{Strict: true})
	if p := findProblem(problems, "input_missing"); p == nil {
		t.Fatalf("expected input_missing problem with strict, got: %+v", problems)
	}
}

func TestValidateInputsStrictWithDefault(t *testing.T) {
	data := []byte(`---
- inputs:
    - name: app
      required: true
      default: my-app
  tasks:
    - dokku_app:
        app: {{ .app | default "" }}
`)
	problems := Validate(data, ValidateOptions{Strict: true})
	if n := countProblems(problems, "input_missing"); n != 0 {
		t.Errorf("expected no input_missing with default, got %d: %+v", n, problems)
	}
}

func TestValidateInputsStrictWithOverride(t *testing.T) {
	data := []byte(`---
- inputs:
    - name: app
      required: true
  tasks:
    - dokku_app:
        app: {{ .app | default "" }}
`)
	problems := Validate(data, ValidateOptions{
		Strict:         true,
		InputOverrides: map[string]bool{"app": true},
	})
	if n := countProblems(problems, "input_missing"); n != 0 {
		t.Errorf("expected no input_missing with override, got %d: %+v", n, problems)
	}
}

func TestValidateMultipleProblemsCollected(t *testing.T) {
	data := []byte(`---
- tasks:
    - dokku_appp:
        app: my-app
    - dokku_config:
        restart: false
`)
	problems := Validate(data, ValidateOptions{})
	if countProblems(problems, "unknown_task_type") != 1 {
		t.Errorf("expected exactly one unknown_task_type, got: %+v", problems)
	}
	if countProblems(problems, "missing_required_field") < 1 {
		t.Errorf("expected at least one missing_required_field, got: %+v", problems)
	}
}

func TestValidateNearestTaskName(t *testing.T) {
	tests := []struct {
		input  string
		expect string
	}{
		{"dokku_appp", "dokku_app"},
		{"dokku_app", "dokku_app"}, // exact match returns same name
		{"completely_unrelated", ""},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := nearestTaskName(tt.input)
			if got != tt.expect {
				t.Errorf("nearestTaskName(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestValidateLevenshtein(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"abc", "abd", 1},
		{"abc", "abcd", 1},
		{"abc", "", 3},
		{"", "abc", 3},
		{"kitten", "sitting", 3},
	}
	for _, tt := range tests {
		got := levenshtein(tt.a, tt.b)
		if got != tt.want {
			t.Errorf("levenshtein(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestValidateReservedEnvelopeKeyFlagged(t *testing.T) {
	// register / changed_when / failed_when / ignore_errors are now
	// activated by #210; block / rescue / always remain reserved for
	// #211, so use `block:` to pin the diagnostic to a key whose
	// semantics have not landed yet.
	data := []byte(`---
- tasks:
    - dokku_app:
        app: my-app
      block: []
`)
	problems := Validate(data, ValidateOptions{})
	if p := findProblem(problems, "envelope_key_unsupported"); p == nil {
		t.Fatalf("expected envelope_key_unsupported problem, got: %+v", problems)
	}
}

func TestValidateUnexpectedPlayKey(t *testing.T) {
	data := []byte(`---
- foo: bar
  tasks: []
`)
	problems := Validate(data, ValidateOptions{})
	p := findProblem(problems, "recipe_shape")
	if p == nil {
		t.Fatalf("expected recipe_shape problem, got: %+v", problems)
	}
	if !strings.Contains(p.Message, `"foo"`) {
		t.Errorf("expected message to mention foo, got: %q", p.Message)
	}
}

func TestValidateLineColumnAnchored(t *testing.T) {
	data := []byte(`---
- tasks:
    - dokku_appp:
        app: my-app
`)
	problems := Validate(data, ValidateOptions{})
	p := findProblem(problems, "unknown_task_type")
	if p == nil {
		t.Fatalf("expected unknown_task_type problem")
	}
	if p.Line != 3 {
		t.Errorf("expected Line=3, got %d", p.Line)
	}
	if p.Column == 0 {
		t.Errorf("expected non-zero Column, got %d", p.Column)
	}
}

func TestParseYAMLErrorPosition(t *testing.T) {
	tests := []struct {
		msg  string
		line int
		col  int
	}{
		{"yaml: line 5: did not find expected key", 5, 0},
		{"yaml: line 12, column 7: foo", 12, 7},
		{"some unrelated error", 0, 0},
	}
	for _, tt := range tests {
		gotLine, gotCol := parseYAMLErrorPosition(tt.msg)
		if gotLine != tt.line || gotCol != tt.col {
			t.Errorf("parseYAMLErrorPosition(%q) = (%d, %d), want (%d, %d)", tt.msg, gotLine, gotCol, tt.line, tt.col)
		}
	}
}

func TestParseSigilErrorPosition(t *testing.T) {
	msg := "template: tasks.yml:5: unclosed action started at tasks.yml:4"
	line, col := parseSigilErrorPosition(msg)
	if line != 5 {
		t.Errorf("expected line 5, got %d", line)
	}
	if col != 0 {
		t.Errorf("expected col 0, got %d", col)
	}
}

func TestValidateExprPredicateCompileError(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: deploy
      when: 'env =='
      dokku_app:
        app: x
`)
	problems := Validate(data, ValidateOptions{})
	p := findProblem(problems, "expr_compile")
	if p == nil {
		t.Fatalf("expected expr_compile problem, got: %+v", problems)
	}
	if p.Line == 0 {
		t.Errorf("expected non-zero source line, got %d", p.Line)
	}
}

func TestValidateExprPredicateLoopStringForm(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: deploy
      loop: 'apps where ('
      dokku_app:
        app: x
`)
	problems := Validate(data, ValidateOptions{})
	if findProblem(problems, "expr_compile") == nil {
		t.Fatalf("expected expr_compile problem on loop, got: %+v", problems)
	}
}

func TestValidateLoopVarOutsideLoopFlagged(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: deploy
      dokku_app:
        app: "{{ .item }}"
`)
	problems := Validate(data, ValidateOptions{})
	if findProblem(problems, "loop_var_outside_loop") == nil {
		t.Fatalf("expected loop_var_outside_loop problem, got: %+v", problems)
	}
}

func TestValidateLoopVarInsideLoopAllowed(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: deploy
      loop: [a, b]
      dokku_app:
        app: "{{ .item }}"
`)
	problems := Validate(data, ValidateOptions{})
	if p := findProblem(problems, "loop_var_outside_loop"); p != nil {
		t.Fatalf("did not expect loop_var_outside_loop inside a loop body, got: %+v", p)
	}
}

func TestValidateActiveEnvelopeKeysNotReserved(t *testing.T) {
	// name / tags / when / loop are activated by #205 and must not
	// produce an envelope_key_unsupported diagnostic.
	data := []byte(`---
- tasks:
    - name: deploy
      tags: [api]
      when: 'true'
      dokku_app:
        app: x
`)
	problems := Validate(data, ValidateOptions{})
	if p := findProblem(problems, "envelope_key_unsupported"); p != nil {
		t.Fatalf("did not expect envelope_key_unsupported, got: %+v", p)
	}
}

// TestValidateRegisterUniqueWithinPlay reports a duplicate register
// name declared twice in the same play.
func TestValidateRegisterUniqueWithinPlay(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: first
      register: foo
      dokku_app:
        app: app-a
    - name: second
      register: foo
      dokku_app:
        app: app-b
`)
	problems := Validate(data, ValidateOptions{})
	if got := countProblems(problems, "register_duplicate"); got != 1 {
		t.Fatalf("expected exactly one register_duplicate, got %d: %+v", got, problems)
	}
}

// TestValidateRegisterUniqueAcrossPlays reports a duplicate register
// name declared in two different plays. The registered map is
// recipe-wide at apply / plan time, so the validator's check is too.
func TestValidateRegisterUniqueAcrossPlays(t *testing.T) {
	data := []byte(`---
- name: api
  tasks:
    - register: foo
      dokku_app:
        app: api
- name: worker
  tasks:
    - register: foo
      dokku_app:
        app: worker
`)
	problems := Validate(data, ValidateOptions{})
	if got := countProblems(problems, "register_duplicate"); got != 1 {
		t.Fatalf("expected exactly one register_duplicate, got %d: %+v", got, problems)
	}
}

// TestValidateRegisterUniqueAcceptsDistinctNames passes when each
// register declaration uses a different name.
func TestValidateRegisterUniqueAcceptsDistinctNames(t *testing.T) {
	data := []byte(`---
- tasks:
    - register: alpha
      dokku_app:
        app: a
    - register: beta
      dokku_app:
        app: b
`)
	problems := Validate(data, ValidateOptions{})
	if p := findProblem(problems, "register_duplicate"); p != nil {
		t.Fatalf("did not expect register_duplicate, got: %+v", p)
	}
}

// TestValidateChangedWhenCompileError pins that compile errors in
// changed_when are surfaced via the existing expr_compile diagnostic
// shape (the validator pre-compiles changed_when / failed_when).
func TestValidateChangedWhenCompileError(t *testing.T) {
	data := []byte(`---
- tasks:
    - changed_when: 'env =='
      dokku_app:
        app: x
`)
	problems := Validate(data, ValidateOptions{})
	if p := findProblem(problems, "expr_compile"); p == nil {
		t.Fatalf("expected expr_compile problem, got: %+v", problems)
	}
}
