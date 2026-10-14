package tasks

import (
	"reflect"
	"testing"
)

// TestEveryTaskDeclaresMinimumVersions is the registry walk that keeps a new
// task from shipping without a floor. The declaration is what plan, apply,
// export, and the catalog all read, so a task that parses on this server and
// a task page that names a different release cannot drift apart.
func TestEveryTaskDeclaresMinimumVersions(t *testing.T) {
	for name, task := range allRegisteredTasks() {
		support, ok := TaskMinimumVersions(task)
		if !ok {
			t.Errorf("task %q does not implement VersionDocer (add MinimumVersions())", name)
			continue
		}
		if _, err := parseVersion(support.Dokku); err != nil {
			t.Errorf("task %q Dokku floor %q: %v", name, support.Dokku, err)
		}
		schema, err := TaskSchemaOf(name, task)
		if err != nil {
			t.Errorf("task %q schema: %v", name, err)
			continue
		}
		if schema.MinimumDokkuVersion != support.Dokku {
			t.Errorf("task %q catalog minimum_dokku_version = %q, declaration %q", name, schema.MinimumDokkuVersion, support.Dokku)
		}
		if len(schema.Requirements) != len(support.Plugins) || len(schema.PluginRequirements) != len(support.Plugins) {
			t.Errorf("task %q catalog plugins = %d requirements / %d plugin_requirements, declaration %d", name, len(schema.Requirements), len(schema.PluginRequirements), len(support.Plugins))
			continue
		}
		for i, plugin := range support.Plugins {
			if plugin.Text == "" {
				t.Errorf("task %q plugin %d has no Text", name, i)
			}
			if schema.Requirements[i] != plugin.Text {
				t.Errorf("task %q requirements[%d] = %q, Text %q", name, i, schema.Requirements[i], plugin.Text)
			}
			if _, err := parseVersion(plugin.Minimum); err != nil {
				t.Errorf("task %q plugin %d minimum %q: %v", name, i, plugin.Minimum, err)
			}
			item := schema.PluginRequirements[i]
			if item.Minimum != plugin.Minimum {
				t.Errorf("task %q plugin_requirements[%d].minimum = %q, want %q", name, i, item.Minimum, plugin.Minimum)
			}
			if plugin.WhenField != "" {
				field, ok := structField(task, plugin.WhenField)
				if !ok || field.Kind() != reflect.Bool {
					t.Errorf("task %q WhenField %q is not a bool field", name, plugin.WhenField)
				}
				if got, want := item.When, recipeKeyFor(task, plugin.WhenField); got != want || want == "" {
					t.Errorf("task %q plugin_requirements[%d].when = %q, want recipe key %q", name, i, got, want)
				}
			}
			if plugin.NameField != "" {
				field, ok := structField(task, plugin.NameField)
				if !ok || field.Kind() != reflect.String {
					t.Errorf("task %q NameField %q is not a string field", name, plugin.NameField)
				}
				if item.Name != "" {
					t.Errorf("task %q plugin_requirements[%d] sets name %q alongside name_field", name, i, item.Name)
				}
				if got, want := item.NameField, recipeKeyFor(task, plugin.NameField); got != want || want == "" {
					t.Errorf("task %q plugin_requirements[%d].name_field = %q, want recipe key %q", name, i, got, want)
				}
				continue
			}
			if plugin.Name == "" {
				t.Errorf("task %q plugin %d has neither Name nor NameField", name, i)
			}
			if item.Name != plugin.Name {
				t.Errorf("task %q plugin_requirements[%d].name = %q, want %q", name, i, item.Name, plugin.Name)
			}
		}
	}
}
