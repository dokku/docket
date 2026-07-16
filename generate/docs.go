//go:generate go run docs.go docs/tasks
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/dokku/docket/tasks"
)

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

// param is one row of a task's Parameters table, derived from a struct field.
type param struct {
	Name        string
	Type        string
	Required    bool
	Default     string
	Choices     string
	Description string
}

// displayType maps a Go field type to the doc-facing type name. Named string
// types such as State render as "string"; slices render as "list" and maps as
// "dict", mirroring Ansible's type vocabulary. Pointer fields (e.g. an optional
// *bool) render as their element type so they read as "bool" rather than "*bool".
func displayType(t reflect.Type) string {
	if t.Kind() == reflect.Ptr {
		return displayType(t.Elem())
	}
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "bool"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return "int"
	case reflect.Slice:
		return "list"
	case reflect.Map:
		return "dict"
	default:
		return t.String()
	}
}

// yamlName extracts the recipe key for a field from its yaml tag, dropping any
// ",omitempty"-style suffix. Returns "" when the field is not serialized.
func yamlName(tag reflect.StructTag, fieldName string) string {
	name := tag.Get("yaml")
	if comma := strings.IndexByte(name, ','); comma >= 0 {
		name = name[:comma]
	}
	if name == "-" {
		return ""
	}
	if name == "" {
		return strings.ToLower(fieldName)
	}
	return name
}

// elementYamlNames returns the recipe keys of the fields of a slice element
// struct, so a list-of-struct field can describe its item shape inline.
func elementYamlNames(t reflect.Type) []string {
	if t.Kind() != reflect.Slice {
		return nil
	}
	el := t.Elem()
	if el.Kind() != reflect.Struct {
		return nil
	}
	var names []string
	for i := 0; i < el.NumField(); i++ {
		f := el.Field(i)
		if n := yamlName(f.Tag, f.Name); n != "" {
			names = append(names, n)
		}
	}
	return names
}

// buildParams reflects over a task struct type and returns its parameters in
// declaration order.
func buildParams(rt reflect.Type) []param {
	var params []param
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if field.PkgPath != "" { // unexported
			continue
		}
		name := yamlName(field.Tag, field.Name)
		if name == "" {
			continue
		}

		def := field.Tag.Get("default")
		// A field with a default is effectively optional even if it is
		// tagged required:"true".
		required := field.Tag.Get("required") == "true" && def == ""

		desc := field.Tag.Get("description")
		if items := elementYamlNames(field.Type); len(items) > 0 {
			desc = strings.TrimSpace(desc)
			if desc != "" && !strings.HasSuffix(desc, ".") {
				desc += "."
			}
			desc = strings.TrimSpace(desc + " Each item has: " + strings.Join(items, ", ") + ".")
		}
		if field.Tag.Get("sensitive") == "true" {
			desc = strings.TrimSpace(desc + " (sensitive)")
		}

		choices := ""
		if opts := field.Tag.Get("options"); opts != "" {
			choices = strings.Join(strings.Split(opts, ","), ", ")
		}

		params = append(params, param{
			Name:        name,
			Type:        displayType(field.Type),
			Required:    required,
			Default:     def,
			Choices:     choices,
			Description: desc,
		})
	}
	return params
}

// escapeCell makes a string safe to embed in a markdown table cell.
func escapeCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.ReplaceAll(s, "\n", " ")
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// parametersSection renders the Parameters table for a task. Returns "" when
// the task has no documented parameters.
func parametersSection(params []param) string {
	if len(params) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Parameters\n\n")
	b.WriteString("| Parameter | Type | Required | Default | Choices | Description |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, p := range params {
		b.WriteString(fmt.Sprintf(
			"| `%s` | %s | %s | %s | %s | %s |\n",
			p.Name,
			p.Type,
			yesNo(p.Required),
			escapeCell(p.Default),
			escapeCell(p.Choices),
			escapeCell(p.Description),
		))
	}
	return b.String()
}

// requirementsSection renders the Requirements bullet list for a task that
// declares non-core plugin dependencies. Returns "" otherwise.
func requirementsSection(task tasks.Task) string {
	doc, ok := task.(tasks.RequirementsDocer)
	if !ok {
		return ""
	}
	reqs := doc.Requirements()
	if len(reqs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Requirements\n\n")
	for _, r := range reqs {
		b.WriteString("- " + r + "\n")
	}
	return b.String()
}

// exportSupportSection renders the Export support section, stating whether
// `docket export` can reconstruct the task from a live server. Every task
// declares this via ExportSupport(); the section is omitted only if a task
// does not (the export coverage test prevents that in practice).
func exportSupportSection(task tasks.Task) string {
	support, ok := tasks.TaskExportSupport(task)
	if !ok {
		return ""
	}
	var label string
	switch support.Status {
	case tasks.ExportSupported:
		label = "Supported"
	case tasks.ExportPartial:
		label = "Partial"
	case tasks.ExportUnsupported:
		label = "Not supported"
	default:
		label = string(support.Status)
	}
	line := label
	if support.Caveat != "" {
		line += " - " + support.Caveat
	}
	return "## Export support\n\n" + line + "."
}

// deprecationSection renders the Deprecated admonition for a task that
// implements DeprecationDocer. The notice sits between the Synopsis and
// the Requirements/Parameters sections so the reader sees it before
// scanning the field table.
func deprecationSection(task tasks.Task) string {
	msg := tasks.TaskDeprecation(task)
	if msg == "" {
		return ""
	}
	return "> **Deprecated:** " + strings.TrimSpace(msg)
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
		b.WriteString(fmt.Sprintf("| `%s` | %s | %s | %s |\n", r[0], r[1], r[2], r[3]))
	}
	return b.String()
}

// examplesSection renders a task's examples under a single Examples header,
// one H3 subsection per example.
func examplesSection(taskName string, examples []tasks.Doc) string {
	if len(examples) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Examples\n\n")
	for _, example := range examples {
		b.WriteString("### " + example.Name + "\n\n")
		b.WriteString("```yaml\n")
		b.WriteString(strings.TrimSpace(example.Codeblock) + "\n")
		b.WriteString("```\n\n")
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// renderPage builds the full markdown page for one task.
func renderPage(taskName string, task tasks.Task, examples []tasks.Doc) string {
	rt := reflect.TypeOf(task)
	if rt.Kind() == reflect.Ptr {
		rt = rt.Elem()
	}

	var sections []string
	sections = append(sections, "# "+taskName)
	sections = append(sections, "## Synopsis\n\n"+strings.TrimSpace(task.Doc()))
	if dep := deprecationSection(task); dep != "" {
		sections = append(sections, dep)
	}
	if req := requirementsSection(task); req != "" {
		sections = append(sections, strings.TrimRight(req, "\n"))
	}
	if es := exportSupportSection(task); es != "" {
		sections = append(sections, es)
	}
	if pt := parametersSection(buildParams(rt)); pt != "" {
		sections = append(sections, strings.TrimRight(pt, "\n"))
	}
	if ex := examplesSection(taskName, examples); ex != "" {
		sections = append(sections, strings.TrimRight(ex, "\n"))
	}
	sections = append(sections, strings.TrimRight(returnValuesSection(), "\n"))

	return strings.Join(sections, "\n\n") + "\n"
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

	registeredTasks := tasks.RegisteredTasks

	// Sorted names keep the generation order (and console output) stable.
	names := make([]string, 0, len(registeredTasks))
	for name := range registeredTasks {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, taskName := range names {
		fmt.Println(taskName)
		task := registeredTasks[taskName]

		examples, err := task.Examples()
		if err != nil {
			log.Fatalf("failed to get examples for task %s: %v", taskName, err)
		}

		output := renderPage(taskName, task, examples)

		taskDocsFile := filepath.Join(docsFolderName, taskName+".md")
		if err := os.WriteFile(taskDocsFile, []byte(output), 0644); err != nil {
			log.Fatalf("failed to write docblock: %v", err)
		}
	}

	// Emit an index listing every task with a one-line summary.
	var index strings.Builder
	index.WriteString("# Tasks\n\n")
	index.WriteString("Reference for every task type docket can run inside a recipe. Each page lists the task's fields and example usage. These pages are generated from the task definitions with `make docs`.\n\n")
	for _, name := range names {
		suffix := ""
		if tasks.TaskDeprecation(registeredTasks[name]) != "" {
			suffix = " (deprecated)"
		}
		index.WriteString(fmt.Sprintf("- [%s](%s.md) - %s%s\n", name, name, summarize(registeredTasks[name].Doc()), suffix))
	}

	indexFile := filepath.Join(docsFolderName, "README.md")
	if err := os.WriteFile(indexFile, []byte(index.String()), 0644); err != nil {
		log.Fatalf("failed to write tasks index: %v", err)
	}
}
