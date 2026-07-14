package tasks

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
)

// programCache holds compiled expr programs keyed by their source string.
// Loop iterations re-use the same compiled program so a literal recipe
// like `loop: [a,b,c]` plus `when: 'item != "b"'` only compiles `when`
// once. The cache is process-wide because expr programs are read-only
// after compilation.
var programCache sync.Map // map[string]*vm.Program

// CompilePredicate compiles src as an expr-lang/expr program suitable for
// envelope predicates (when:, changed_when:, failed_when:, scalar-form
// loop:). Returns the cached compiled program when src has already been
// compiled. Empty src returns (nil, nil); callers should treat nil as
// "no predicate".
//
// The compiler is configured with AllowUndefinedVariables so a predicate
// referencing a context key that the apply / plan path has not populated
// yet (e.g. .registered.foo before #210 lands) does not error at compile
// time - it evaluates to nil at runtime.
func CompilePredicate(src string) (*vm.Program, error) {
	if src == "" {
		return nil, nil
	}
	if cached, ok := programCache.Load(src); ok {
		return cached.(*vm.Program), nil
	}
	prog, err := expr.Compile(src, expr.AllowUndefinedVariables())
	if err != nil {
		return nil, err
	}
	actual, _ := programCache.LoadOrStore(src, prog)
	return actual.(*vm.Program), nil
}

// EvalBool runs prog against env and reports the truthiness of the
// result. Non-bool results are coerced via the standard expr truth rules
// (nil, 0, "", and empty collections are false; everything else is
// true). Returns an error when the program errors at run time.
func EvalBool(prog *vm.Program, env map[string]interface{}) (bool, error) {
	if prog == nil {
		return true, nil
	}
	out, err := expr.Run(prog, env)
	if err != nil {
		return false, err
	}
	return truthy(out), nil
}

// EvalList runs prog against env and asserts that the result is a list.
// Returns an error when the program errors or returns a non-list value.
func EvalList(prog *vm.Program, env map[string]interface{}) ([]interface{}, error) {
	if prog == nil {
		return nil, fmt.Errorf("expr program is nil")
	}
	out, err := expr.Run(prog, env)
	if err != nil {
		return nil, err
	}
	switch v := out.(type) {
	case []interface{}:
		return v, nil
	case nil:
		return nil, fmt.Errorf("loop expression returned nil; expected a list")
	}
	// Reflect-based fallback for typed slices ([]string, []int, etc.).
	return reflectToList(out)
}

// truthy mirrors expr's Boolean coercion: nil, false, zero numerics,
// empty strings, and empty collections are false; everything else is
// true. Reaching the default case for an unsupported type returns true
// since a non-nil structural value is meaningful.
func truthy(v interface{}) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	case []interface{}:
		return len(x) > 0
	case map[string]interface{}:
		return len(x) > 0
	}
	// Reflect fallback for typed collections (`[]string` from
	// registered.foo.Commands, `[]TaskOutputState` from .Results, typed
	// maps) that do not match the generic case arms above. A typed nil
	// slice/map has Kind Slice/Map and length 0, so an empty one is
	// correctly false rather than falling through to the default true.
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return rv.Len() > 0
	}
	return true
}
