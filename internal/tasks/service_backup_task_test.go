package tasks

import (
	"slices"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

func TestServiceBackupTaskInvalidState(t *testing.T) {
	task := ServiceBackupTask{Service: "postgres", Name: "my-db", Schedule: "0 3 * * *", Bucket: "b", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestServiceBackupTaskScheduleRequiresBucket(t *testing.T) {
	task := ServiceBackupTask{Service: "postgres", Name: "my-db", Schedule: "0 3 * * *"}
	result := task.Plan(testCtx())
	if result.Error == nil {
		t.Fatal("Plan with schedule and no bucket should return an error")
	}
}

func TestServiceBackupTaskBucketRequiresSchedule(t *testing.T) {
	task := ServiceBackupTask{Service: "postgres", Name: "my-db", Bucket: "my-bucket"}
	result := task.Plan(testCtx())
	if result.Error == nil {
		t.Fatal("Plan with bucket and no schedule should return an error")
	}
}

func TestServiceBackupTaskPartialAuthRejected(t *testing.T) {
	task := ServiceBackupTask{Service: "postgres", Name: "my-db", AwsAccessKeyID: "AKIA"}
	result := task.Plan(testCtx())
	if result.Error == nil {
		t.Fatal("Plan with only an access key id should return an error")
	}
}

func TestServiceBackupTaskValidateAuthChain(t *testing.T) {
	base := ServiceBackupTask{Service: "postgres", Name: "my-db", State: StatePresent, AwsAccessKeyID: "AKIA", AwsSecretAccessKey: "secret"}

	tests := []struct {
		name    string
		mutate  func(*ServiceBackupTask)
		wantErr string
	}{
		{
			name: "full chain is valid",
			mutate: func(b *ServiceBackupTask) {
				b.AwsDefaultRegion = "us-east-1"
				b.AwsSignatureVersion = "s3v4"
				b.EndpointURL = "https://s3.example.com"
			},
		},
		{
			name:   "region alone with keys is valid",
			mutate: func(b *ServiceBackupTask) { b.AwsDefaultRegion = "us-east-1" },
		},
		{
			name:    "endpoint without signature version is rejected",
			mutate:  func(b *ServiceBackupTask) { b.AwsDefaultRegion = "us-east-1"; b.EndpointURL = "https://s3.example.com" },
			wantErr: "'aws_signature_version' is required when 'endpoint_url' is set",
		},
		{
			name:    "signature version without region is rejected",
			mutate:  func(b *ServiceBackupTask) { b.AwsSignatureVersion = "s3v4" },
			wantErr: "'aws_default_region' is required when 'aws_signature_version' is set",
		},
		{
			name: "endpoint without keys is rejected",
			mutate: func(b *ServiceBackupTask) {
				b.AwsAccessKeyID = ""
				b.AwsSecretAccessKey = ""
				b.EndpointURL = "https://s3.example.com"
			},
			wantErr: "are required when 'aws_default_region', 'aws_signature_version', or 'endpoint_url' is set",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := base
			tt.mutate(&task)
			err := task.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("expected error %q, got: %v", tt.wantErr, err)
			}
		})
	}
}

func TestServiceBackupTaskPresentRequiresSomething(t *testing.T) {
	task := ServiceBackupTask{Service: "postgres", Name: "my-db"}
	result := task.Plan(testCtx())
	if result.Error == nil {
		t.Fatal("Plan with nothing to configure should return an error")
	}
}

func TestGetTasksServiceBackupTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: schedule postgres backups
      dokku_service_backup:
        service: postgres
        name: my-db
        schedule: "0 3 * * *"
        bucket: my-backup-bucket
        aws_access_key_id: AKIAEXAMPLE
        aws_secret_access_key: examplesecret
        aws_default_region: us-east-1
        encryption_passphrase: hunter2
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("schedule postgres backups")
	if task == nil {
		t.Fatal("task 'schedule postgres backups' not found")
	}

	sbTask, ok := task.(*ServiceBackupTask)
	if !ok {
		st, ok2 := task.(ServiceBackupTask)
		if !ok2 {
			t.Fatalf("task is not a ServiceBackupTask (type is %T)", task)
		}
		sbTask = &st
	}

	if sbTask.Service != "postgres" {
		t.Errorf("Service = %q, want %q", sbTask.Service, "postgres")
	}
	if sbTask.Name != "my-db" {
		t.Errorf("Name = %q, want %q", sbTask.Name, "my-db")
	}
	if sbTask.Schedule != "0 3 * * *" {
		t.Errorf("Schedule = %q, want %q", sbTask.Schedule, "0 3 * * *")
	}
	if sbTask.Bucket != "my-backup-bucket" {
		t.Errorf("Bucket = %q, want %q", sbTask.Bucket, "my-backup-bucket")
	}
	if sbTask.AwsSecretAccessKey != "examplesecret" {
		t.Errorf("AwsSecretAccessKey = %q, want %q", sbTask.AwsSecretAccessKey, "examplesecret")
	}
	if sbTask.EncryptionPassphrase != "hunter2" {
		t.Errorf("EncryptionPassphrase = %q, want %q", sbTask.EncryptionPassphrase, "hunter2")
	}
	if sbTask.State != StatePresent {
		t.Errorf("expected default state 'present', got %q", sbTask.State)
	}
}

func TestServiceBackupTaskSensitiveValuesCollected(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: schedule postgres backups
      dokku_service_backup:
        service: postgres
        name: my-db
        schedule: "0 3 * * *"
        bucket: my-backup-bucket
        aws_access_key_id: AKIAEXAMPLE
        aws_secret_access_key: topsecret
        encryption_passphrase: hunter2
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	values := CollectSensitiveValues(tasks)
	for _, want := range []string{"topsecret", "hunter2"} {
		found := false
		for _, v := range values {
			if v == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected sensitive value %q to be collected, got %v", want, values)
		}
	}
}

const (
	backupKeyID      = "AKIAEXAMPLE"
	backupSecret     = "topsecret"
	backupPassphrase = "hunter2"
)

// configuredBackupInfo is the info payload of a service whose backup settings
// match fullBackupTask exactly.
func configuredBackupInfo() map[string]string {
	return map[string]string{
		"backup-schedule":                      "0 3 * * *",
		"backup-bucket":                        "my-bucket",
		"backup-use-iam":                       "false",
		"backup-access-key-id-fingerprint":     backupFingerprint(backupKeyID),
		"backup-secret-access-key-fingerprint": backupFingerprint(backupSecret),
		"backup-default-region":                "us-east-1",
		"backup-signature-version":             "",
		"backup-endpoint-url":                  "",
		"backup-encryption-fingerprint":        backupFingerprint(backupPassphrase),
		"backup-public-key-id":                 "",
	}
}

func fullBackupTask() ServiceBackupTask {
	return ServiceBackupTask{
		Service:              "redis",
		Name:                 "cache",
		Schedule:             "0 3 * * *",
		Bucket:               "my-bucket",
		AwsAccessKeyID:       backupKeyID,
		AwsSecretAccessKey:   backupSecret,
		AwsDefaultRegion:     "us-east-1",
		EncryptionPassphrase: backupPassphrase,
		State:                StatePresent,
	}
}

// planServiceBackup plans task against a redis service named cache whose
// info reports the given payload, with the task's secrets masked as a run
// would mask them.
func planServiceBackup(t *testing.T, task ServiceBackupTask, info map[string]string) PlanResult {
	t.Helper()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(infoFixture("redis", "cache", info)))
	ctx = subprocess.ContextWithMasker(ctx, subprocess.NewMasker(sensitiveValuesFromTask(&task)...))
	plan := task.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	assertNoBackupSecrets(t, plan)
	return plan
}

// assertNoBackupSecrets fails when a secret or a fingerprint of one reaches
// the reason or a mutation line.
func assertNoBackupSecrets(t *testing.T, plan PlanResult) {
	t.Helper()
	for _, text := range append([]string{plan.Reason}, plan.Mutations...) {
		for _, secret := range []string{backupKeyID, backupSecret, backupPassphrase} {
			if strings.Contains(text, secret) || strings.Contains(text, backupFingerprint(secret)) {
				t.Errorf("a secret or its fingerprint leaked into %q", text)
			}
		}
	}
}

func TestServiceBackupTaskInSyncWhenEverythingMatches(t *testing.T) {
	t.Parallel()
	if plan := planServiceBackup(t, fullBackupTask(), configuredBackupInfo()); !plan.InSync || plan.Status != PlanStatusOK {
		t.Errorf("expected in sync, got InSync=%v status=%q reason=%q", plan.InSync, plan.Status, plan.Reason)
	}
}

func TestServiceBackupTaskPresentDrift(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		task     func(*ServiceBackupTask)
		info     func(map[string]string)
		reason   string
		mutation string
	}{
		{
			name:     "schedule",
			info:     func(i map[string]string) { i["backup-schedule"] = "0 4 * * *" },
			reason:   "backup drift on redis service cache: schedule (schedule)",
			mutation: "redis:backup-schedule cache 0 3 * * * my-bucket",
		},
		{
			name:     "bucket",
			info:     func(i map[string]string) { i["backup-bucket"] = "other-bucket" },
			reason:   "backup drift on redis service cache: schedule (bucket)",
			mutation: "redis:backup-schedule cache 0 3 * * * my-bucket",
		},
		{
			// A substring match on the crontab line used to miss this.
			name:     "use_iam",
			task:     func(t *ServiceBackupTask) { t.UseIam = true },
			reason:   "backup drift on redis service cache: schedule (use_iam)",
			mutation: "redis:backup-schedule cache 0 3 * * * my-bucket --use-iam",
		},
		{
			name:     "never scheduled",
			info:     func(i map[string]string) { i["backup-schedule"], i["backup-bucket"] = "", "" },
			reason:   "backup drift on redis service cache: schedule (schedule, bucket)",
			mutation: "redis:backup-schedule cache 0 3 * * * my-bucket",
		},
		{
			name:     "access key id",
			info:     func(i map[string]string) { i["backup-access-key-id-fingerprint"] = backupFingerprint("AKIAOTHER") },
			reason:   "backup drift on redis service cache: auth (access key id)",
			mutation: "redis:backup-auth cache (access key id drift)",
		},
		{
			name:     "secret access key",
			info:     func(i map[string]string) { i["backup-secret-access-key-fingerprint"] = backupFingerprint("rotated") },
			reason:   "backup drift on redis service cache: auth (secret access key)",
			mutation: "redis:backup-auth cache (secret access key drift)",
		},
		{
			name:     "region",
			info:     func(i map[string]string) { i["backup-default-region"] = "eu-west-1" },
			reason:   "backup drift on redis service cache: auth (region)",
			mutation: "redis:backup-auth cache (region drift)",
		},
		{
			// backup-auth clears what it is not given, so a stored endpoint the
			// recipe does not declare is drift.
			name: "undeclared endpoint",
			info: func(i map[string]string) {
				i["backup-signature-version"], i["backup-endpoint-url"] = "s3v4", "https://s3.example.com"
			},
			reason:   "backup drift on redis service cache: auth (signature version, endpoint url)",
			mutation: "redis:backup-auth cache (signature version, endpoint url drift)",
		},
		{
			name:     "encryption passphrase",
			info:     func(i map[string]string) { i["backup-encryption-fingerprint"] = backupFingerprint("old-passphrase") },
			reason:   "backup drift on redis service cache: encryption passphrase",
			mutation: "redis:backup-set-encryption cache",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			task := fullBackupTask()
			if tc.task != nil {
				tc.task(&task)
			}
			info := configuredBackupInfo()
			if tc.info != nil {
				tc.info(info)
			}
			plan := planServiceBackup(t, task, info)
			if plan.InSync || plan.Status != PlanStatusModify {
				t.Fatalf("expected a modify, got InSync=%v status=%q", plan.InSync, plan.Status)
			}
			if plan.Reason != tc.reason {
				t.Errorf("reason = %q, want %q", plan.Reason, tc.reason)
			}
			if !slices.Equal(plan.Mutations, []string{tc.mutation}) {
				t.Errorf("mutations = %v, want [%q]", plan.Mutations, tc.mutation)
			}
			for _, cmd := range plan.Commands {
				if strings.Contains(cmd, backupSecret) || strings.Contains(cmd, backupPassphrase) {
					t.Errorf("a secret leaked into command %q", cmd)
				}
			}
		})
	}
}

func TestServiceBackupTaskAbsent(t *testing.T) {
	t.Parallel()
	empty := func() map[string]string {
		info := configuredBackupInfo()
		for k := range info {
			info[k] = ""
		}
		info["backup-use-iam"] = "false"
		return info
	}
	cases := []struct {
		name     string
		info     func(map[string]string)
		commands []string
		reason   string
	}{
		{
			name: "everything configured",
			info: func(i map[string]string) {
				for k, v := range configuredBackupInfo() {
					i[k] = v
				}
			},
			commands: []string{
				"dokku --quiet redis:backup-unschedule cache",
				"dokku --quiet redis:backup-deauth cache",
				"dokku --quiet redis:backup-unset-encryption cache",
			},
			reason: "backup schedule, auth, encryption passphrase configured on redis service cache",
		},
		{
			name:     "only a schedule",
			info:     func(i map[string]string) { i["backup-schedule"], i["backup-bucket"] = "0 3 * * *", "my-bucket" },
			commands: []string{"dokku --quiet redis:backup-unschedule cache"},
			reason:   "backup schedule configured on redis service cache",
		},
		{
			// Half a credential pair reads as unauthenticated upstream, but it
			// is still stored and still removed by a deauth.
			name:     "half a credential pair",
			info:     func(i map[string]string) { i["backup-secret-access-key-fingerprint"] = backupFingerprint(backupSecret) },
			commands: []string{"dokku --quiet redis:backup-deauth cache"},
			reason:   "backup auth configured on redis service cache",
		},
		{
			name:     "a region left behind",
			info:     func(i map[string]string) { i["backup-default-region"] = "us-east-1" },
			commands: []string{"dokku --quiet redis:backup-deauth cache"},
			reason:   "backup auth configured on redis service cache",
		},
		{
			name:     "only a passphrase",
			info:     func(i map[string]string) { i["backup-encryption-fingerprint"] = backupFingerprint(backupPassphrase) },
			commands: []string{"dokku --quiet redis:backup-unset-encryption cache"},
			reason:   "backup encryption passphrase configured on redis service cache",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := empty()
			tc.info(info)
			// No secrets in the recipe: absent works from info alone.
			plan := planServiceBackup(t, ServiceBackupTask{Service: "redis", Name: "cache", State: StateAbsent}, info)
			if plan.InSync || plan.Status != PlanStatusDestroy {
				t.Fatalf("expected a destroy, got InSync=%v status=%q", plan.InSync, plan.Status)
			}
			if !slices.Equal(plan.Commands, tc.commands) {
				t.Errorf("commands = %v, want %v", plan.Commands, tc.commands)
			}
			if plan.Reason != tc.reason {
				t.Errorf("reason = %q, want %q", plan.Reason, tc.reason)
			}
		})
	}

	t.Run("leaves settings it does not own", func(t *testing.T) {
		info := empty()
		info["backup-public-key-id"] = "ABCDEF01"
		info["backup-mailto"] = "ops@example.com"
		info["backup-timestamp"] = "false"
		plan := planServiceBackup(t, ServiceBackupTask{Service: "redis", Name: "cache", State: StateAbsent}, info)
		if !plan.InSync {
			t.Errorf("expected in sync, got %+v", plan)
		}
	})

	t.Run("in sync when nothing is configured", func(t *testing.T) {
		plan := planServiceBackup(t, ServiceBackupTask{Service: "redis", Name: "cache", State: StateAbsent}, empty())
		if !plan.InSync || plan.Status != PlanStatusOK {
			t.Errorf("expected in sync, got InSync=%v status=%q", plan.InSync, plan.Status)
		}
	})
}

func TestServiceBackupTaskMissingServiceIsAnError(t *testing.T) {
	t.Parallel()
	for _, state := range []State{StatePresent, StateAbsent} {
		task := fullBackupTask()
		if state == StateAbsent {
			task = ServiceBackupTask{Service: "redis", Name: "cache", State: StateAbsent}
		}
		plan := task.Plan(subprocess.ContextWithRunner(testCtx(), serviceMissing()))
		if plan.Error == nil || !strings.Contains(plan.Error.Error(), "does not exist") {
			t.Errorf("%s: expected a missing-service error, got %v", state, plan.Error)
		}
	}
}
