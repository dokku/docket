package tasks

// Documented is the interface a task implements to describe itself: a synopsis
// and a set of runnable examples. Neither has anything to do with running a
// task, so it is kept out of Task and modelled as an optional interface to
// match ExportDocer, ProbeDocer, DeprecationDocer and RequirementsDocer. Every
// registered task is expected to implement it - a coverage test enforces that
// so no task ships without a synopsis or examples - and the task catalog is
// what reads it, which in turn feeds the generated task pages and
// `docket schema`.
type Documented interface {
	// Doc returns the docblock for the task
	Doc() string

	// Examples returns the examples for the task
	Examples() ([]Doc, error)
}

// TaskSynopsis returns the docblock for t, or "" when t does not implement
// Documented. Centralised so the catalog and the coverage test share one read
// site, mirroring TaskDeprecation.
func TaskSynopsis(t Task) string {
	if d, ok := t.(Documented); ok {
		return d.Doc()
	}
	return ""
}

// TaskExamples returns the examples for t, or none when t does not implement
// Documented. The error is the one Examples() returns when an example fails to
// marshal.
func TaskExamples(t Task) ([]Doc, error) {
	if d, ok := t.(Documented); ok {
		return d.Examples()
	}
	return nil, nil
}
