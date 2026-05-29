package tasks

import (
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
