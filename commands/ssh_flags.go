package commands

import (
	"context"

	"github.com/dokku/docket/subprocess"
	"github.com/dokku/docket/tasks"
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
//
// getenv is how the three variables are read - `os.Getenv` in production. It
// is a parameter rather than a direct call so the tests can state their own
// environment and still run in parallel, which `t.Setenv` panics on.
func resolveSshFlags(getenv func(string) string, hostFlag string, sudo, acceptNewHostKeys bool) subprocess.Target {
	return subprocess.Target{
		Host:              firstNonEmpty(hostFlag, getenv("DOKKU_HOST")),
		Sudo:              sudo || getenv("DOKKU_SUDO") == "1",
		AcceptNewHostKeys: acceptNewHostKeys || getenv("DOKKU_SSH_ACCEPT_NEW_HOST_KEYS") == "1",
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

// closeControlMasters tears down the multiplexed SSH connection for every host
// the run could have opened one against: the run-wide target, plus whatever
// the plays declared for themselves.
//
// One per host rather than one per run, because a recipe whose plays span
// servers opens a master per server. Best-effort, like the underlying call -
// a host that was never reached has no socket and is skipped there.
func closeControlMasters(base subprocess.Target, plays []*tasks.Play) {
	seen := map[string]bool{}
	for _, host := range append([]string{base.Host}, playHosts(base, plays)...) {
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		_ = subprocess.CloseSshControlMaster(host)
	}
}

// playHosts returns the host each play resolves to against base.
func playHosts(base subprocess.Target, plays []*tasks.Play) []string {
	hosts := make([]string, 0, len(plays))
	for _, play := range plays {
		if play == nil {
			continue
		}
		hosts = append(hosts, play.ResolveTarget(base).Host)
	}
	return hosts
}
