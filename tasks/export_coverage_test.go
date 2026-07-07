package tasks

import "testing"

// TestEveryTaskDeclaresExportSupport asserts that every registered task
// declares its export support via ExportSupport(). This is what makes "export
// covers every task type" enforceable: adding a new task without an
// ExportSupport() declaration fails the build here rather than silently
// shipping without an export decision.
func TestEveryTaskDeclaresExportSupport(t *testing.T) {
	for name, task := range RegisteredTasks {
		support, ok := TaskExportSupport(task)
		if !ok {
			t.Errorf("task %q does not implement ExportDocer (add an ExportSupport() declaration)", name)
			continue
		}
		switch support.Status {
		case ExportSupported, ExportPartial, ExportUnsupported:
			// valid
		default:
			t.Errorf("task %q declares an unknown export status %q", name, support.Status)
		}
		if support.Status != ExportSupported && support.Caveat == "" {
			t.Errorf("task %q is %q but has no caveat explaining why", name, support.Status)
		}
	}
}
