package tasks

import (
	"reflect"
	"strings"
	"testing"
)

// propertyTasksWithoutTable names the tasks that look like property tasks but
// have no enumerable table, with the reason. Being on this list is a statement
// that docket cannot know the legal property names, not that the task was
// overlooked.
var propertyTasksWithoutTable = map[string]string{
	"dokku_service_property": "the legal names come from whichever datastore plugin backs the service, and no plugin exposes a report that enumerates them",
}

// TestEveryPropertyTaskDeclaresPropertyTable asserts that every task whose type
// key ends in _property either declares its table or is explicitly exempt.
//
// The shared helpers already take a PropertyTableDocer, so a task cannot reach
// planProperty or validatePropertyInput without a table; this catches the other
// direction - a new property task that hand-rolls its Plan() and so publishes
// nothing to the catalog, which is how dokku_service_property came to be the
// odd one out. Checking the allowlist in both directions keeps it from rotting
// once such a task grows a real table.
func TestEveryPropertyTaskDeclaresPropertyTable(t *testing.T) {
	for name, task := range RegisteredTasks {
		if !strings.HasSuffix(name, "_property") {
			continue
		}
		_, declared := TaskPropertyTable(task)
		reason, exempt := propertyTasksWithoutTable[name]

		switch {
		case declared && exempt:
			t.Errorf("task %q declares a property table but is listed as exempt (%q); drop it from propertyTasksWithoutTable", name, reason)
		case !declared && !exempt:
			t.Errorf("task %q does not implement PropertyTableDocer (add a PropertyTable() declaration, or explain the exemption in propertyTasksWithoutTable)", name)
		}
	}

	for name := range propertyTasksWithoutTable {
		if _, ok := RegisteredTasks[name]; !ok {
			t.Errorf("propertyTasksWithoutTable names %q, which is not a registered task", name)
		}
	}
}

// TestPropertyTablePluginMatchesTaskType asserts that a task's table belongs to
// the plugin its name claims. This is the one drift the interface cannot
// prevent: a new property task written by copying an existing one, where the
// key map got renamed but the subcommand did not.
func TestPropertyTablePluginMatchesTaskType(t *testing.T) {
	for name, task := range RegisteredTasks {
		table, declared := TaskPropertyTable(task)
		if !declared {
			continue
		}
		want := strings.TrimSuffix(strings.TrimPrefix(name, "dokku_"), "_property")
		if got := strings.ReplaceAll(table.Plugin(), "-", "_"); got != want {
			t.Errorf("task %q uses the %q plugin (subcommand %q); want the %q plugin", name, table.Plugin(), table.Subcommand, want)
		}
	}
}

// TestPropertyTableEntriesHaveAScope asserts that no property is unusable. An
// entry with neither a per-app nor a global report key is rejected in both
// scopes by validateProperty, so it can never be set at all.
func TestPropertyTableEntriesHaveAScope(t *testing.T) {
	for name, task := range RegisteredTasks {
		table, declared := TaskPropertyTable(task)
		if !declared {
			continue
		}
		if len(table.Keys) == 0 {
			t.Errorf("task %q declares an empty property table", name)
		}
		for property, entry := range table.Keys {
			if entry.PerApp == "" && entry.Global == "" {
				t.Errorf("task %q property %q has no per-app or global form, so it can never be set", name, property)
			}
		}
	}
}

// TestPropertyTableGlobalKeysRequireAGlobalField asserts that a task with no
// `global` recipe key declares no globally-scoped property. Such a task passes
// a literal false for global (dokku_scheduler_docker_local_property), so a
// global-only entry in its table would be unreachable - and would be published
// by the catalog as settable when nothing can set it.
func TestPropertyTableGlobalKeysRequireAGlobalField(t *testing.T) {
	for name, task := range RegisteredTasks {
		table, declared := TaskPropertyTable(task)
		if !declared {
			continue
		}
		if hasGlobalField(task) {
			continue
		}
		for property, entry := range table.Keys {
			if entry.Global != "" {
				t.Errorf("task %q has no `global` field but property %q declares the global key %q", name, property, entry.Global)
			}
			if entry.PerApp == "" {
				t.Errorf("task %q has no `global` field, so property %q needs a per-app key", name, property)
			}
		}
	}
}

// hasGlobalField reports whether a task accepts `global:` in a recipe.
func hasGlobalField(task Task) bool {
	rt := taskStructType(task)
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if field.PkgPath == "" && catalogYAMLName(field.Tag, field.Name) == "global" {
			return true
		}
	}
	return false
}

// TestDynamicPropertyFamiliesArePublished asserts the exported view matches the
// table the validator reads, so a consumer told to accept a prefix is told
// about every prefix docket itself accepts.
func TestDynamicPropertyFamiliesArePublished(t *testing.T) {
	for plugin, families := range dynamicPropertyFamilies {
		published := DynamicPropertyFamilies(plugin)
		if len(published) != len(families) {
			t.Errorf("plugin %q publishes %d families; the table has %d", plugin, len(published), len(families))
			continue
		}
		for _, family := range families {
			want := DynamicPropertySchema{Prefix: family.Prefix, Probeable: family.Probeable, Sensitive: family.Sensitive}
			found := false
			for _, got := range published {
				if got == want {
					found = true
				}
			}
			if !found {
				t.Errorf("plugin %q does not publish %+v (published %+v)", plugin, want, published)
			}

			// A prefix nothing recognises would be published as legal and then
			// rejected by validateProperty.
			if !isDynamicProperty(plugin, family.Prefix+"EXAMPLE") {
				t.Errorf("plugin %q publishes prefix %q but does not accept a name using it", plugin, family.Prefix)
			}
		}
	}

	if got := DynamicPropertyFamilies("apps"); got != nil {
		t.Errorf("a plugin with no dynamic families published %+v; want nil", got)
	}
}

// TestDynamicFamilySensitivityIsIndependentOfProbing asserts every family's
// Sensitive mark survives the lookup planProperty and the exporters make.
// Sensitivity used to be read off an entry only a probeable family ever got, so
// an unprobeable credential reached argv in cleartext (#457); this fails the
// build if that coupling comes back.
func TestDynamicFamilySensitivityIsIndependentOfProbing(t *testing.T) {
	for plugin, families := range dynamicPropertyFamilies {
		for _, family := range families {
			property := family.Prefix + "EXAMPLE"
			if got := propertyEntry(plugin, property, nil).Sensitive; got != family.Sensitive {
				t.Errorf("propertyEntry(%q, %q).Sensitive = %v; want %v (probeable: %v)",
					plugin, property, got, family.Sensitive, family.Probeable)
			}
		}
	}
}

// TestPropertyTablesAreDistinct asserts no two property tasks share a table.
// Copying a task file and forgetting to rename the table var would otherwise
// give two tasks the same properties and the same :set subcommand, and every
// other test here would still pass for one of them.
func TestPropertyTablesAreDistinct(t *testing.T) {
	seen := map[string]string{}
	for name, task := range RegisteredTasks {
		table, declared := TaskPropertyTable(task)
		if !declared {
			continue
		}
		if previous, ok := seen[table.Subcommand]; ok {
			t.Errorf("tasks %q and %q both use subcommand %q", previous, name, table.Subcommand)
		}
		seen[table.Subcommand] = name
	}
}

// TestPropertyTaskValidatesAgainstItsPublishedTable drives each task's own
// Validate() against every name its table publishes, and against one it does
// not. This is the end-to-end version of the invariant the helper signatures
// enforce structurally: what the catalog says a recipe may write is exactly
// what the task accepts.
func TestPropertyTaskValidatesAgainstItsPublishedTable(t *testing.T) {
	for name, task := range RegisteredTasks {
		table, declared := TaskPropertyTable(task)
		if !declared {
			continue
		}
		if _, ok := task.(InputValidator); !ok {
			t.Errorf("task %q declares a property table but has no Validate()", name)
			continue
		}

		// Every name the table publishes must validate in at least one of the
		// scopes the table says it supports.
		for property, entry := range table.Keys {
			global := entry.PerApp == "" && entry.Global != ""
			instance := newPropertyTaskInstance(t, task, property, global)
			if instance == nil {
				continue
			}
			if err := instance.Validate(); err != nil {
				t.Errorf("task %q rejects its own property %q (global=%v): %v", name, property, global, err)
			}
		}

		// And a name it does not publish must be rejected, unless a dynamic
		// family covers it.
		instance := newPropertyTaskInstance(t, task, "definitely-not-a-real-property", false)
		if instance == nil {
			continue
		}
		if err := instance.Validate(); err == nil {
			t.Errorf("task %q accepts a property absent from its table", name)
		}
	}
}

// TestRejectedPropertyFamiliesAreWellFormed checks the declarations themselves.
// RejectedPropertyFamily.err() formats all three fields unconditionally, so an
// empty one would ship a sentence with a hole in it; and a prefix that shadows
// a name the table publishes, or a dynamic family the plugin accepts, would
// make a legal recipe unwritable.
func TestRejectedPropertyFamiliesAreWellFormed(t *testing.T) {
	for name, task := range RegisteredTasks {
		table, declared := TaskPropertyTable(task)
		if !declared {
			continue
		}
		for _, family := range table.Rejected {
			if family.Prefix == "" || family.Replacement == "" || family.Reason == "" {
				t.Errorf("task %q declares an incomplete rejected family %+v", name, family)
				continue
			}
			if _, ok := RegisteredTasks[family.Replacement]; !ok {
				t.Errorf("task %q points %q at %q, which is not a registered task", name, family.Prefix, family.Replacement)
			}
			for property := range table.Keys {
				if strings.HasPrefix(property, family.Prefix) {
					t.Errorf("task %q rejects prefix %q, which shadows its own property %q", name, family.Prefix, property)
				}
			}
			for _, dynamic := range DynamicPropertyFamilies(table.Plugin()) {
				if strings.HasPrefix(family.Prefix, dynamic.Prefix) || strings.HasPrefix(dynamic.Prefix, family.Prefix) {
					t.Errorf("task %q rejects prefix %q, which overlaps the dynamic family %q on plugin %q",
						name, family.Prefix, dynamic.Prefix, table.Plugin())
				}
			}
		}
	}
}

// TestRejectedPropertyFamiliesReportIdenticallyFromPlanAndValidate is the
// invariant behind #458: a refusal declared on the table is reached through
// validatePropertyInput, which planProperty and every task's Validate() both
// call, so the two cannot report a member of the family differently. Driving it
// generically means the next task to declare a family gets the guarantee
// without writing its own test.
//
// No server is contacted: validatePropertyInput returns before planProperty
// probes anything.
func TestRejectedPropertyFamiliesReportIdenticallyFromPlanAndValidate(t *testing.T) {
	checked := 0
	for name, task := range RegisteredTasks {
		table, declared := TaskPropertyTable(task)
		if !declared {
			continue
		}
		for _, family := range table.Rejected {
			checked++
			property := family.Prefix + "example"
			instance := newPropertyTaskInstance(t, task, property, false)
			if instance == nil {
				continue
			}

			validateErr := instance.Validate()
			if validateErr == nil {
				t.Errorf("task %q accepts %q from its own rejected family", name, property)
				continue
			}

			planner, ok := instance.(interface{ Plan() PlanResult })
			if !ok {
				t.Errorf("task %q has no Plan() to compare against", name)
				continue
			}
			plan := planner.Plan()
			if plan.Status != PlanStatusError || plan.Error == nil {
				t.Errorf("task %q plans %q instead of rejecting it: status %q", name, property, plan.Status)
				continue
			}

			if validateErr.Error() != plan.Error.Error() {
				t.Errorf("task %q reports %q differently:\n  validate: %v\n  plan:     %v", name, property, validateErr, plan.Error)
			}
			if !strings.Contains(validateErr.Error(), family.Replacement) {
				t.Errorf("task %q rejects %q without naming %q: %v", name, property, family.Replacement, validateErr)
			}
		}
	}

	// A registry-wide loop that iterates nothing passes silently, and this one
	// is the whole guarantee behind #458.
	if checked == 0 {
		t.Error("no task declares a rejected property family; the parity invariant went untested")
	}
}

// newPropertyTaskInstance builds a runnable copy of a property task addressing
// one property, so its Validate() can be driven against its own table. Returns
// nil for a task whose fields do not fit the shape (none today).
func newPropertyTaskInstance(t *testing.T, prototype Task, property string, global bool) InputValidator {
	t.Helper()
	rt := taskStructType(prototype)
	value := reflect.New(rt).Elem()

	set := func(name string, v interface{}) bool {
		for i := 0; i < rt.NumField(); i++ {
			field := rt.Field(i)
			if field.PkgPath != "" || catalogYAMLName(field.Tag, field.Name) != name {
				continue
			}
			rv := reflect.ValueOf(v)
			if !rv.Type().ConvertibleTo(field.Type) {
				return false
			}
			value.Field(i).Set(rv.Convert(field.Type))
			return true
		}
		return false
	}

	if !set("property", property) || !set("value", "some-value") || !set("state", string(StatePresent)) {
		return nil
	}
	if global {
		if !set("global", true) {
			return nil
		}
	} else if !set("app", "some-app") {
		return nil
	}

	instance, ok := value.Interface().(InputValidator)
	if !ok {
		return nil
	}
	return instance
}
