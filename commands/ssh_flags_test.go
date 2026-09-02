package commands

import (
	"os"
	"sort"
	"strings"
	"testing"
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
	got := resolveSshFlags(sshEnv(nil), "", false, false)
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
	t.Parallel()
	env := sshEnv(map[string]string{
		"DOKKU_HOST":                     "deploy@dokku.example.com",
		"DOKKU_SUDO":                     "1",
		"DOKKU_SSH_ACCEPT_NEW_HOST_KEYS": "1",
	})

	got := resolveSshFlags(env, "", false, false)
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

	got := resolveSshFlags(env, "", false, false)
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

	got := resolveSshFlags(env, "from-flag", true, true)
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
// Serial on purpose, and not because of t.Setenv - it sets nothing. Rendering
// a recipe goes through sigil, which exports the template variables into the
// process environment and restores it with os.Clearenv on the way out, so for
// a moment during any render the environment is empty. tasks.RenderTemplate
// serializes renders against each other, which is what stops variables being
// lost for good, but it cannot stop a concurrent *reader* seeing the gap. A
// whole-environment assertion is therefore only meaningful while nothing else
// is running, which the serial phase guarantees.
//
// The snapshot is sorted because os.Environ()'s order is not stable across a
// Setenv/Unsetenv round trip - restoring a variable appends it rather than
// putting it back in its old slot - so an unsorted compare fails on a
// reordering no one performed.
func TestResolveSshFlagsDoesNotMutateProcessEnv(t *testing.T) {
	before := sortedEnv()

	resolveSshFlags(os.Getenv, "deploy@dokku.example.com", true, true)

	if after := sortedEnv(); after != before {
		t.Error("resolving flags must not write the process environment")
	}
}

func sortedEnv() string {
	env := os.Environ()
	sort.Strings(env)
	return strings.Join(env, "\n")
}
