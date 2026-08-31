package commands

import (
	"context"
	"os"

	"github.com/dokku/docket/subprocess"
)

// resolveSshFlags merges the apply/plan SSH-related CLI flags with their
// env-var counterparts and applies the result to package state for the
// rest of the run:
//
//   - --host wins over DOKKU_HOST; the resolved value is stored on the
//     subprocess package via SetDefaultHost so the dispatcher can read
//     it from ExecCommandInput.Host without consulting the environment.
//   - --sudo and --accept-new-host-keys are bridged to the env vars
//     subprocess/ssh.go reads at argv-build time (DOKKU_SUDO,
//     DOKKU_SSH_ACCEPT_NEW_HOST_KEYS).
//
// Returns the resolved host so callers can use it for play-header
// rendering and ControlMaster teardown without re-reading env state.
func resolveSshFlags(hostFlag string, sudo, acceptNewHostKeys bool) string {
	host := hostFlag
	if host == "" {
		host = os.Getenv("DOKKU_HOST")
	}
	subprocess.SetDefaultHost(host)
	if sudo {
		_ = os.Setenv("DOKKU_SUDO", "1")
	}
	if acceptNewHostKeys {
		_ = os.Setenv("DOKKU_SSH_ACCEPT_NEW_HOST_KEYS", "1")
	}
	return host
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
