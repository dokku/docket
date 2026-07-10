package tasks

import (
	"encoding/json"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestIntegrationPlugin(t *testing.T) {
	skipIfNoDokkuT(t)

	// smoke-test-plugin is dokku's purpose-built plugin for exercising the
	// install/uninstall flow. Pinned to a tag for reproducibility.
	pluginName := "smoke-test-plugin"
	pluginURL := "https://github.com/dokku/smoke-test-plugin.git"
	pluginCommittish := "v0.9.0"

	uninstallPlugin := func() {
		subprocess.CallExecCommand(subprocess.ExecCommandInput{
			Command: "dokku",
			Args:    []string{"--quiet", "plugin:uninstall", pluginName},
		})
	}

	uninstallPlugin()
	defer uninstallPlugin()

	assertInstalled := func(t *testing.T, label string, want bool) {
		t.Helper()
		got, err := pluginInstalled(pluginName)
		if err != nil {
			t.Fatalf("%s: pluginInstalled failed: %v", label, err)
		}
		if got != want {
			t.Errorf("%s: plugin installed = %v, want %v", label, got, want)
		}
	}

	assertInstalled(t, "initial", false)

	// install the plugin
	installTask := PluginTask{Name: pluginName, URL: pluginURL, Committish: pluginCommittish, State: StatePresent}
	result := installTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to install plugin: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first install")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	assertInstalled(t, "after install", true)

	// install again - idempotent
	result = installTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second install: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent install")
	}

	// uninstall the plugin
	uninstallTask := PluginTask{Name: pluginName, State: StateAbsent}
	result = uninstallTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to uninstall plugin: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first uninstall")
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	assertInstalled(t, "after uninstall", false)

	// uninstall again - idempotent
	result = uninstallTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second uninstall: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent uninstall")
	}
}

func TestIntegrationExportPlugin(t *testing.T) {
	skipIfNoDokkuT(t)

	// A dokku predating plugin:list --format json ignores the flag and prints
	// stdout text with a zero exit, so skip unless the output parses as a JSON
	// array rather than guarding on a non-zero exit.
	probe, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "plugin:list", "--format", "json"},
	})
	if err != nil {
		t.Skipf("plugin:list --format json not available: %v", err)
	}
	var probed []pluginListEntry
	if err := json.Unmarshal(probe.StdoutBytes(), &probed); err != nil {
		t.Skipf("plugin:list --format json unsupported on this dokku: %v", err)
	}

	pluginName := "smoke-test-plugin"
	pluginURL := "https://github.com/dokku/smoke-test-plugin.git"
	pluginCommittish := "v0.9.0"

	uninstallPlugin := func() {
		subprocess.CallExecCommand(subprocess.ExecCommandInput{
			Command: "dokku",
			Args:    []string{"--quiet", "plugin:uninstall", pluginName},
		})
	}

	uninstallPlugin()
	defer uninstallPlugin()

	installTask := PluginTask{Name: pluginName, URL: pluginURL, Committish: pluginCommittish, State: StatePresent}
	if result := installTask.Execute(); result.Error != nil {
		t.Fatalf("failed to install plugin: %v", result.Error)
	}

	bodies, err := PluginTask{}.ExportGlobal()
	if err != nil {
		t.Fatalf("ExportGlobal: %v", err)
	}

	var found *PluginTask
	for _, b := range bodies {
		task, ok := b.(PluginTask)
		if !ok {
			t.Fatalf("ExportGlobal returned %T, want PluginTask", b)
		}
		if task.Name == pluginName {
			task := task
			found = &task
			break
		}
	}
	if found == nil {
		t.Fatalf("ExportGlobal did not include %q; got %+v", pluginName, bodies)
	}

	if found.URL != pluginURL {
		t.Errorf("exported url = %q, want %q", found.URL, pluginURL)
	}
	// The plugin was pinned to a tag (a detached checkout), so the export records
	// the exact commit rather than a branch.
	if found.Committish == "" {
		t.Errorf("expected a non-empty committish for the pinned plugin")
	}
	if found.State != StatePresent {
		t.Errorf("exported state = %q, want %q", found.State, StatePresent)
	}

	// The exported task round-trips: re-planning it against the same server is a
	// no-op because the plugin is already installed.
	plan := found.Plan()
	if plan.Error != nil {
		t.Fatalf("re-plan of exported task failed: %v", plan.Error)
	}
	if !plan.InSync {
		t.Errorf("exported task should be in sync after export, got %+v", plan)
	}
}
