package tasks

import (
	"testing"
)

func TestIntegrationProxyProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-proxy-prop"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "proxy per-app",
		setTask:   ProxyPropertyTask{App: appName, Property: "type", Value: "nginx", State: StatePresent},
		unsetTask: ProxyPropertyTask{App: appName, Property: "type", State: StateAbsent},
	})
}

func TestIntegrationProxyPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := ProxyPropertyTask{Global: true, Property: "type", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "proxy global",
		setTask:   ProxyPropertyTask{Global: true, Property: "type", Value: "haproxy", State: StatePresent},
		unsetTask: unsetTask,
	})
}
