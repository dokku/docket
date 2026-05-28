package tasks

import (
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestIntegrationLogsPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-logs-prop"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	networkName := "docket-test-logs-vector-net"
	if _, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "docker",
		Args:    []string{"network", "create", networkName},
	}); err != nil {
		t.Logf("docker network create %s failed: %v (vector-networks subtest will be skipped)", networkName, err)
	} else {
		defer subprocess.CallExecCommand(subprocess.ExecCommandInput{
			Command: "docker",
			Args:    []string{"network", "rm", networkName},
		})
	}

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"app-label-alias", "com.example.app", true, true},
		{"max-size", "5m", true, true},
		{"vector-image", "timberio/vector:1.0.0", false, true},
		{"vector-networks", networkName, false, true},
		{"vector-sink", "console://?encoding[codec]=json", true, true},
	}
	for _, tc := range cases {
		if tc.perApp {
			t.Run(tc.property+"/per-app", func(t *testing.T) {
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "logs per-app " + tc.property,
					setTask:   LogsPropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: LogsPropertyTask{App: appName, Property: tc.property, State: StateAbsent},
				})
			})
		}
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := LogsPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute()
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "logs global " + tc.property,
					setTask:   LogsPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
