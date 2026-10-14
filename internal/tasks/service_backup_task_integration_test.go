package tasks

import (
	"strings"
	"testing"
)

// TestIntegrationServiceBackup drives a real backup configuration through
// auth, encryption and a schedule, a drifted region, and an absent that
// carries no secrets. None of it needs a reachable bucket: backup-auth and
// backup-schedule only store the settings and write the crontab.
func TestIntegrationServiceBackup(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "redis")

	serviceType := "redis"
	serviceName := "docket-test-backup"

	destroyService(testCtx(), serviceType, serviceName)
	t.Cleanup(func() { destroyService(testCtx(), serviceType, serviceName) })
	if r := (ServiceCreateTask{Service: serviceType, Name: serviceName, State: StatePresent}).Execute(testCtx()); r.Error != nil {
		t.Fatalf("failed to create service: %v", r.Error)
	}

	task := ServiceBackupTask{
		Service:              serviceType,
		Name:                 serviceName,
		Schedule:             "0 3 * * *",
		Bucket:               "docket-test-bucket",
		AwsAccessKeyID:       "AKIADOCKETTEST",
		AwsSecretAccessKey:   "docket-test-secret",
		AwsDefaultRegion:     "us-east-1",
		EncryptionPassphrase: "docket-test-passphrase",
		State:                StatePresent,
	}
	result := task.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to configure backups: %v\n  commands: %v\n  stderr: %s", result.Error, result.Commands, result.Stderr)
	}
	if !result.Changed {
		t.Error("expected the first apply to change the service")
	}
	if plan := task.Plan(testCtx()); !plan.InSync {
		t.Fatalf("expected the configured backups to re-plan in sync, got %+v", plan)
	}

	moved := task
	moved.AwsDefaultRegion = "eu-west-1"
	plan := moved.Plan(testCtx())
	if plan.Error != nil || plan.InSync || plan.Status != PlanStatusModify {
		t.Fatalf("a changed region must drift, got InSync=%v status=%q err=%v", plan.InSync, plan.Status, plan.Error)
	}
	if !strings.Contains(plan.Reason, "region") {
		t.Errorf("reason %q should name the region", plan.Reason)
	}

	absent := ServiceBackupTask{Service: serviceType, Name: serviceName, State: StateAbsent}
	result = absent.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to remove backups: %v\n  commands: %v\n  stderr: %s", result.Error, result.Commands, result.Stderr)
	}
	if !result.Changed {
		t.Error("expected absent to change the service")
	}
	if plan := absent.Plan(testCtx()); !plan.InSync {
		t.Errorf("expected the removed backups to re-plan in sync, got %+v", plan)
	}
	info, err := serviceInfo(testCtx(), serviceType, serviceName)
	if err != nil {
		t.Fatalf("serviceInfo: %v", err)
	}
	for _, key := range []string{"backup-schedule", "backup-access-key-id-fingerprint", "backup-secret-access-key-fingerprint", "backup-default-region", "backup-encryption-fingerprint"} {
		if v := strings.TrimSpace(info[key]); v != "" {
			t.Errorf("%s = %q after absent, want empty", key, v)
		}
	}
}
