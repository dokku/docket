//go:generate go run docs.go docs/tasks
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/dokku/docket/internal/tasks"
)

// The per-task reference pages are rendered from tasks.Catalog(), the same
// machine-readable description `docket schema` emits. The reflection used to
// live here, which meant the only thing that could be done with docket's
// knowledge of its own task types was write markdown (#422). Rendering from the
// catalog instead is what keeps the published JSON and the published markdown
// describing the same thing.
//
// What stays here is presentation: the table layout and the section order.
// The prose of every note, task-level and per-field, is rendered in
// internal/tasks and published in the catalog's `notes` and `note` keys
// (#651), so an embedder reusing it prints exactly what these pages print.

// summarize reduces a task's docblock to a single-line summary for the index:
// the first line, trimmed to its first sentence.
func summarize(doc string) string {
	s := strings.TrimSpace(doc)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	if i := strings.Index(s, ". "); i >= 0 {
		s = s[:i+1]
	}
	return s
}

// escapeCell makes a string safe to embed in a markdown table cell.
func escapeCell(s string) string {
	s = strings.ReplaceAll(tasks.EscapeProse(s), "|", "\\|")
	return strings.ReplaceAll(s, "\n", " ")
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// paramDescription is the Parameters table's description cell: the raw
// description with the field's published note appended.
func paramDescription(field tasks.FieldSchema) string {
	if field.Note == nil {
		return field.Description
	}
	return field.Description + field.Note.Suffix
}

// parametersSection renders the Parameters table for a task. Returns "" when
// the task has no documented parameters.
func parametersSection(fields []tasks.FieldSchema) string {
	if len(fields) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Parameters\n\n")
	b.WriteString("| Parameter | Type | Required | Default | Choices | Description |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, field := range fields {
		fmt.Fprintf(&b,
			"| `%s` | %s | %s | %s | %s | %s |\n",
			field.Name,
			field.Type,
			yesNo(field.Required),
			escapeCell(field.Default),
			escapeCell(strings.Join(field.Choices, ", ")),
			escapeCell(paramDescription(field)),
		)
	}
	return b.String()
}

// afterParameters are the note kinds a page prints after the Parameters
// table, since they read as remarks on it. Every other note comes before it.
var afterParameters = map[tasks.NoteKind]bool{
	tasks.NoteConstraints: true,
	tasks.NoteProperties:  true,
}

// returnValuesSection is the shared Return Values table. Every task returns a
// tasks.TaskOutputState, so the table is identical across pages. The keys are
// the Go field names because that is how recipes reference them through
// `register:` and `result.<Field>`.
func returnValuesSection() string {
	rows := [][4]string{
		{"Changed", "always", "bool", "Whether the task changed server state."},
		{"State", "always", "string", "Resulting state of the resource."},
		{"DesiredState", "always", "string", "The state the task targeted."},
		{"Message", "always", "string", "Human-readable result message (may be empty)."},
		{"Commands", "when a subprocess ran", "list", "Resolved dokku command lines executed."},
		{"Stdout", "when a subprocess ran", "string", "Captured stdout of the final command."},
		{"Stderr", "when a subprocess ran", "string", "Captured stderr of the final command."},
		{"ExitCode", "when a subprocess ran", "int", "Exit code of the final command."},
	}
	var b strings.Builder
	b.WriteString("## Return Values\n\n")
	b.WriteString("Available after the task runs when captured with `register:`, referenced as `result.<Key>` (or `registered.<name>.<Key>`).\n\n")
	b.WriteString("| Key | Returned | Type | Description |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, r := range rows {
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n", r[0], r[1], r[2], r[3])
	}
	return b.String()
}

// examplesSection renders a task's examples under a single Examples header,
// one H3 subsection per example.
func examplesSection(examples []tasks.ExampleSchema) string {
	if len(examples) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Examples\n\n")
	for _, example := range examples {
		b.WriteString("### " + example.Name + "\n\n")
		b.WriteString("```yaml\n")
		b.WriteString(strings.TrimSpace(example.YAML) + "\n")
		b.WriteString("```\n\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// renderPage builds the full markdown page for one task from its catalog
// entry: the notes as published, with the Parameters table placed before the
// first note that comments on it.
func renderPage(schema tasks.TaskSchema) string {
	var sections []string
	sections = append(sections, "# "+schema.Type)
	sections = append(sections, "## Synopsis\n\n"+tasks.EscapeProse(schema.Synopsis))
	parameters := strings.TrimRight(parametersSection(schema.Fields), "\n")
	for _, note := range schema.Notes {
		if afterParameters[note.Kind] && parameters != "" {
			sections = append(sections, parameters)
			parameters = ""
		}
		sections = append(sections, note.Markdown)
	}
	if parameters != "" {
		sections = append(sections, parameters)
	}
	if ex := examplesSection(schema.Examples); ex != "" {
		sections = append(sections, strings.TrimRight(ex, "\n"))
	}
	sections = append(sections, strings.TrimRight(returnValuesSection(), "\n"))

	return strings.Join(sections, "\n\n") + "\n"
}

// renderIndex builds docs/tasks/README.md: every task with a one-line summary.
func renderIndex(catalog tasks.TaskCatalog) string {
	var index strings.Builder
	index.WriteString("# Tasks\n\n")
	index.WriteString("Reference for every task type docket can run inside a recipe. Each page lists the task's fields and example usage. These pages are generated from the task definitions with `make docs`.\n\n")
	index.WriteString("A task marked `(never converges)` cannot read its own state, so it plans as drift on every run. See the Probe support section on its page.\n\n")
	for _, schema := range catalog.Tasks {
		suffix := ""
		if schema.Deprecation != "" {
			suffix += " (deprecated)"
		}
		if schema.Probe.Status == tasks.ProbeUnsupported {
			suffix += " (never converges)"
		}
		fmt.Fprintf(&index, "- [%s](%s.md) - %s%s\n", schema.Type, schema.Type, tasks.EscapeProse(summarize(schema.Synopsis)), suffix)
	}
	return index.String()
}

func main() {
	docsFolderName := "../" + os.Args[1]
	docsFolderName, err := filepath.Abs(docsFolderName)
	if err != nil {
		log.Fatalf("failed to expand docs folder name: %v", err)
	}

	if _, err := os.Stat(docsFolderName); os.IsNotExist(err) {
		if err = os.MkdirAll(docsFolderName, 0755); err != nil {
			log.Fatalf("failed to create docs folder: %v", err)
		}
	}

	// The catalog is sorted by task type, which keeps the generation order
	// (and the console output) stable.
	catalog, err := tasks.Catalog()
	if err != nil {
		log.Fatalf("failed to build the task catalog: %v", err)
	}

	for _, schema := range catalog.Tasks {
		fmt.Println(schema.Type)

		taskDocsFile := filepath.Join(docsFolderName, schema.Type+".md")
		if err := os.WriteFile(taskDocsFile, []byte(renderPage(schema)), 0644); err != nil {
			log.Fatalf("failed to write docblock: %v", err)
		}
	}

	indexFile := filepath.Join(docsFolderName, "README.md")
	if err := os.WriteFile(indexFile, []byte(renderIndex(catalog)), 0644); err != nil {
		log.Fatalf("failed to write tasks index: %v", err)
	}
}
