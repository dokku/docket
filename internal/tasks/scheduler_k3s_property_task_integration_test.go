package tasks

import (
	"testing"
)

func TestIntegrationSchedulerK3sPropertyAll(t *testing.T) {
	skipUnlessSchedulerK3sT(t)

	appName := "docket-test-scheduler-k3s"
	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"cert-issuer-kind", "Issuer", true, true},
		{"cert-issuer-name", "acme-dns", true, true},
		{"deploy-timeout", "600s", true, true},
		{"image-pull-secrets", "secret-name", true, true},
		{"ingress-class", "traefik", false, true},
		{"kube-context", "test-ctx", false, true},
		{"kubeconfig-path", "/etc/rancher/k3s/k3s.yaml", false, true},
		{"kustomize-root-path", "config/kustomize", true, true},
		{"letsencrypt-email-prod", "admin@example.com", true, true},
		{"letsencrypt-email-stag", "staging@example.com", true, true},
		{"letsencrypt-server", "prod", true, true},
		{"namespace", "test-ns", true, true},
		{"network-interface", "eth0", false, true},
		{"node-sysctls-image", "busybox:1.36", false, true},
		{"node-sysctls-pause-image", "registry.k8s.io/pause:3.9", false, true},
		{"rollback-on-failure", "true", true, true},
		{"shm-size", "64m", true, true},
		{"token", "test-token", false, true},
	}
	for _, tc := range cases {
		if tc.perApp {
			t.Run(tc.property+"/per-app", func(t *testing.T) {
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "scheduler-k3s per-app " + tc.property,
					setTask:   SchedulerK3sPropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: SchedulerK3sPropertyTask{App: appName, Property: tc.property, State: StateAbsent},
				})
			})
		}
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := SchedulerK3sPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute(testCtx())
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "scheduler-k3s global " + tc.property,
					setTask:   SchedulerK3sPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
