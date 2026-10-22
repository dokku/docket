package tasks

import (
	"fmt"
	"reflect"
	"strings"

	defaults "github.com/mcuadros/go-defaults"
)

// A consumer that manages resources declaratively, such as a Terraform
// provider, has to know for every task type which state applies the resource
// and which tears it down. It used to work that out from each task's `state`
// choices with a rule every embedder rewrote, and that broke silently when a
// task gained a state the rule did not expect (#640). The rule now lives here,
// the catalog publishes its answer per task, and TestTaskLifecycles holds
// every registered task to it.
//
// The rule is derived from the `state` field alone: apply with `set` when the
// task offers it, since `set` declares the whole resource, else with the
// field's default; tear down with `clear` when offered, else `absent`, and not
// at all for an imperative task. A task the rule does not fit implements
// LifecycleDocer instead. One does today: dokku_storage_ensure offers absent
// but refuses it, so it declares no teardown.

// LifecycleSchema is the pair of states a consumer drives a task through: the
// one that applies the resource, and the one that undoes it.
type LifecycleSchema struct {
	// Apply is the state that declares the resource.
	Apply State `json:"apply"`

	// Teardown is the state that removes what Apply declared. Empty for an
	// imperative task, which has nothing to undo, and for a task that
	// declares no teardown.
	Teardown State `json:"teardown,omitempty"`
}

// LifecycleDocer is an optional interface for a task whose lifecycle the
// generic rule gets wrong. TestTaskLifecycles pins which tasks implement it.
type LifecycleDocer interface {
	Lifecycle() LifecycleSchema
}

// TaskLifecycle returns the state that applies t's resource and the state
// that tears it down, the same pair the catalog publishes as `lifecycle`.
// Apply is `set` when t offers it and the `state` field's default otherwise;
// Teardown is `clear` when offered, else `absent`, and empty when t is
// imperative. A task the rule does not fit declares its own pair instead:
// dokku_storage_ensure offers `absent` but refuses it, so it has no Teardown.
//
//sdk:export
func TaskLifecycle(t Task) LifecycleSchema {
	if d, ok := t.(LifecycleDocer); ok {
		return d.Lifecycle()
	}
	choices, def := stateFieldOf(taskStructOf(t))
	lifecycle := LifecycleSchema{Apply: StatePresent}
	if def != "" {
		lifecycle.Apply = State(def)
	}
	if containsState(choices, StateSet) {
		lifecycle.Apply = StateSet
	}
	if TaskImperative(t) {
		return lifecycle
	}
	switch {
	case containsState(choices, StateClear):
		lifecycle.Teardown = StateClear
	case containsState(choices, StateAbsent):
		lifecycle.Teardown = StateAbsent
	}
	return lifecycle
}

// stateFieldOf returns the choices and the default of rt's `state` field. Both
// are empty for a struct with no state field.
func stateFieldOf(rt reflect.Type) ([]string, string) {
	if rt == nil || rt.Kind() != reflect.Struct {
		return nil, ""
	}
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if recipeKey(field) != "state" {
			continue
		}
		return conditionalStates(field.Tag.Get("options")), field.Tag.Get("default")
	}
	return nil, ""
}

// TaskInState returns a copy of t moved into state: its `state` key set to
// state, and every field whose `forbidden_when` names state reset to its
// default, or to its zero value when it has none. Element fields of a list of
// structs are reset the same way. t itself is left unchanged, and the copy
// shares no list or map with it.
//
// It is how a consumer tears down a resource it applied: the task it applied
// carries values the teardown state refuses, such as dokku_ps_scale's `scale`
// under `clear` or a property task's `value` under `absent`, and moving it
// with TaskInState(t, TaskLifecycle(t).Teardown) drops them. A field the state
// requires is not invented, so moving a task into a state it lacks the input
// for still fails its validation.
//
// The error is a state t does not offer.
//
//sdk:export
func TaskInState(t Task, state State) (Task, error) {
	rv := reflect.ValueOf(t)
	if !rv.IsValid() {
		return nil, fmt.Errorf("no task to move into state %q", state)
	}
	pointer := rv.Kind() == reflect.Pointer
	if pointer {
		if rv.IsNil() {
			return nil, fmt.Errorf("no task to move into state %q", state)
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return nil, fmt.Errorf("task %T is not a struct", t)
	}

	choices, _ := stateFieldOf(rv.Type())
	if !containsState(choices, state) {
		return nil, fmt.Errorf("task %T offers no state %q; its states are %s", t, state, strings.Join(choices, ", "))
	}

	moved := reflect.New(rv.Type()).Elem()
	moved.Set(deepCopyValue(rv))
	resetForbiddenFields(moved, state)
	if field, ok := fieldByRecipeKey(moved, "state"); ok {
		field.SetString(string(state))
	}

	out := moved
	if pointer {
		out = moved.Addr()
	}
	task, ok := out.Interface().(Task)
	if !ok {
		return nil, fmt.Errorf("task %T does not implement Task", t)
	}
	return task, nil
}

// resetForbiddenFields resets every field of the struct rv whose
// forbidden_when names state, then does the same inside each element of its
// lists of structs. rv must be addressable.
func resetForbiddenFields(rv reflect.Value, state State) {
	rt := rv.Type()
	var fresh reflect.Value
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if recipeKey(field) == "" {
			continue
		}
		if stateListed(field.Tag.Get(forbiddenWhenTag), state) {
			if !fresh.IsValid() {
				// go-defaults fills a fresh struct exactly as the loader
				// does, so a reset field reads as a recipe that left it out.
				ptr := reflect.New(rt)
				defaults.SetDefaults(ptr.Interface())
				fresh = ptr.Elem()
			}
			rv.Field(i).Set(fresh.Field(i))
			continue
		}
		list := rv.Field(i)
		if list.Kind() != reflect.Slice && list.Kind() != reflect.Array {
			continue
		}
		for j := 0; j < list.Len(); j++ {
			elem := list.Index(j)
			for elem.Kind() == reflect.Pointer && !elem.IsNil() {
				elem = elem.Elem()
			}
			if elem.Kind() == reflect.Struct && elem.CanSet() {
				resetForbiddenFields(elem, state)
			}
		}
	}
}
