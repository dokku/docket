package tasks

import (
	"testing"
)

func TestIntegrationSchedulerK3sAutoscalingAuthAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-scheduler-k3s-autoscaling-auth"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		name     string
		global   bool
		trigger  string
		metadata map[string]string
	}{
		{
			name:    "per-app/single-metadata-key",
			trigger: "aws-secret-manager",
			metadata: map[string]string{
				"awsRegion": "us-east-1",
			},
		},
		{
			name:    "per-app/multiple-metadata-keys",
			trigger: "aws-secret-manager",
			metadata: map[string]string{
				"awsRegion":  "us-east-1",
				"secretName": "my-secret",
			},
		},
		{
			name:    "global/single-metadata-key",
			global:  true,
			trigger: "azure-keyvault",
			metadata: map[string]string{
				"vaultUri": "https://my-vault.vault.azure.net",
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := appName
			if tc.global {
				app = ""
			}
			setTask := SchedulerK3sAutoscalingAuthTask{
				App:      app,
				Global:   tc.global,
				Trigger:  tc.trigger,
				Metadata: tc.metadata,
				State:    StatePresent,
			}
			unsetTask := SchedulerK3sAutoscalingAuthTask{
				App:      app,
				Global:   tc.global,
				Trigger:  tc.trigger,
				Metadata: tc.metadata,
				State:    StateAbsent,
			}
			defer unsetTask.Execute()
			runPropertyIdempotencyTest(t, propertyIdempotencyCase{
				label:     "scheduler-k3s autoscaling-auth " + tc.name,
				setTask:   setTask,
				unsetTask: unsetTask,
			})
		})
	}
}

// TestIntegrationSchedulerK3sAutoscalingAuthPartialClear verifies the
// wipe-and-restore dance preserves keys the absent task does not name.
func TestIntegrationSchedulerK3sAutoscalingAuthPartialClear(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-scheduler-k3s-autoscaling-auth-partial"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	trigger := "aws-secret-manager"

	// Seed three keys.
	seed := SchedulerK3sAutoscalingAuthTask{
		App:     appName,
		Trigger: trigger,
		Metadata: map[string]string{
			"awsRegion":  "us-east-1",
			"secretName": "my-secret",
			"roleArn":    "arn:aws:iam::123:role/keda",
		},
		State: StatePresent,
	}
	if result := seed.Execute(); result.Error != nil {
		t.Fatalf("seed set failed: %v", result.Error)
	}

	// Clear only two of the three keys; roleArn must survive.
	clear := SchedulerK3sAutoscalingAuthTask{
		App:     appName,
		Trigger: trigger,
		Metadata: map[string]string{
			"awsRegion":  "",
			"secretName": "",
		},
		State: StateAbsent,
	}
	if result := clear.Execute(); result.Error != nil {
		t.Fatalf("partial clear failed: %v", result.Error)
	}
	if result := clear.Execute(); result.Error != nil {
		t.Fatalf("re-apply partial clear failed: %v", result.Error)
	} else if result.Changed {
		t.Errorf("re-apply partial clear should be a no-op, got Changed=true")
	}

	// Verify roleArn still exists and the other two are gone.
	current, err := getSchedulerK3sAutoscalingAuth(schedulerK3sAutoscalingAuthSpec{
		App:      appName,
		Trigger:  trigger,
		Metadata: map[string]string{},
	})
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	if got, want := current["roleArn"], "arn:aws:iam::123:role/keda"; got != want {
		t.Errorf("expected roleArn=%q to survive, got %q", want, got)
	}
	if _, ok := current["awsRegion"]; ok {
		t.Errorf("expected awsRegion to be cleared, still present")
	}
	if _, ok := current["secretName"]; ok {
		t.Errorf("expected secretName to be cleared, still present")
	}

	// Final cleanup: wipe the remaining key.
	finalClear := SchedulerK3sAutoscalingAuthTask{
		App:     appName,
		Trigger: trigger,
		Metadata: map[string]string{
			"roleArn": "",
		},
		State: StateAbsent,
	}
	finalClear.Execute()
}

// TestIntegrationSchedulerK3sAutoscalingAuthAdditivePresent verifies that
// present-state leaves extra keys (set out-of-band) untouched.
func TestIntegrationSchedulerK3sAutoscalingAuthAdditivePresent(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-scheduler-k3s-autoscaling-auth-additive"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	trigger := "aws-secret-manager"

	seed := SchedulerK3sAutoscalingAuthTask{
		App:     appName,
		Trigger: trigger,
		Metadata: map[string]string{
			"awsRegion":  "us-east-1",
			"secretName": "my-secret",
			"extraKey":   "extra-value",
		},
		State: StatePresent,
	}
	if result := seed.Execute(); result.Error != nil {
		t.Fatalf("seed set failed: %v", result.Error)
	}
	defer SchedulerK3sAutoscalingAuthTask{
		App:     appName,
		Trigger: trigger,
		Metadata: map[string]string{
			"awsRegion":  "",
			"secretName": "",
			"extraKey":   "",
		},
		State: StateAbsent,
	}.Execute()

	// Apply present with only the two original keys; extraKey should be left alone.
	apply := SchedulerK3sAutoscalingAuthTask{
		App:     appName,
		Trigger: trigger,
		Metadata: map[string]string{
			"awsRegion":  "us-east-1",
			"secretName": "my-secret",
		},
		State: StatePresent,
	}
	result := apply.Execute()
	if result.Error != nil {
		t.Fatalf("present re-apply failed: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("present re-apply with no drift should be a no-op, got Changed=true")
	}

	current, err := getSchedulerK3sAutoscalingAuth(schedulerK3sAutoscalingAuthSpec{
		App:      appName,
		Trigger:  trigger,
		Metadata: map[string]string{},
	})
	if err != nil {
		t.Fatalf("probe failed: %v", err)
	}
	if got, want := current["extraKey"], "extra-value"; got != want {
		t.Errorf("expected extraKey=%q to be preserved, got %q", want, got)
	}
}
