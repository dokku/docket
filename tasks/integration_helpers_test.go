package tasks

import (
	"github.com/dokku/docket/subprocess"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}

func dokkuAvailable() bool {
	_, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"version"},
	})
	return err == nil
}

func skipIfNoDokkuT(t *testing.T) {
	t.Helper()
	skipIfNotInShardT(t)
	if !dokkuAvailable() {
		t.Skip("skipping integration test: dokku not available")
	}
}

func dokkuPluginInstalled(plugin string) bool {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"plugin:list"},
	})
	if err != nil {
		return false
	}

	for _, line := range strings.Split(result.StdoutContents(), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == plugin {
			return true
		}
	}
	return false
}

func skipIfPluginMissingT(t *testing.T, plugin string) {
	t.Helper()
	if !dokkuPluginInstalled(plugin) {
		t.Skipf("skipping integration test: dokku plugin %q not installed", plugin)
	}
}

func dockerLinkSupported() bool {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "docker",
		Args:    []string{"version", "--format", "{{.Server.Version}}"},
	})
	if err != nil {
		return false
	}

	version := strings.TrimSpace(result.StdoutContents())
	parts := strings.SplitN(version, ".", 2)
	if len(parts) == 0 {
		return false
	}

	major, err := strconv.Atoi(parts[0])
	if err != nil {
		return false
	}

	// Docker < 29 supports --link natively
	if major < 29 {
		return true
	}

	// Docker >= 29 requires DOCKER_KEEP_DEPRECATED_LEGACY_LINKS_ENV_VARS=1
	// on the daemon. Test by creating two containers with --link and checking
	// if the link env vars are present.
	subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "docker",
		Args:    []string{"rm", "-f", "docket-link-test-target", "docket-link-test-client"},
	})

	_, err = subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "docker",
		Args:    []string{"run", "-d", "--name", "docket-link-test-target", "alpine", "sleep", "30"},
	})
	if err != nil {
		return false
	}
	defer subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "docker",
		Args:    []string{"rm", "-f", "docket-link-test-target", "docket-link-test-client"},
	})

	result, err = subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "docker",
		Args:    []string{"run", "--rm", "--name", "docket-link-test-client", "--link", "docket-link-test-target:target", "alpine", "env"},
	})
	if err != nil {
		return false
	}

	return strings.Contains(result.StdoutContents(), "TARGET_NAME=")
}

func skipIfDockerLinkUnsupportedT(t *testing.T) {
	t.Helper()
	if !dockerLinkSupported() {
		t.Skip("skipping integration test: docker does not support legacy container links")
	}
}

// getCurrentContainerIDs reads the container IDs from dokku's internal
// CONTAINER files (e.g., /home/dokku/APP/CONTAINER.web.1) which are the
// authoritative source for the current deployment's containers.
func getCurrentContainerIDs(appName, processType string) ([]string, error) {
	scale, err := getPsScale(appName)
	if err != nil {
		return nil, err
	}
	count, ok := scale[processType]
	if !ok || count == 0 {
		return nil, nil
	}
	var ids []string
	for i := 1; i <= count; i++ {
		result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
			Command: "cat",
			Args:    []string{fmt.Sprintf("/home/dokku/%s/CONTAINER.%s.%d", appName, processType, i)},
		})
		if err != nil {
			continue
		}
		id := strings.TrimSpace(result.StdoutContents())
		if id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}
