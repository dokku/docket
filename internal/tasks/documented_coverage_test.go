package tasks

import (
	"strings"
	"testing"
)

// TestEveryTaskIsDocumented asserts that every registered task describes itself
// through Documented. Doc() and examples() are not part of Task, so the compiler
// no longer insists on them; without this check a new task could ship without
// a synopsis or examples and its generated page and `docket schema` entry would
// silently lose them.
func TestEveryTaskIsDocumented(t *testing.T) {
	for name, task := range allRegisteredTasks() {
		doc, ok := task.(Documented)
		if !ok {
			t.Errorf("task %q does not implement Documented (add Doc() and examples())", name)
			continue
		}
		if strings.TrimSpace(doc.Doc()) == "" {
			t.Errorf("task %q has an empty Doc()", name)
		}
		examples, err := doc.examples()
		if err != nil {
			t.Errorf("task %q examples() returned error: %v", name, err)
			continue
		}
		if len(examples) == 0 {
			t.Errorf("task %q has no examples", name)
		}
	}
}
