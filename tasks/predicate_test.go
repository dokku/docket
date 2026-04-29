package tasks

import (
	"fmt"
	"strings"
	"testing"
)

func TestCompilePredicateEmptyReturnsNil(t *testing.T) {
	prog, err := CompilePredicate("")
	if err != nil {
		t.Fatalf("CompilePredicate(empty) err = %v", err)
	}
	if prog != nil {
		t.Fatalf("CompilePredicate(empty) prog = %v, want nil", prog)
	}
}

func TestCompilePredicateSyntaxErrorReports(t *testing.T) {
	_, err := CompilePredicate("env ==")
	if err == nil {
		t.Fatal("expected syntax error, got nil")
	}
	if !strings.Contains(err.Error(), "(") {
		t.Errorf("expected error to include position info, got: %v", err)
	}
}

func TestCompilePredicateCacheReturnsSamePointer(t *testing.T) {
	const src = "env == \"prod\""

	a, err := CompilePredicate(src)
	if err != nil {
		t.Fatalf("first compile: %v", err)
	}
	b, err := CompilePredicate(src)
	if err != nil {
		t.Fatalf("second compile: %v", err)
	}
	if a != b {
		t.Fatalf("cache miss: a=%p b=%p", a, b)
	}
}

func TestEvalBoolTruthy(t *testing.T) {
	prog, err := CompilePredicate("env == \"prod\"")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got, err := EvalBool(prog, map[string]interface{}{"env": "prod"})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !got {
		t.Errorf("got false, want true")
	}
}

func TestEvalBoolFalsy(t *testing.T) {
	prog, err := CompilePredicate("env == \"prod\"")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got, err := EvalBool(prog, map[string]interface{}{"env": "staging"})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got {
		t.Errorf("got true, want false")
	}
}

func TestEvalBoolNilProgramIsTrue(t *testing.T) {
	got, err := EvalBool(nil, nil)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !got {
		t.Errorf("nil program must evaluate as true (no predicate)")
	}
}

func TestEvalListReturnsSlice(t *testing.T) {
	prog, err := CompilePredicate("[\"a\", \"b\", \"c\"]")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got, err := EvalList(prog, map[string]interface{}{})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
}

func TestEvalListNonListErrors(t *testing.T) {
	prog, err := CompilePredicate("\"not a list\"")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	if _, err := EvalList(prog, nil); err == nil {
		t.Fatal("expected non-list error, got nil")
	}
}

// TestEvalBoolResultStructFields covers the post-execute predicate
// shape: result is a TaskOutputState struct value passed under the
// "result" key. expr-lang/expr accesses Go struct fields by name via
// reflection, so `result.Changed` and `result.Stderr` work directly.
func TestEvalBoolResultStructFields(t *testing.T) {
	state := TaskOutputState{Changed: true, Stderr: "expected message"}

	prog, err := CompilePredicate(`result.Changed`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got, err := EvalBool(prog, map[string]interface{}{"result": state})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !got {
		t.Errorf("result.Changed: got false, want true")
	}

	prog, err = CompilePredicate(`result.Stderr contains "expected"`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	got, err = EvalBool(prog, map[string]interface{}{"result": state})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !got {
		t.Errorf("result.Stderr contains: got false, want true")
	}
}

// TestEvalBoolResultErrorNilCheck covers the `result.Error != nil`
// pattern from the issue body. A nil error renders as nil under expr's
// reflection bridge, so the comparison uses the `nil` keyword.
func TestEvalBoolResultErrorNilCheck(t *testing.T) {
	prog, err := CompilePredicate(`result.Error != nil`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	noErr := TaskOutputState{}
	got, err := EvalBool(prog, map[string]interface{}{"result": noErr})
	if err != nil {
		t.Fatalf("eval (no error): %v", err)
	}
	if got {
		t.Errorf("result.Error != nil with nil Error: got true, want false")
	}

	withErr := TaskOutputState{Error: fmt.Errorf("boom")}
	got, err = EvalBool(prog, map[string]interface{}{"result": withErr})
	if err != nil {
		t.Fatalf("eval (with error): %v", err)
	}
	if !got {
		t.Errorf("result.Error != nil with non-nil Error: got false, want true")
	}
}

// TestEvalBoolRegisteredLookup covers the `.registered.<name>` shape.
// The map carries RegisteredValue entries, and expr's struct-field
// access traverses the embedded TaskOutputState so
// `registered.foo.Changed` resolves to the aggregate flag.
func TestEvalBoolRegisteredLookup(t *testing.T) {
	prog, err := CompilePredicate(`registered.foo.Changed`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	registered := map[string]RegisteredValue{
		"foo": {TaskOutputState: TaskOutputState{Changed: true}},
	}
	got, err := EvalBool(prog, map[string]interface{}{"registered": registered})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !got {
		t.Errorf("got false, want true")
	}
}

// TestEvalBoolRegisteredMissingIsFalsy covers AllowUndefinedVariables
// for nested key access: a predicate referencing a register name that
// has not been declared yet evaluates to nil at runtime, which is
// falsy. No expr error is raised.
func TestEvalBoolRegisteredMissingIsFalsy(t *testing.T) {
	prog, err := CompilePredicate(`registered.missing.Error != nil`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	registered := map[string]RegisteredValue{}
	got, err := EvalBool(prog, map[string]interface{}{"registered": registered})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got {
		t.Errorf("missing register key should be nil-comparable; got true")
	}
}

// TestEvalBoolRegisteredResultsIndex covers loop-style register access
// where Results is the per-iteration list.
func TestEvalBoolRegisteredResultsIndex(t *testing.T) {
	prog, err := CompilePredicate(`registered.foo.Results[0].Changed`)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	registered := map[string]RegisteredValue{
		"foo": {
			TaskOutputState: TaskOutputState{Changed: true},
			Results: []TaskOutputState{
				{Changed: true},
				{Changed: false},
			},
		},
	}
	got, err := EvalBool(prog, map[string]interface{}{"registered": registered})
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if !got {
		t.Errorf("Results[0].Changed: got false, want true")
	}
}

// TestAggregateRegisteredAggregatesAcrossIterations pins the
// loop-aggregation rules: Changed = any iteration changed, Error =
// first non-nil error, last-iteration values for the rest.
func TestAggregateRegisteredAggregatesAcrossIterations(t *testing.T) {
	iters := []TaskOutputState{
		{Changed: false, Stderr: "one"},
		{Changed: true, Stderr: "two", Error: fmt.Errorf("first error")},
		{Changed: false, Stderr: "three", Error: fmt.Errorf("later error")},
	}
	got := AggregateRegistered(iters)
	if !got.Changed {
		t.Errorf("aggregate Changed should be true (any iteration changed)")
	}
	if got.Error == nil || got.Error.Error() != "first error" {
		t.Errorf("aggregate Error should be the first non-nil error; got %v", got.Error)
	}
	if got.Stderr != "three" {
		t.Errorf("aggregate Stderr should follow last iteration; got %q", got.Stderr)
	}
	if len(got.Results) != 3 {
		t.Errorf("Results should carry all iterations; got %d", len(got.Results))
	}
}

func TestAggregateRegisteredEmptyAndSingle(t *testing.T) {
	if got := AggregateRegistered(nil); got.Changed || got.Error != nil || len(got.Results) != 0 {
		t.Errorf("empty aggregate should be zero value; got %+v", got)
	}
	single := []TaskOutputState{{Changed: true, Stderr: "only"}}
	got := AggregateRegistered(single)
	if !got.Changed || got.Stderr != "only" {
		t.Errorf("single-element aggregate should mirror the input; got %+v", got)
	}
	if got.Results != nil {
		t.Errorf("single-element aggregate should leave Results nil; got %v", got.Results)
	}
}
