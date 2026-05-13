package tasks

import (
	"testing"
)

func TestIntegrationNginxProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-nginx"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "nginx per-app",
		setTask:   NginxPropertyTask{App: appName, Property: "proxy-read-timeout", Value: "120s", State: StatePresent},
		unsetTask: NginxPropertyTask{App: appName, Property: "proxy-read-timeout", State: StateAbsent},
	})
}

func TestIntegrationNginxPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := NginxPropertyTask{Global: true, Property: "bind-address-ipv4", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "nginx global",
		setTask:   NginxPropertyTask{Global: true, Property: "bind-address-ipv4", Value: "0.0.0.0", State: StatePresent},
		unsetTask: unsetTask,
	})
}
