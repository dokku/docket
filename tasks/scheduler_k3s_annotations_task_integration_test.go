package tasks

import (
	"testing"
)

func TestIntegrationSchedulerK3sAnnotationsAll(t *testing.T) {
	skipUnlessSchedulerK3sT(t)

	appName := "docket-test-scheduler-k3s-annotations"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		name         string
		global       bool
		processType  string
		resourceType string
		annotations  map[string]string
	}{
		{
			name:         "per-app/default-process-type/deployment",
			resourceType: "deployment",
			annotations: map[string]string{
				"prometheus.io/scrape": "true",
				"prometheus.io/port":   "9090",
			},
		},
		{
			name:         "per-app/explicit-process-type/deployment",
			processType:  "web",
			resourceType: "deployment",
			annotations: map[string]string{
				"sidecar.istio.io/inject": "false",
			},
		},
		{
			name:         "per-app/default-process-type/ingress",
			resourceType: "ingress",
			annotations: map[string]string{
				"nginx.ingress.kubernetes.io/whitelist-source-range": "10.0.0.0/8",
			},
		},
		{
			name:         "global/default-process-type/deployment",
			global:       true,
			resourceType: "deployment",
			annotations: map[string]string{
				"managed-by": "docket",
			},
		},
		{
			name:         "global/explicit-process-type/deployment",
			global:       true,
			processType:  "worker",
			resourceType: "deployment",
			annotations: map[string]string{
				"prometheus.io/scrape": "false",
			},
		},
		{
			name:         "per-app/multi-line-annotation-value",
			resourceType: "ingress",
			annotations: map[string]string{
				"nginx.ingress.kubernetes.io/configuration-snippet": "if ($host = 'old.example.com') {\n  return 301 https://new.example.com$request_uri;\n}\n",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := appName
			if tc.global {
				app = ""
			}
			setTask := SchedulerK3sAnnotationsTask{
				App:          app,
				Global:       tc.global,
				ProcessType:  tc.processType,
				ResourceType: tc.resourceType,
				Annotations:  tc.annotations,
				State:        StatePresent,
			}
			unsetTask := SchedulerK3sAnnotationsTask{
				App:          app,
				Global:       tc.global,
				ProcessType:  tc.processType,
				ResourceType: tc.resourceType,
				Annotations:  tc.annotations,
				State:        StateAbsent,
			}
			defer unsetTask.Execute()
			runPropertyIdempotencyTest(t, propertyIdempotencyCase{
				label:     "scheduler-k3s annotations " + tc.name,
				setTask:   setTask,
				unsetTask: unsetTask,
			})
		})
	}
}
