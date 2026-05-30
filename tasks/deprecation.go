package tasks

// DeprecationDocer is an optional interface a task implements when the task
// type is deprecated. Deprecation returns a human-readable notice naming
// the recommended replacement and (optionally) the reason. The docs
// generator renders the message in a Deprecated section on the task's
// page; --list-tasks marks the task; apply and plan emit a one-time
// warning when the task runs.
type DeprecationDocer interface {
	Deprecation() string
}

// TaskDeprecation returns the deprecation notice for t, or "" when t does
// not implement DeprecationDocer. Centralised so docs generation, the
// list-tasks renderer, and the apply/plan executors share one read site.
func TaskDeprecation(t Task) string {
	if d, ok := t.(DeprecationDocer); ok {
		return d.Deprecation()
	}
	return ""
}
