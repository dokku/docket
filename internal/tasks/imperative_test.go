package tasks

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestImperativeTasksOfferNoTeardown pins which tasks declare themselves
// imperative, and holds the declaration to what it promises: an imperative
// task has no absent or clear state, and every task with neither is either
// imperative or listed here as declarative without a teardown.
func TestImperativeTasksOfferNoTeardown(t *testing.T) {
	t.Parallel()
	want := []string{"dokku_app_clone", "dokku_git_from_archive", "dokku_git_from_image", "dokku_git_sync"}
	// dokku_ps_scale declares the app's process counts, but has nothing to
	// remove: an app always runs some count of each process type.
	declarativeWithoutTeardown := map[string]bool{"dokku_ps_scale": true}

	var got []string
	for _, name := range TaskTypes() {
		task, _ := Lookup(name)
		options := stateOptions(task)
		teardown := false
		for _, o := range options {
			if o == string(StateAbsent) || o == string(StateClear) {
				teardown = true
			}
		}
		if TaskImperative(task) {
			got = append(got, name)
			if teardown {
				t.Errorf("%s is imperative but offers state options %v", name, options)
			}
			continue
		}
		if !teardown && len(options) > 0 && !declarativeWithoutTeardown[name] {
			t.Errorf("%s offers no absent or clear state (%v) but does not declare itself imperative", name, options)
		}
	}
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("imperative tasks = %v, want %v", got, want)
	}
}

// stateOptions returns the options a task's State field declares, including
// one it embeds, or nil for a task with no State field.
func stateOptions(task Task) []string {
	typ := reflect.TypeOf(task)
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	field, ok := typ.FieldByName("State")
	if !ok || field.Tag.Get("options") == "" {
		return nil
	}
	return strings.Split(field.Tag.Get("options"), ",")
}
