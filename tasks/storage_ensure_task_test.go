package tasks

import (
	"testing"
)

func TestStorageEnsureTaskInvalidState(t *testing.T) {
	task := StorageEnsureTask{App: "test-app", Chown: "heroku", State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestStorageEnsureValidChownValues(t *testing.T) {
	validValues := []string{"heroku", "herokuish", "paketo", "root", "false"}
	for _, chown := range validValues {
		task := StorageEnsureTask{App: "test-app", Chown: chown, State: StatePresent}
		result := task.Execute()
		// These will fail because dokku isn't running, but should NOT fail
		// due to invalid chown value
		if result.Error != nil && result.Error.Error() == "'chown' must be one of heroku, herokuish, paketo, root, false" {
			t.Errorf("chown value %q should be valid but was rejected", chown)
		}
	}
}

func TestStorageEnsureInvalidChownValue(t *testing.T) {
	task := StorageEnsureTask{App: "test-app", Chown: "packeto", State: StatePresent}
	result := task.Execute()
	if result.Error == nil || result.Error.Error() != "'chown' must be one of heroku, herokuish, paketo, root, false" {
		t.Errorf("chown value 'packeto' (misspelled) should be rejected as invalid")
	}
}

func TestStorageEnsureAbsentStateReturnsError(t *testing.T) {
	task := StorageEnsureTask{App: "test-app", Chown: "heroku", State: StateAbsent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with absent state should return an error for storage ensure")
	}
}

func TestStorageEnsureOmittedChownAllowed(t *testing.T) {
	task := StorageEnsureTask{App: "test-app", State: StatePresent}
	if err := task.Validate(); err != nil {
		t.Errorf("an omitted chown should pass validation, got: %v", err)
	}
}

func TestStorageEnsureChownCommandShape(t *testing.T) {
	task := StorageEnsureTask{App: "node-js-app", Chown: "herokuish", State: StatePresent}
	args := task.ensureArgs()
	want := []string{"--quiet", "storage:ensure-directory", "--chown", "herokuish", "node-js-app"}
	if !equalStrings(args, want) {
		t.Errorf("ensureArgs mismatch:\n  got: %v\n want: %v", args, want)
	}
}

func TestStorageEnsureOmittedChownCommandShape(t *testing.T) {
	task := StorageEnsureTask{App: "node-js-app", State: StatePresent}
	args := task.ensureArgs()
	want := []string{"--quiet", "storage:ensure-directory", "node-js-app"}
	if !equalStrings(args, want) {
		t.Errorf("ensureArgs mismatch:\n  got: %v\n want: %v", args, want)
	}
	for _, a := range args {
		if a == "--chown" {
			t.Errorf("omitted chown must not emit --chown flag: %v", args)
		}
	}
}
