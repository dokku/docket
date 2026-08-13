package tasks

import (
	"reflect"
	"strings"
	"testing"
)

// toggleTasksWithOwnFields names the toggle tasks that declare their own field
// set instead of ToggleFields, with the reason. Being on this list is a
// statement that the task addresses its resource differently - a toggle that
// needs a `global` scope, say - not that it was missed when the shared type was
// extracted (#467).
//
// It is empty today: every toggle turns a plugin on or off for one app.
var toggleTasksWithOwnFields = map[string]string{}

// TestToggleTasksDeclareTheSharedFields asserts that every task delegating to
// planToggle declares its recipe shape by reusing ToggleFields rather than
// restating the two fields.
//
// This is the invariant #467 is about. Four tasks carried a field set that was
// identical apart from the plugin named in its `description`, with nothing
// binding the copies together, so a cross-cutting change - #427's identity tags
// - had to be applied four times by hand, and a copy that was missed would have
// failed nothing. The comparison is field-by-field including the full struct
// tag: reflect's ConvertibleTo is no use here because Go conversion between
// struct types ignores tag differences, which is exactly the difference that
// matters.
//
// Which tasks are toggles is read off the plan graph rather than off a
// `_toggle` name suffix. dokku_maintenance is a planToggle caller that is not
// spelled that way, and keying on the name is precisely what left it out of the
// issue's own count.
func TestToggleTasksDeclareTheSharedFields(t *testing.T) {
	g := buildPlanGraph(t)
	shared := reflect.TypeOf(ToggleFields{})

	toggles := map[string]bool{}
	for name, task := range RegisteredTasks {
		rt := taskStructType(task)
		if !g.reaches(planFuncKey(rt.Name()+".Plan"), "planToggle") {
			continue
		}
		toggles[name] = true

		reason, exempt := toggleTasksWithOwnFields[name]
		matched := sameStructShape(rt, shared)

		switch {
		case matched && exempt:
			t.Errorf("task %q has the ToggleFields shape but is listed as exempt (%q); drop it from toggleTasksWithOwnFields", name, reason)
		case !matched && !exempt:
			t.Errorf("task %q restates the toggle field set; declare it as `type %s ToggleFields`, or explain the difference in toggleTasksWithOwnFields", name, rt.Name())
		}
	}

	// A walk that silently found nothing would leave the whole test vacuous, so
	// cross-check the AST predicate against the independent one it replaced:
	// every task named `*_toggle` had better be in the set.
	for name := range RegisteredTasks {
		if strings.HasSuffix(name, "_toggle") && !toggles[name] {
			t.Errorf("task %q is named as a toggle but its Plan() does not reach planToggle, so the shared-field check skipped it", name)
		}
	}

	for name := range toggleTasksWithOwnFields {
		if _, ok := RegisteredTasks[name]; !ok {
			t.Errorf("toggleTasksWithOwnFields names %q, which is not a registered task", name)
		}
	}
}

// TestToggleFieldsShapeIsTagSensitive proves the check above fires on the
// duplication it exists to stop, not merely on a differing field list. The four
// copies #467 removed differed from one another in exactly one struct tag - the
// plugin named in the `state` description - so a shape comparison blind to tags
// would have called every one of them a match and enforced nothing.
func TestToggleFieldsShapeIsTagSensitive(t *testing.T) {
	shared := reflect.TypeOf(ToggleFields{})

	// Same field names and types as ToggleFields, one plugin-specific description.
	type pluginSpecificToggle struct {
		App   string `required:"true" identity:"key" yaml:"app" description:"Name of the app"`
		State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the checks plugin"`
	}

	if sameStructShape(reflect.TypeOf(pluginSpecificToggle{}), shared) {
		t.Error("a struct differing from ToggleFields only in the state description matches it; the shape check is not reading tags")
	}
	if !sameStructShape(reflect.TypeOf(ChecksToggleTask{}), shared) {
		t.Error("ChecksToggleTask does not match ToggleFields, so the shape check would reject every toggle task")
	}
}
