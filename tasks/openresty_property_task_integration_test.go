package tasks

import (
	"testing"
)

func TestIntegrationOpenrestyProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-openresty"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	// Per dokku/dokku#8612 (v0.38.4), openresty:set <app> rejects every
	// global-only property. bind-address-ipv4 is the only legitimate per-app
	// property and exposes a raw key (empty after unset) post-fix.
	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "openresty per-app",
		setTask:   OpenrestyPropertyTask{App: appName, Property: "bind-address-ipv4", Value: "1.2.3.4", State: StatePresent},
		unsetTask: OpenrestyPropertyTask{App: appName, Property: "bind-address-ipv4", State: StateAbsent},
	})
}

func TestIntegrationOpenrestyPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := OpenrestyPropertyTask{Global: true, Property: "bind-address-ipv4", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "openresty global",
		setTask:   OpenrestyPropertyTask{Global: true, Property: "bind-address-ipv4", Value: "0.0.0.0", State: StatePresent},
		unsetTask: unsetTask,
	})
}
