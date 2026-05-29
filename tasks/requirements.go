package tasks

// RequirementsDocer is an optional interface a task implements when it
// depends on a dokku plugin that is not part of dokku core. The docs
// generator renders the returned entries in a Requirements section on the
// task's page. Each entry names a plugin and may carry a parenthetical
// qualifier when the dependency is conditional.
type RequirementsDocer interface {
	Requirements() []string
}
