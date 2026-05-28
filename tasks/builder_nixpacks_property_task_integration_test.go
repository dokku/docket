package tasks

import (
	"testing"
)

func TestIntegrationBuilderNixpacksPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-builder-nixpacks"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"nixpackstoml-path", "config/nixpacks.toml", true, true},
	}
	for _, tc := range cases {
		if tc.perApp {
			t.Run(tc.property+"/per-app", func(t *testing.T) {
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "builder-nixpacks per-app " + tc.property,
					setTask:   BuilderNixpacksPropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: BuilderNixpacksPropertyTask{App: appName, Property: tc.property, State: StateAbsent},
				})
			})
		}
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := BuilderNixpacksPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute()
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "builder-nixpacks global " + tc.property,
					setTask:   BuilderNixpacksPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
