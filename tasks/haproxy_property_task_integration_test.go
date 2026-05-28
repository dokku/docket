package tasks

import (
	"testing"
)

func TestIntegrationHaproxyPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"image", "byjg/easy-haproxy:6.0.1", false, true},
		{"letsencrypt-email", "admin@example.com", false, true},
		{"letsencrypt-server", "https://acme-staging-v02.api.letsencrypt.org/directory", false, true},
		{"log-level", "INFO", false, true},
		{"refresh-conf", "15", false, true},
	}
	for _, tc := range cases {
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := HaproxyPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute()
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "haproxy global " + tc.property,
					setTask:   HaproxyPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
