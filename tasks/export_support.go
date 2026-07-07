package tasks

// ExportStatus classifies how a task participates in `docket export`, which
// reads a live Dokku server and emits a recipe (the inverse of apply).
type ExportStatus string

const (
	// ExportSupported means the task's full state can be read back from the
	// server and reconstructed faithfully.
	ExportSupported ExportStatus = "supported"

	// ExportPartial means the task can be exported but with caveats, for
	// example a secret value that is written to the companion vars-file, or a
	// field that cannot be read back and becomes a required input.
	ExportPartial ExportStatus = "partial"

	// ExportUnsupported means the task cannot be reconstructed from live
	// state: write-only secrets that never read back, imperative operations
	// that are not state, or resources whose export is not implemented yet.
	ExportUnsupported ExportStatus = "unsupported"
)

// ExportSupport describes a task's export behaviour. Status is required;
// Caveat is a human-readable note explaining a partial or unsupported status
// (and may be empty for a plainly supported task).
type ExportSupport struct {
	Status ExportStatus
	Caveat string
}

// ExportDocer is the interface a task implements to declare its export
// support. Every registered task is expected to implement it - a coverage
// test enforces that so no task ships without an export decision - but it is
// modelled as an optional interface to match DeprecationDocer and
// RequirementsDocer. The docs generator renders the result in an Export
// support section on the task's page; the export engine reads it to decide
// whether to emit or skip (and warn) the task.
type ExportDocer interface {
	ExportSupport() ExportSupport
}

// TaskExportSupport returns the export support for t, and whether t declared
// it. Centralised so docs generation, the export engine, and the coverage
// test share one read site, mirroring TaskDeprecation.
func TaskExportSupport(t Task) (ExportSupport, bool) {
	if d, ok := t.(ExportDocer); ok {
		return d.ExportSupport(), true
	}
	return ExportSupport{}, false
}

// serviceExportCaveat is the shared caveat for the datastore/service tasks,
// whose export is deferred to a follow-up issue (it needs a per-service-type
// `<service>:list` primitive that does not exist yet). Each service task's
// ExportSupport() references this so the wording stays consistent.
const serviceExportCaveat = "service export is not yet implemented; tracked in a follow-up issue"
