package tasks

import "testing"

// exportOrderMembership returns, for each task type-key, whether it appears in
// appExportOrder and globalExportOrder.
func exportOrderMembership() (app, global map[string]bool) {
	app = map[string]bool{}
	global = map[string]bool{}
	for _, key := range appExportOrder {
		app[key] = true
	}
	for _, key := range globalExportOrder {
		global[key] = true
	}
	return app, global
}

// TestExportSupportMatchesExportWiring asserts that a task's declared export
// support and its actual wiring agree. TestAppExportOrderIsValid and
// TestGlobalExportOrderIsValid only check one direction - every key in an order
// list resolves to a task implementing the matching interface - which let
// dokku_http_auth declare ExportPartial while implementing no exporter and
// appearing in no order list, so `docket export` silently dropped an app's
// http-auth state (#428). These are the converse invariants, and they rule out
// the whole class: a task that claims to export must actually be reachable from
// ExportRecipe, a task that claims it cannot must not be, and an exporter that
// is never listed is dead code.
func TestExportSupportMatchesExportWiring(t *testing.T) {
	inAppOrder, inGlobalOrder := exportOrderMembership()

	for name, task := range RegisteredTasks {
		support, ok := TaskExportSupport(task)
		if !ok {
			continue // TestEveryTaskDeclaresExportSupport reports this
		}

		_, isAppExporter := task.(AppExporter)
		_, isGlobalExporter := task.(GlobalExporter)

		// An exporter that no order list names is never called.
		if isAppExporter && !inAppOrder[name] {
			t.Errorf("task %q implements AppExporter but is missing from appExportOrder", name)
		}
		if isGlobalExporter && !inGlobalOrder[name] {
			t.Errorf("task %q implements GlobalExporter but is missing from globalExportOrder", name)
		}

		switch support.Status {
		case ExportSupported, ExportPartial:
			if !isAppExporter && !isGlobalExporter {
				t.Errorf("task %q declares export status %q but implements neither AppExporter nor GlobalExporter", name, support.Status)
				continue
			}
			if !inAppOrder[name] && !inGlobalOrder[name] {
				t.Errorf("task %q declares export status %q but appears in no export order, so it is never exported", name, support.Status)
			}
		case ExportUnsupported:
			if isAppExporter || isGlobalExporter {
				t.Errorf("task %q declares export status %q but implements an exporter", name, support.Status)
			}
			if inAppOrder[name] || inGlobalOrder[name] {
				t.Errorf("task %q declares export status %q but appears in an export order", name, support.Status)
			}
		}
	}
}

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
