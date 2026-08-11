package commands

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// jsonTaskNames runs an apply with --json and returns the `name` of every
// `task` event, in emission order.
func jsonTaskNames(t *testing.T, path string) []string {
	t.Helper()
	stdout, stderr, exit := runApply(t, path, "--json")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	var names []string
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if line == "" {
			continue
		}
		var ev struct {
			Type string `json:"type"`
			Name string `json:"name"`
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("event is not JSON: %v\n%s", err, line)
		}
		if ev.Type == "task" {
			names = append(names, ev.Name)
		}
	}
	return names
}

// TestApplyJSONTaskNamesAreStableAcrossRuns is the guarantee #427 exists to
// provide: `name` is the only correlation key in the event stream, so two runs
// of one recipe have to emit the same names for the same tasks. Before the
// change an unnamed task carried eight random bytes and a consumer diffing one
// run against another could line nothing up.
func TestApplyJSONTaskNamesAreStableAcrossRuns(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- tasks:
    - dokku_stub: { key: a }
    - dokku_stub: { key: b }
    - name: explicit
      dokku_stub: { key: c }
`)

	first := jsonTaskNames(t, path)
	second := jsonTaskNames(t, path)

	if !reflect.DeepEqual(first, second) {
		t.Fatalf("task names differ between runs:\n first = %v\nsecond = %v", first, second)
	}
	want := []string{"dokku_stub[key=a]", "dokku_stub[key=b]", "explicit"}
	if !reflect.DeepEqual(first, want) {
		t.Errorf("task names = %v, want %v", first, want)
	}
}

// TestApplyStartAtTaskAcceptsGeneratedName covers the other half: an unnamed
// task could not be named on the command line at all before, because its name
// was different every run.
func TestApplyStartAtTaskAcceptsGeneratedName(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- tasks:
    - dokku_stub: { key: a }
    - dokku_stub: { key: b }
`)

	stdout, stderr, exit := runApply(t, path, "--start-at-task", "dokku_stub[key=b]")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "[skipped] dokku_stub[key=a]") {
		t.Errorf("expected the earlier task to be skipped; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "before --start-at-task") {
		t.Errorf("expected the resume-point skip reason; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "[skipped] dokku_stub[key=b]") {
		t.Errorf("the named task should run, not be skipped; got:\n%s", stdout)
	}
}

// TestApplyStartAtTaskUnknownNameListsGeneratedNames asserts the failure hint
// is usable: when the name matches nothing, the available names it prints are
// the addresses the user would have to type.
func TestApplyStartAtTaskUnknownNameListsGeneratedNames(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- tasks:
    - dokku_stub: { key: a }
`)

	stdout, stderr, exit := runApply(t, path, "--start-at-task", "nope")
	if exit == 0 {
		t.Fatalf("expected a non-zero exit; stdout=%s", stdout)
	}
	if !strings.Contains(stderr+stdout, "dokku_stub[key=a]") {
		t.Errorf("expected the available names to list the generated address; got:\n%s\n%s", stdout, stderr)
	}
}
