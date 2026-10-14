package tasks

import (
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

// TestIntegrationDeclaredVersions checks the floors against the Dokku this
// process can run. A declaration newer than that Dokku, or than an installed
// plugin, fails CI. A floor set older than the release that introduced the
// command still passes: this test does not prove a floor is the oldest
// release that works.
func TestIntegrationDeclaredVersions(t *testing.T) {
	skipIfNoDokkuT(t)

	version, err := subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"version"},
	})
	if err != nil {
		t.Fatalf("dokku version: %v", err)
	}
	have, err := parseVersion(version.StdoutContents())
	if err != nil {
		t.Fatalf("parsing dokku version %q: %v", version.StdoutContents(), err)
	}

	listed, err := subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "plugin:list", "--format", "json"},
	})
	if err != nil {
		t.Fatalf("plugin:list: %v", err)
	}
	plugins := parsePluginList(listed.StdoutContents())

	for name, task := range allRegisteredTasks() {
		support, ok := TaskMinimumVersions(task)
		if !ok {
			t.Errorf("%s declares no Dokku floor", name)
			continue
		}
		need, err := parseVersion(support.Dokku)
		if err != nil {
			t.Errorf("%s declares Dokku %q: %v", name, support.Dokku, err)
			continue
		}
		if have.olderThan(need) {
			t.Errorf("%s requires Dokku >= %s, this server has %s", name, need, have)
		}
		for _, plugin := range support.Plugins {
			if plugin.Name == "" {
				continue
			}
			reported, installed := plugins[plugin.Name]
			if !installed {
				continue
			}
			got, err := parseVersion(reported)
			if err != nil {
				t.Errorf("%s: installed %s version %q: %v", name, plugin.Name, reported, err)
				continue
			}
			needPlugin, err := parseVersion(plugin.Minimum)
			if err != nil {
				t.Errorf("%s declares %s %q: %v", name, plugin.Name, plugin.Minimum, err)
				continue
			}
			if got.olderThan(needPlugin) {
				t.Errorf("%s requires %s >= %s, this server has %s", name, plugin.Name, needPlugin, got)
			}
		}
	}
}
