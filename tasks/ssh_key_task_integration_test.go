package tasks

import (
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestIntegrationSshKey(t *testing.T) {
	skipIfNoDokkuT(t)

	keyName := "docket-test-ssh-key"

	removeSSHKey := func() {
		subprocess.CallExecCommand(subprocess.ExecCommandInput{
			Command: "dokku",
			Args:    []string{"--quiet", "ssh-keys:remove", keyName},
		})
	}

	removeSSHKey()
	defer removeSSHKey()

	assertPresent := func(t *testing.T, label string, want bool) {
		t.Helper()
		keys, err := sshKeysList()
		if err != nil {
			t.Fatalf("%s: sshKeysList failed: %v", label, err)
		}
		if _, found := keys[keyName]; found != want {
			t.Errorf("%s: key present = %v, want %v", label, found, want)
		}
	}

	assertPresent(t, "initial", false)

	// add the key
	addTask := SshKeyTask{Name: keyName, Key: testSSHKey1, State: StatePresent}
	result := addTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to add key: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first add")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	assertPresent(t, "after add", true)

	// add the same key again - idempotent
	result = addTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second add: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent add")
	}

	// rotate to a different key under the same name
	rotateTask := SshKeyTask{Name: keyName, Key: testSSHKey2, State: StatePresent}
	result = rotateTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to rotate key: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on rotation")
	}
	assertPresent(t, "after rotate", true)

	// rotation is now idempotent
	result = rotateTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second rotate: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent rotation")
	}

	// remove the key
	removeTask := SshKeyTask{Name: keyName, State: StateAbsent}
	result = removeTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to remove key: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first remove")
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	assertPresent(t, "after remove", false)

	// remove again - idempotent
	result = removeTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second remove: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent remove")
	}
}
