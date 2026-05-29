package tasks

import (
	"testing"

	_ "github.com/gliderlabs/sigil/builtin"
)

func TestServiceBackupTaskInvalidState(t *testing.T) {
	task := ServiceBackupTask{Service: "postgres", Name: "my-db", Schedule: "0 3 * * *", Bucket: "b", State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestServiceBackupTaskScheduleRequiresBucket(t *testing.T) {
	task := ServiceBackupTask{Service: "postgres", Name: "my-db", Schedule: "0 3 * * *"}
	result := task.Plan()
	if result.Error == nil {
		t.Fatal("Plan with schedule and no bucket should return an error")
	}
}

func TestServiceBackupTaskBucketRequiresSchedule(t *testing.T) {
	task := ServiceBackupTask{Service: "postgres", Name: "my-db", Bucket: "my-bucket"}
	result := task.Plan()
	if result.Error == nil {
		t.Fatal("Plan with bucket and no schedule should return an error")
	}
}

func TestServiceBackupTaskPartialAuthRejected(t *testing.T) {
	task := ServiceBackupTask{Service: "postgres", Name: "my-db", AwsAccessKeyID: "AKIA"}
	result := task.Plan()
	if result.Error == nil {
		t.Fatal("Plan with only an access key id should return an error")
	}
}

func TestServiceBackupTaskPresentRequiresSomething(t *testing.T) {
	task := ServiceBackupTask{Service: "postgres", Name: "my-db"}
	result := task.Plan()
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
