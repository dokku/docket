package tasks

import (
	"testing"
)

func TestIntegrationRegistryPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-registry"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"image-repo", "dokku/test-app", true, false},
		{"image-repo-template", "dokku/{{.APP}}-prod", true, true},
		{"push-extra-tags", "v1,latest", true, true},
		{"push-on-release", "true", true, true},
		{"server", "ghcr.io", true, true},
	}
	for _, tc := range cases {
		if tc.perApp {
			t.Run(tc.property+"/per-app", func(t *testing.T) {
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "registry per-app " + tc.property,
					setTask:   RegistryPropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: RegistryPropertyTask{App: appName, Property: tc.property, State: StateAbsent},
				})
			})
		}
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := RegistryPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute()
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "registry global " + tc.property,
					setTask:   RegistryPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
