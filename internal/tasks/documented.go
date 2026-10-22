package tasks

// Documented is the interface a task implements to describe itself: a synopsis
// and a set of runnable examples. Neither has anything to do with running a
// task, so it is kept out of Task and modelled as an optional interface to
// match ExportDocer, ProbeDocer, DeprecationDocer and VersionDocer. Every
// registered task is expected to implement it - a coverage test enforces that
// so no task ships without a synopsis or examples - and the task catalog is
// what reads it, which in turn feeds the generated task pages and
// `docket schema`.
type Documented interface {
	// Doc returns the docblock for the task
	Doc() string

	// examples returns the examples for the task
	examples() ([]Doc, error)
}

// TaskSynopsis returns t's description, or "" when it has none.
//
//sdk:export
func TaskSynopsis(t Task) string {
	// Centralised so the catalog and the coverage test share one read site,
	// mirroring TaskDeprecation.
	if d, ok := t.(Documented); ok {
		return d.Doc()
	}
	return ""
}

// TaskExamples returns the examples for t, or none when t does not implement
// Documented. The error is the one examples() returns when an example fails to
// marshal.
func TaskExamples(t Task) ([]Doc, error) {
	if d, ok := t.(Documented); ok {
		return d.examples()
	}
	return nil, nil
}
