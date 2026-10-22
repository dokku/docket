package tasks

type Doc struct {
	Name      string
	Codeblock string

	// Requires are the tasks the example declares it needs applied first,
	// beyond the ones TaskRequires derives. Absent for most examples.
	Requires []Task
}
