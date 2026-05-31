package tasks

import (
	"testing"
)

func TestIntegrationSchedulerK3sLabelsAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-scheduler-k3s-labels"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		name         string
		global       bool
		processType  string
		resourceType string
		labels       map[string]string
	}{
		{
			name:         "per-app/default-process-type/deployment",
			resourceType: "deployment",
			labels: map[string]string{
				"app.kubernetes.io/component": "api",
				"tier":                        "edge",
			},
		},
		{
			name:         "per-app/explicit-process-type/deployment",
			processType:  "web",
			resourceType: "deployment",
			labels: map[string]string{
				"role": "frontend",
			},
		},
		{
			name:         "per-app/default-process-type/ingress",
			resourceType: "ingress",
			labels: map[string]string{
				"team": "platform",
			},
		},
		{
			name:         "global/default-process-type/deployment",
			global:       true,
			resourceType: "deployment",
			labels: map[string]string{
				"managed-by": "docket",
			},
		},
		{
			name:         "global/explicit-process-type/deployment",
			global:       true,
			processType:  "worker",
			resourceType: "deployment",
			labels: map[string]string{
				"queue": "default",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := appName
			if tc.global {
				app = ""
			}
			setTask := SchedulerK3sLabelsTask{
				App:          app,
				Global:       tc.global,
				ProcessType:  tc.processType,
				ResourceType: tc.resourceType,
				Labels:       tc.labels,
				State:        StatePresent,
			}
			unsetTask := SchedulerK3sLabelsTask{
				App:          app,
				Global:       tc.global,
				ProcessType:  tc.processType,
				ResourceType: tc.resourceType,
				Labels:       tc.labels,
				State:        StateAbsent,
			}
			defer unsetTask.Execute()
			runPropertyIdempotencyTest(t, propertyIdempotencyCase{
				label:     "scheduler-k3s labels " + tc.name,
				setTask:   setTask,
				unsetTask: unsetTask,
			})
		})
	}
}
