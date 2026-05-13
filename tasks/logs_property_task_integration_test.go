package tasks

import (
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestIntegrationLogsProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-logs-prop"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "logs per-app",
		setTask:   LogsPropertyTask{App: appName, Property: "max-size", Value: "100m", State: StatePresent},
		unsetTask: LogsPropertyTask{App: appName, Property: "max-size", State: StateAbsent},
	})
}

func TestIntegrationLogsPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := LogsPropertyTask{Global: true, Property: "max-size", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "logs global",
		setTask:   LogsPropertyTask{Global: true, Property: "max-size", Value: "100m", State: StatePresent},
		unsetTask: unsetTask,
	})
}

// TestIntegrationLogsPropertyGlobalVectorNetworks exercises the 5th resolver
// candidate (<plugin>-<group>-global-<rest>) which dokku's logs plugin uses
// for vector-image and vector-networks. Filed as dokku/dokku#8632.
func TestIntegrationLogsPropertyGlobalVectorNetworks(t *testing.T) {
	skipIfNoDokkuT(t)

	networkName := "docket-test-logs-vector-net"
	if _, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "docker",
		Args:    []string{"network", "create", networkName},
	}); err != nil {
		t.Skipf("skipping: docker network create failed: %v", err)
	}
	defer subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "docker",
		Args:    []string{"network", "rm", networkName},
	})

	unsetTask := LogsPropertyTask{Global: true, Property: "vector-networks", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "logs global vector-networks",
		setTask:   LogsPropertyTask{Global: true, Property: "vector-networks", Value: networkName, State: StatePresent},
		unsetTask: unsetTask,
	})
}
