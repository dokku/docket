package tasks

// ImperativeDocer is an optional interface a task implements when it runs an
// operation rather than declaring a resource: it has nothing to undo, so it
// offers no absent or clear state, and a consumer that tears down what it
// applied skips it. The catalog marks such a task `imperative`, and its
// reference page says so.
type ImperativeDocer interface {
	Imperative() bool
}

// TaskImperative reports whether t declares itself imperative. A task that
// does not implement ImperativeDocer is declarative. Centralised so the
// catalog and the docs generator share one read site.
func TaskImperative(t Task) bool {
	if i, ok := t.(ImperativeDocer); ok {
		return i.Imperative()
	}
	return false
}
