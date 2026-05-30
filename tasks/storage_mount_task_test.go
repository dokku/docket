package tasks

import (
	"strings"
	"testing"
)

func TestStorageMountTaskInvalidState(t *testing.T) {
	task := StorageMountTask{
		App:          "test-app",
		HostDir:      "/host",
		ContainerDir: "/container",
		State:        "invalid",
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestStorageMountRequiresExactlyOneSource(t *testing.T) {
	cases := []struct {
		name string
		task StorageMountTask
		want string
	}{
		{
			name: "neither set",
			task: StorageMountTask{App: "test-app", ContainerDir: "/c", State: StatePresent},
			want: "exactly one of 'entry_name' or 'host_dir' is required",
		},
		{
			name: "both set",
			task: StorageMountTask{App: "test-app", EntryName: "e", HostDir: "/h", ContainerDir: "/c", State: StatePresent},
			want: "'entry_name' and 'host_dir' are mutually exclusive",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.task.Plan()
			if result.Error == nil {
				t.Fatalf("expected error %q, got nil", tc.want)
			}
			if !strings.Contains(result.Error.Error(), tc.want) {
				t.Errorf("expected error to contain %q, got %q", tc.want, result.Error.Error())
			}
		})
	}
}

func TestStorageMountRejectsInvalidPhase(t *testing.T) {
	task := StorageMountTask{
		App:          "test-app",
		EntryName:    "data",
		ContainerDir: "/app/storage",
		Phases:       []string{"deploy", "boot"},
		State:        StatePresent,
	}
	result := task.Plan()
	if result.Error == nil {
		t.Fatal("expected error for invalid phase")
	}
	if !strings.Contains(result.Error.Error(), `invalid phase "boot"`) {
		t.Errorf("expected invalid phase error, got %q", result.Error.Error())
	}
}

func TestStorageMountNamedEntryCommandShape(t *testing.T) {
	task := StorageMountTask{
		App:          "test-app",
		EntryName:    "data",
		ContainerDir: "/app/storage",
		Phases:       []string{"deploy", "run"},
		ProcessType:  "web",
		Subpath:      "sub",
		Readonly:     true,
		VolumeChown:  "herokuish",
	}
	args := task.mountArgs()
	want := []string{
		"--quiet", "storage:mount", "test-app", "data",
		"--container-dir", "/app/storage",
		"--phase", "deploy", "--phase", "run",
		"--process-type", "web",
		"--volume-subpath", "sub",
		"--volume-readonly",
		"--volume-chown", "herokuish",
	}
	if !equalStrings(args, want) {
		t.Errorf("mountArgs mismatch:\n  got: %v\n want: %v", args, want)
	}
	unmount := task.unmountArgs()
	wantUnmount := []string{"--quiet", "storage:unmount", "test-app", "data", "--container-dir", "/app/storage"}
	if !equalStrings(unmount, wantUnmount) {
		t.Errorf("unmountArgs mismatch:\n  got: %v\n want: %v", unmount, wantUnmount)
	}
}

func TestStorageMountLegacyCommandShape(t *testing.T) {
	task := StorageMountTask{
		App:          "test-app",
		HostDir:      "/var/data",
		ContainerDir: "/app/storage",
	}
	args := task.mountArgs()
	want := []string{"--quiet", "storage:mount", "test-app", "/var/data:/app/storage"}
	if !equalStrings(args, want) {
		t.Errorf("legacy mountArgs mismatch:\n  got: %v\n want: %v", args, want)
	}
	unmount := task.unmountArgs()
	wantUnmount := []string{"--quiet", "storage:unmount", "test-app", "/var/data:/app/storage"}
	if !equalStrings(unmount, wantUnmount) {
		t.Errorf("legacy unmountArgs mismatch:\n  got: %v\n want: %v", unmount, wantUnmount)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
