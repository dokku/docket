package tasks

import (
	"testing"
)

func TestIntegrationBuilderLambdaProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-builder-lambda"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "builder-lambda per-app",
		setTask:   BuilderLambdaPropertyTask{App: appName, Property: "lambdayml-path", Value: "config/lambda.yml", State: StatePresent},
		unsetTask: BuilderLambdaPropertyTask{App: appName, Property: "lambdayml-path", State: StateAbsent},
	})
}

func TestIntegrationBuilderLambdaPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := BuilderLambdaPropertyTask{Global: true, Property: "lambdayml-path", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "builder-lambda global",
		setTask:   BuilderLambdaPropertyTask{Global: true, Property: "lambdayml-path", Value: "config/lambda.yml", State: StatePresent},
		unsetTask: unsetTask,
	})
}
