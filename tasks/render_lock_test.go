package tasks

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
)

// TestRenderTemplateLeavesTheEnvironmentAlone is the regression lock on the
// render lock. sigil exports template variables into the process environment
// and restores it with os.Clearenv on the way out, so two concurrent renders
// interleave a wipe with a restore and variables vanish from the process for
// good. Serial callers never saw it; #502 made the command tests parallel and
// the environment started emptying underneath them.
//
// The assertion is on the environment rather than on the rendered output
// because the corruption is the damage - a render that loses PATH has already
// broken every later command in the process, whatever it returned.
//
// Serial on purpose. The lock stops renders corrupting each other, but a
// render still empties the environment for the instant between its Clearenv
// and its replay, so a snapshot taken while any other test is rendering can
// come back short or carry that render's template variables. The serial phase
// is the only place a whole-environment assertion means anything.
func TestRenderTemplateLeavesTheEnvironmentAlone(t *testing.T) {
	before := snapshotEnv()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Each goroutine exports its own variable names, so an
			// interleaved restore replays a snapshot that never held the
			// other's - which is how a real variable goes missing.
			for n := 0; n < 40; n++ {
				vars := map[string]interface{}{
					fmt.Sprintf("app_%d_%d", i, n):  "app",
					fmt.Sprintf("host_%d_%d", i, n): "host",
				}
				body := fmt.Sprintf("name: {{ .app_%d_%d }}-{{ .host_%d_%d }}\n", i, n, i, n)
				if _, err := RenderTemplate([]byte(body), vars, "test"); err != nil {
					t.Errorf("RenderTemplate: %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	if after := snapshotEnv(); after != before {
		t.Errorf("concurrent renders changed the process environment\nlost: %v", lostKeys(before, after))
	}
}

// snapshotEnv sorts because os.Environ()'s order is not stable across a
// Setenv/Unsetenv round trip, and a reordering is not the damage being pinned.
func snapshotEnv() string {
	env := os.Environ()
	sort.Strings(env)
	return strings.Join(env, "\n")
}

func lostKeys(before, after string) []string {
	have := map[string]bool{}
	for _, kv := range strings.Split(after, "\n") {
		have[kv] = true
	}
	var lost []string
	for _, kv := range strings.Split(before, "\n") {
		if !have[kv] {
			lost = append(lost, strings.SplitN(kv, "=", 2)[0])
		}
	}
	return lost
}
