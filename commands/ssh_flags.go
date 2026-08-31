package commands

import (
	"context"
	"os"

	"github.com/dokku/docket/subprocess"
)

// resolveSshFlags merges the SSH-related CLI flags with their env-var
// counterparts and returns the target for this run. Each setting takes the
// flag when it is set and the environment otherwise:
//
//   - --host over DOKKU_HOST
//   - --sudo over DOKKU_SUDO=1
//   - --accept-new-host-keys over DOKKU_SSH_ACCEPT_NEW_HOST_KEYS=1
//
// The caller puts the result on the run context with
// subprocess.ContextWithTarget, and every dokku invocation under that context
// reads it from there. Nothing is written to package state or to the process
// environment: --sudo and --accept-new-host-keys used to be bridged through
// os.Setenv so the SSH argv builder could read them back, which made them
// sticky for the life of the process and impossible to vary per call.
func resolveSshFlags(hostFlag string, sudo, acceptNewHostKeys bool) subprocess.Target {
	return subprocess.Target{
		Host:              firstNonEmpty(hostFlag, os.Getenv("DOKKU_HOST")),
		Sudo:              sudo || os.Getenv("DOKKU_SUDO") == "1",
		AcceptNewHostKeys: acceptNewHostKeys || os.Getenv("DOKKU_SSH_ACCEPT_NEW_HOST_KEYS") == "1",
	}
}

// firstNonEmpty returns the first of its arguments that is not the empty
// string, which is how a flag beats the env var it shadows.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// runContext returns the context every task in this run is planned and
// executed under. It is the process signal context when docket was started
// from main.go, so an interrupt cancels the whole run; a command built
// directly (as the tests do) gets a background context instead.
func runContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}
