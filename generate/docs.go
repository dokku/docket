//go:generate go run docs.go docs/tasks
package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/dokku/docket/tasks"
)

// The per-task reference pages are rendered from tasks.Catalog(), the same
// machine-readable description `docket schema` emits. The reflection used to
// live here, which meant the only thing that could be done with docket's
// knowledge of its own task types was write markdown (#422). Rendering from the
// catalog instead is what keeps the published JSON and the published markdown
// describing the same thing.
//
// What stays here is presentation: the prose the catalog deliberately does not
// bake into its `description` field, the table layout, and the section order.

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
	s = strings.ReplaceAll(s, "|", "\\|")
	return strings.ReplaceAll(s, "\n", " ")
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// itemFieldNames returns the recipe keys of a list-of-struct field's element,
// so the Parameters table can describe the item shape inline. Only a list
// qualifies: a dict's entries are keyed rather than shaped, and its value type
// is already carried by the type column.
func itemFieldNames(field tasks.FieldSchema) []string {
	if field.Type != tasks.TypeList || field.Item == nil || field.Item.Type != tasks.TypeObject {
		return nil
	}
	names := make([]string, 0, len(field.Item.Fields))
	for _, item := range field.Item.Fields {
		names = append(names, item.Name)
	}
	return names
}

// paramDescription is the Parameters table's description cell. The catalog
// carries the raw description tag and the facts (Sensitive, Item) separately,
// so the prose a reader expects is composed here rather than baked into the
// machine-readable form.
func paramDescription(field tasks.FieldSchema) string {
	desc := field.Description
	if items := itemFieldNames(field); len(items) > 0 {
		desc = strings.TrimSpace(desc)
		if desc != "" && !strings.HasSuffix(desc, ".") {
			desc += "."
		}
		desc = strings.TrimSpace(desc + " Each item has: " + strings.Join(items, ", ") + ".")
	}
	if field.Sensitive {
		desc = strings.TrimSpace(desc + " (sensitive)")
	}
	return desc
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
		b.WriteString(fmt.Sprintf(
			"| `%s` | %s | %s | %s | %s | %s |\n",
			field.Name,
			field.Type,
			yesNo(field.Required),
			escapeCell(field.Default),
			escapeCell(strings.Join(field.Choices, ", ")),
			escapeCell(paramDescription(field)),
		))
	}
	return b.String()
}

// propertiesSection renders the Properties table for a task that manages a
// dokku property table: every name its `property` field accepts, the scopes
// that accept it, and the report keys the drift probe reads. Without it the
// only way to discover a legal property name is to trigger a validation error.
func propertiesSection(schema *tasks.PropertySchema) string {
	if schema == nil || len(schema.Properties) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Properties\n\n")
	b.WriteString(fmt.Sprintf(
		"`property` accepts one of the following, applied with `dokku %s`. A property with no form in a scope is rejected there, matching dokku's own rejection.\n\n",
		schema.Subcommand,
	))
	b.WriteString("| Property | Scopes | Report key (app) | Report key (global) |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, property := range schema.Properties {
		name := "`" + property.Name + "`"
		if property.Sensitive {
			name += " (sensitive)"
		}
		b.WriteString(fmt.Sprintf(
			"| %s | %s | %s | %s |\n",
			name,
			strings.Join(property.Scopes, ", "),
			codeOrBlank(property.AppReportKey),
			codeOrBlank(property.GlobalReportKey),
		))
	}

	for _, family := range schema.Dynamic {
		b.WriteString(fmt.Sprintf("\nNames starting with `%s` are also accepted. dokku validates them through `%s` rather than through its report schema, so they cannot be listed above. ",
			family.Prefix, schema.Subcommand))
		if family.Probeable {
			b.WriteString("The plugin reports each one it has been given, so they probe for drift like any listed property.")
		} else {
			b.WriteString("The plugin does not report them, so they are applied on every run and never converge.")
		}
		if family.Sensitive {
			b.WriteString(" Their values are treated as secrets and masked.")
		}
		b.WriteString("\n")
	}

	for _, family := range schema.Rejected {
		b.WriteString(fmt.Sprintf("\nNames starting with `%s` are rejected here - they are managed by `%s`, since %s.\n",
			family.Prefix, family.Replacement, family.Reason))
	}
	return b.String()
}

// codeOrBlank renders a report key as inline code, or an empty cell when the
// property has no form in that scope.
func codeOrBlank(key string) string {
	if key == "" {
		return ""
	}
	return "`" + key + "`"
}

// requirementsSection renders the Requirements bullet list for a task that
// declares non-core plugin dependencies. Returns "" otherwise.
func requirementsSection(requirements []string) string {
	if len(requirements) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("## Requirements\n\n")
	for _, r := range requirements {
		b.WriteString("- " + r + "\n")
	}
	return b.String()
}

// exportSupportSection renders the Export support section, stating whether
// `docket export` can reconstruct the task from a live server. Every task
// declares this via ExportSupport(); the section is omitted only if a task
// does not (the export coverage test prevents that in practice).
func exportSupportSection(support tasks.ExportSupport) string {
	if support.Status == "" {
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

// probeSupportSection renders the Probe support section, stating whether
// `plan` can read the task's current state before deciding on drift. Every task
// declares this via ProbeSupport(); the section is omitted only if a task does
// not (the probe coverage test prevents that in practice). A task rendered as
// "Not supported" here plans as drift on every run, so it never lets
// `plan --detailed-exitcode` exit 0.
func probeSupportSection(support tasks.ProbeSupport) string {
	if support.Status == "" {
		return ""
	}
	var label string
	switch support.Status {
	case tasks.ProbeSupported:
		label = "Supported"
	case tasks.ProbePartial:
		label = "Partial"
	case tasks.ProbeUnsupported:
		label = "Not supported"
	default:
		label = string(support.Status)
	}
	line := label
	if support.Caveat != "" {
		line += " - " + support.Caveat
	}
	return "## Probe support\n\n" + line + "."
}

// identitySection renders the Identity section, naming the fields that select
// the resource the task manages and, where the task owns a collection, what
// identifies one entry in it. Every task declares at least one identity key;
// the section is omitted only if a task declares none, which the identity
// coverage test prevents.
//
// The address form is explained once in docs/task-envelope.md rather than
// repeated on 73 pages; what a reader needs here is which fields go into it.
func identitySection(identity tasks.IdentitySchema) string {
	if len(identity.Keys) == 0 {
		return ""
	}

	line := "Keyed by " + joinCodeNames(identity.Keys) + "."
	if len(identity.Keys) > 1 {
		line += " Fields left empty are omitted from the address."
	}
	for _, collection := range identity.Collections {
		line += fmt.Sprintf(" Manages the whole `%s` collection", collection.Name)
		switch collection.Item {
		case tasks.ItemIdentityMapKey:
			line += "; entries are identified by their key."
		case tasks.ItemIdentityFields:
			if len(collection.ItemKeys) > 0 {
				line += "; entries are identified by " + joinCodeNames(collection.ItemKeys) + "."
			} else {
				line += "."
			}
		default:
			line += "; entries are identified by value."
		}
	}
	return "## Identity\n\n" + line
}

// joinCodeNames renders a list of field names as backticked, comma-separated
// prose with a trailing "and".
func joinCodeNames(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = "`" + name + "`"
	}
	switch len(quoted) {
	case 1:
		return quoted[0]
	case 2:
		return quoted[0] + " and " + quoted[1]
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + ", and " + quoted[len(quoted)-1]
}

// deprecationSection renders the Deprecated admonition for a task that
// declares one. The notice sits between the Synopsis and the
// Requirements/Parameters sections so the reader sees it before scanning the
// field table.
func deprecationSection(message string) string {
	if message == "" {
		return ""
	}
	return "> **Deprecated:** " + message
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

// renderPage builds the full markdown page for one task.
func renderPage(schema tasks.TaskSchema) string {
	var sections []string
	sections = append(sections, "# "+schema.Type)
	sections = append(sections, "## Synopsis\n\n"+schema.Synopsis)
	if dep := deprecationSection(schema.Deprecation); dep != "" {
		sections = append(sections, dep)
	}
	if req := requirementsSection(schema.Requirements); req != "" {
		sections = append(sections, strings.TrimRight(req, "\n"))
	}
	if es := exportSupportSection(schema.Export); es != "" {
		sections = append(sections, es)
	}
	if ps := probeSupportSection(schema.Probe); ps != "" {
		sections = append(sections, ps)
	}
	if id := identitySection(schema.Identity); id != "" {
		sections = append(sections, id)
	}
	if pt := parametersSection(schema.Fields); pt != "" {
		sections = append(sections, strings.TrimRight(pt, "\n"))
	}
	if props := propertiesSection(schema.PropertySchema); props != "" {
		sections = append(sections, strings.TrimRight(props, "\n"))
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
		index.WriteString(fmt.Sprintf("- [%s](%s.md) - %s%s\n", schema.Type, schema.Type, summarize(schema.Synopsis), suffix))
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
