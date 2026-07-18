package tasks

import (
	"strings"
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
	// The named ownership presets plus a raw numeric uid in dokku's supported
	// range (0-65535) all pass validation, matching dokku's ResolveChownID.
	validValues := []string{"heroku", "herokuish", "paketo", "root", "false", "0", "1000", "32767", "65535"}
	for _, chown := range validValues {
		task := StorageEnsureTask{App: "test-app", Chown: chown, State: StatePresent}
		if err := task.Validate(); err != nil {
			t.Errorf("chown value %q should be valid but was rejected: %v", chown, err)
		}
	}
}

func TestStorageEnsureInvalidChownValue(t *testing.T) {
	// Non-preset, non-numeric values and out-of-range/malformed numbers are
	// rejected before dispatch. 'packeto' is dokku's deprecated typo alias and
	// is intentionally still rejected. 0x10/1_000 confirm the raw string (not a
	// resolved integer) is validated with dokku's base-10 parser.
	invalidValues := []string{"packeto", "65536", "70000", "-1", "+5", "1000:1000", "root:root", "0x10", "1_000", "abc"}
	for _, chown := range invalidValues {
		task := StorageEnsureTask{App: "test-app", Chown: chown, State: StatePresent}
		err := task.Validate()
		if err == nil {
			t.Errorf("chown value %q should be rejected as invalid", chown)
			continue
		}
		if !strings.Contains(err.Error(), "'chown' must be one of") {
			t.Errorf("chown value %q: unexpected error message: %v", chown, err)
		}
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

func TestStorageEnsureNumericChownCommandShape(t *testing.T) {
	task := StorageEnsureTask{App: "node-js-app", Chown: "1000", State: StatePresent}
	args := task.ensureArgs()
	want := []string{"--quiet", "storage:ensure-directory", "--chown", "1000", "node-js-app"}
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
