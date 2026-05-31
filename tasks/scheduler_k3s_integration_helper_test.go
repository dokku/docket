package tasks

import (
	"os"
	"testing"
)

// skipUnlessSchedulerK3sT skips a scheduler-k3s integration test unless the
// runner has opted in via DOKKU_TEST_SCHEDULER_K3S=1. The default
// integration-test CI job leaves the env unset (and most local dev hosts have
// no k3s cluster), so every scheduler-k3s test skips with a clear message;
// the dedicated scheduler-k3s-test CI job sets the env after bootstrapping
// k3s via `dokku scheduler-k3s:initialize`. The skipIfNoDokkuT call inside
// preserves the existing shard membership + dokku binary presence checks.
func skipUnlessSchedulerK3sT(t *testing.T) {
	t.Helper()
	skipIfNoDokkuT(t)
	if os.Getenv("DOKKU_TEST_SCHEDULER_K3S") != "1" {
		t.Skip("set DOKKU_TEST_SCHEDULER_K3S=1 to run scheduler-k3s integration tests (requires k3s cluster, see scheduler-k3s-test CI job)")
	}
}
