package tasks

import (
	"testing"
)

func TestIntegrationSchedulerK3sChartAll(t *testing.T) {
	skipUnlessSchedulerK3sT(t)

	cases := []struct {
		name   string
		chart  string
		values map[string]any
	}{
		{
			name:  "flat-dotted-values",
			chart: "ingress-nginx",
			values: map[string]any{
				"controller.replicaCount":         "1",
				"controller.resources.limits.cpu": "100m",
			},
		},
		{
			name:  "nested-values",
			chart: "ingress-nginx",
			values: map[string]any{
				"controller": map[string]any{
					"replicaCount": "2",
				},
			},
		},
		{
			name:  "nested-with-dotted-leaf-escapes",
			chart: "traefik",
			values: map[string]any{
				"service": map[string]any{
					"annotations": map[string]any{
						"prometheus.io/scrape": "true",
					},
				},
			},
		},
		{
			name:  "multi-line-value",
			chart: "traefik",
			values: map[string]any{
				"controller.config": "line one\nline two\nline three\n",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setTask := SchedulerK3sChartTask{
				Chart:  tc.chart,
				Values: tc.values,
				State:  StatePresent,
			}
			unsetTask := SchedulerK3sChartTask{
				Chart:  tc.chart,
				Values: tc.values,
				State:  StateAbsent,
			}
			defer unsetTask.Execute()
			runPropertyIdempotencyTest(t, propertyIdempotencyCase{
				label:     "scheduler-k3s chart " + tc.name,
				setTask:   setTask,
				unsetTask: unsetTask,
			})
		})
	}
}
