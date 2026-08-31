package commands

import (
	"os"
	"testing"
)

// clearSshEnv removes the three env vars resolveSshFlags reads so a test
// starts from a known state whatever the developer's shell exports.
func clearSshEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"DOKKU_HOST", "DOKKU_SUDO", "DOKKU_SSH_ACCEPT_NEW_HOST_KEYS"} {
		t.Setenv(name, "")
		os.Unsetenv(name)
	}
}

func TestResolveSshFlagsDefaultsToLocal(t *testing.T) {
	clearSshEnv(t)

	got := resolveSshFlags("", false, false)
	if got.Host != "" || got.Sudo || got.AcceptNewHostKeys {
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
	clearSshEnv(t)
	t.Setenv("DOKKU_HOST", "deploy@dokku.example.com")
	t.Setenv("DOKKU_SUDO", "1")
	t.Setenv("DOKKU_SSH_ACCEPT_NEW_HOST_KEYS", "1")

	got := resolveSshFlags("", false, false)
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
	clearSshEnv(t)
	t.Setenv("DOKKU_SUDO", "0")
	t.Setenv("DOKKU_SSH_ACCEPT_NEW_HOST_KEYS", "false")

	got := resolveSshFlags("", false, false)
	if got.Sudo {
		t.Error("DOKKU_SUDO=0 must not enable sudo")
	}
	if got.AcceptNewHostKeys {
		t.Error("DOKKU_SSH_ACCEPT_NEW_HOST_KEYS=false must not enable accept-new")
	}
}

func TestResolveSshFlagsFlagWinsOverEnv(t *testing.T) {
	clearSshEnv(t)
	t.Setenv("DOKKU_HOST", "from-env")

	got := resolveSshFlags("from-flag", true, true)
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
func TestResolveSshFlagsDoesNotMutateProcessEnv(t *testing.T) {
	clearSshEnv(t)

	resolveSshFlags("deploy@dokku.example.com", true, true)

	for _, name := range []string{"DOKKU_HOST", "DOKKU_SUDO", "DOKKU_SSH_ACCEPT_NEW_HOST_KEYS"} {
		if v, ok := os.LookupEnv(name); ok && v != "" {
			t.Errorf("%s = %q; resolving flags must not write the process environment", name, v)
		}
	}
}
