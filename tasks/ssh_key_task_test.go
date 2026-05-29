package tasks

import (
	"testing"

	"golang.org/x/crypto/ssh"
)

// Test fixtures: two distinct ed25519 public keys and their SHA256
// fingerprints (as emitted by `ssh-keygen -lf`). Shared with the integration
// test.
const (
	testSSHKey1 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAINioKrRalhe/VF8s43pjp8jpl6LGwv6tF0F5FvKPjUer docket-test-key-1"
	testSSHKey2 = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIJdrDTcBfAuO6HLQwr43xeidcwfLDiKNUDcxEMxqo0tx docket-test-key-2"

	testSSHKey1Fingerprint = "SHA256:LstajP5Ikfsl+VCQw4ZtoPvox4TMWs+3LIo11pXEr4o"
	testSSHKey2Fingerprint = "SHA256:SgMboxwxpDfXLgOoyoyD/nAgTlCzvcZMMS3r+IyKi1o"
)

func TestSshKeyTaskInvalidState(t *testing.T) {
	task := SshKeyTask{Name: "deploy-bot", Key: testSSHKey1, State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestSshKeyTaskRequiresName(t *testing.T) {
	result := SshKeyTask{Key: testSSHKey1, State: StatePresent}.Plan()
	if result.Error == nil {
		t.Fatal("Plan without 'name' should return an error")
	}
}

func TestSshKeyTaskRequiresKeyWhenPresent(t *testing.T) {
	result := SshKeyTask{Name: "deploy-bot", State: StatePresent}.Plan()
	if result.Error == nil {
		t.Fatal("Plan with state 'present' and no 'key' should return an error")
	}
}

func TestSshKeyTaskInvalidKey(t *testing.T) {
	result := SshKeyTask{Name: "deploy-bot", Key: "not-a-key", State: StatePresent}.Plan()
	if result.Error == nil {
		t.Fatal("Plan with an unparseable key should return an error")
	}
}

func TestSshKeyMatches(t *testing.T) {
	pub, _, _, _, err := ssh.ParseAuthorizedKey([]byte(testSSHKey1))
	if err != nil {
		t.Fatalf("failed to parse fixture key: %v", err)
	}

	// Sanity-check the fixture fingerprint against the crypto/ssh computation
	// so the comparison the task relies on stays correct.
	if got := ssh.FingerprintSHA256(pub); got != testSSHKey1Fingerprint {
		t.Fatalf("fixture fingerprint mismatch: got %q, want %q", got, testSSHKey1Fingerprint)
	}

	cases := []struct {
		name  string
		entry sshKeyEntry
		want  bool
	}{
		{"matching fingerprint only", sshKeyEntry{Fingerprint: testSSHKey1Fingerprint}, true},
		{"different fingerprint only", sshKeyEntry{Fingerprint: testSSHKey2Fingerprint}, false},
		{"matching raw key, different comment", sshKeyEntry{PublicKey: "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAINioKrRalhe/VF8s43pjp8jpl6LGwv6tF0F5FvKPjUer other-comment"}, true},
		{"different raw key", sshKeyEntry{PublicKey: testSSHKey2}, false},
		{"empty entry", sshKeyEntry{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sshKeyMatches(tc.entry, pub); got != tc.want {
				t.Errorf("sshKeyMatches = %v, want %v", got, tc.want)
			}
		})
	}
}
