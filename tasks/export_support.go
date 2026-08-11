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
//
// The json tags are for the task catalog, which publishes this verbatim.
type ExportSupport struct {
	Status ExportStatus `json:"status"`
	Caveat string       `json:"caveat,omitempty"`
}

// ExportDocer is the interface a task implements to declare its export
// support. Every registered task is expected to implement it - a coverage
// test enforces that so no task ships without an export decision - but it is
// modelled as an optional interface to match DeprecationDocer and
// RequirementsDocer. The docs generator renders the result in an Export
// support section on the task's page.
//
// The export engine itself never reads this: emission is driven purely by
// membership in appExportOrder/globalExportOrder plus the AppExporter/
// GlobalExporter type assertion. What keeps the declaration honest is
// TestExportSupportMatchesExportWiring, which fails when a task claims to be
// exportable without being wired into an order list (or vice versa).
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
