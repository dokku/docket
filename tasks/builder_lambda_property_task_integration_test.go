package tasks

import (
	"testing"
)

func TestIntegrationBuilderLambdaPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-builder-lambda"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"lambdayml-path", "config/lambda.yml", true, true},
	}
	for _, tc := range cases {
		if tc.perApp {
			t.Run(tc.property+"/per-app", func(t *testing.T) {
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "builder-lambda per-app " + tc.property,
					setTask:   BuilderLambdaPropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: BuilderLambdaPropertyTask{App: appName, Property: tc.property, State: StateAbsent},
				})
			})
		}
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := BuilderLambdaPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute()
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "builder-lambda global " + tc.property,
					setTask:   BuilderLambdaPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
