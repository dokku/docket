package commands

import (
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

// sshEnv turns a map into the lookup resolveSshFlags takes. It replaces a
// clearSshEnv helper that wiped the three variables out of the process
// environment with t.Setenv so a developer's exported DOKKU_HOST could not
// reach the assertions - which made every test here serial, since t.Setenv
// panics in a parallel test. A stub answers the same need and does not touch
// the process at all.
func sshEnv(vars map[string]string) func(string) string {
	return func(key string) string { return vars[key] }
}

func TestResolveSshFlagsDefaultsToLocal(t *testing.T) {
	t.Parallel()
	got := mustResolveSshFlags(t, sshEnv(nil), "", false, false, "", nil)
	if got.Host != "" || got.Sudo || got.AcceptNewHostKeys || got.IdentityFile != "" || got.SSHOptions != nil {
		t.Errorf("resolveSshFlags = %+v, want the zero Target", got)
	}
}

// TestResolveSshFlagsReadsTheEnvironment pins that the three env vars work on
// their own, with no flag. They are documented equivalents of the flags, and
// they used to work by a different route: the SSH argv builder read
// DOKKU_SUDO and DOKKU_SSH_ACCEPT_NEW_HOST_KEYS directly out of the process
// environment. Now that resolveSshFlags is the only reader, forgetting one
// here is how they would silently stop working.
func TestResolveSshFlagsReadsTheEnvironment(t *testing.T) {
	t.Parallel()
	env := sshEnv(map[string]string{
		"DOKKU_HOST":                     "deploy@dokku.example.com",
		"DOKKU_SUDO":                     "1",
		"DOKKU_SSH_ACCEPT_NEW_HOST_KEYS": "1",
	})

	got := mustResolveSshFlags(t, env, "", false, false, "", nil)
	if got.Host != "deploy@dokku.example.com" {
		t.Errorf("Host = %q, want it read from DOKKU_HOST", got.Host)
	}
	if !got.Sudo {
		t.Error("Sudo should be set by DOKKU_SUDO=1 with no --sudo flag")
	}
	if !got.AcceptNewHostKeys {
		t.Error("AcceptNewHostKeys should be set by DOKKU_SSH_ACCEPT_NEW_HOST_KEYS=1 with no flag")
	}
}

// TestResolveSshFlagsOnlyHonorsTheDocumentedEnvValue pins that the two boolean
// env vars mean "1", not "any value". `DOKKU_SUDO=0` reads as off.
func TestResolveSshFlagsOnlyHonorsTheDocumentedEnvValue(t *testing.T) {
	t.Parallel()
	env := sshEnv(map[string]string{
		"DOKKU_SUDO":                     "0",
		"DOKKU_SSH_ACCEPT_NEW_HOST_KEYS": "false",
	})

	got := mustResolveSshFlags(t, env, "", false, false, "", nil)
	if got.Sudo {
		t.Error("DOKKU_SUDO=0 must not enable sudo")
	}
	if got.AcceptNewHostKeys {
		t.Error("DOKKU_SSH_ACCEPT_NEW_HOST_KEYS=false must not enable accept-new")
	}
}

func TestResolveSshFlagsFlagWinsOverEnv(t *testing.T) {
	t.Parallel()
	env := sshEnv(map[string]string{"DOKKU_HOST": "from-env"})

	got := mustResolveSshFlags(t, env, "from-flag", true, true, "", nil)
	if got.Host != "from-flag" {
		t.Errorf("Host = %q, want --host to win over DOKKU_HOST", got.Host)
	}
	if !got.Sudo || !got.AcceptNewHostKeys {
		t.Errorf("flags alone should set the booleans; got %+v", got)
	}
}

// TestResolveSshFlagsDoesNotMutateProcessEnv is the lock on removing the
// os.Setenv bridge. --sudo and --accept-new-host-keys used to be written into
// the process environment so the SSH argv builder could read them back, which
// meant one invocation's flags applied to every later one in the same process
// - the concrete reason these settings could not be per-invocation.
//
// It compares a snapshot rather than requiring the three variables be unset,
// so it no longer has to clear them first.
//
// The snapshot is sorted because os.Environ()'s order is not stable across a
// Setenv/Unsetenv round trip - restoring a variable appends it rather than
// putting it back in its old slot - so an unsorted compare fails on a
// reordering no one performed.
func TestResolveSshFlagsDoesNotMutateProcessEnv(t *testing.T) {
	t.Parallel()
	before := sortedEnv()

	mustResolveSshFlags(t, os.Getenv, "deploy@dokku.example.com", true, true, "/keys/deploy", []string{"ConnectTimeout=5"})

	if after := sortedEnv(); after != before {
		t.Error("resolving flags must not write the process environment")
	}
}

func sortedEnv() string {
	env := os.Environ()
	sort.Strings(env)
	return strings.Join(env, "\n")
}

// mustResolveSshFlags is resolveSshFlags for the cases that expect no error.
func mustResolveSshFlags(t *testing.T, getenv func(string) string, host string, sudo, acceptNewHostKeys bool, identityFile string, sshOptions []string) subprocess.Target {
	t.Helper()
	got, err := resolveSshFlags(getenv, host, sudo, acceptNewHostKeys, identityFile, sshOptions)
	if err != nil {
		t.Fatalf("resolveSshFlags: %v", err)
	}
	return got
}

func TestResolveSshFlagsIdentityFile(t *testing.T) {
	t.Parallel()
	env := sshEnv(map[string]string{"DOKKU_SSH_IDENTITY_FILE": "/keys/from-env"})

	if got := mustResolveSshFlags(t, env, "", false, false, "", nil); got.IdentityFile != "/keys/from-env" {
		t.Errorf("IdentityFile = %q, want it read from DOKKU_SSH_IDENTITY_FILE", got.IdentityFile)
	}
	if got := mustResolveSshFlags(t, env, "", false, false, "/keys/from-flag", nil); got.IdentityFile != "/keys/from-flag" {
		t.Errorf("IdentityFile = %q, want --ssh-identity-file to win over DOKKU_SSH_IDENTITY_FILE", got.IdentityFile)
	}
}

func TestResolveSshFlagsPassesOptionsThrough(t *testing.T) {
	t.Parallel()
	opts := []string{"ConnectTimeout=5", "ProxyJump bastion"}
	got := mustResolveSshFlags(t, sshEnv(nil), "deploy@dokku.example.com", false, false, "", opts)
	if !reflect.DeepEqual(got.SSHOptions, opts) {
		t.Errorf("SSHOptions = %q, want %q", got.SSHOptions, opts)
	}
}

// TestResolveSshFlagsRefusesAManagedOption pins that a bad --ssh-option fails
// while the flags are resolved, before anything connects, and that the error
// names the flag it came from.
func TestResolveSshFlagsRefusesAManagedOption(t *testing.T) {
	t.Parallel()
	_, err := resolveSshFlags(sshEnv(nil), "deploy@dokku.example.com", false, false, "", []string{"ControlPath=/tmp/x"})
	if err == nil {
		t.Fatal("resolveSshFlags accepted ControlPath, want an error")
	}
	if !strings.Contains(err.Error(), "--ssh-option") {
		t.Errorf("error = %q, want it to name --ssh-option", err)
	}
}
