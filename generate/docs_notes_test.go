package main

import (
	"strings"
	"testing"

	"github.com/dokku/docket/internal/tasks"
)

// TestRenderPageUsesPublishedNotes asserts the page is built from the
// catalog's published notes rather than from prose of its own (#651): a note
// the catalog carries is printed verbatim, the Parameters table goes before
// the first note that comments on it, and a field's published suffix is what
// its description cell gains.
func TestRenderPageUsesPublishedNotes(t *testing.T) {
	schema := tasks.TaskSchema{
		Type:     "dokku_example",
		Synopsis: "Manages an example",
		Fields: []tasks.FieldSchema{
			{Name: "app", Type: tasks.TypeString, Description: "Name of the app", Note: &tasks.FieldNoteSchema{Suffix: " (published suffix)"}},
		},
		Notes: []tasks.NoteSchema{
			{Kind: tasks.NoteIdentity, Markdown: "## Identity\n\nPUBLISHED IDENTITY"},
			{Kind: tasks.NoteConstraints, Markdown: "## Constraints\n\nPUBLISHED CONSTRAINTS"},
		},
	}
	page := renderPage(schema)

	identity := strings.Index(page, "PUBLISHED IDENTITY")
	parameters := strings.Index(page, "## Parameters")
	constraints := strings.Index(page, "PUBLISHED CONSTRAINTS")
	if identity < 0 || parameters < 0 || constraints < 0 || identity > parameters || parameters > constraints {
		t.Errorf("page does not print identity, then parameters, then constraints:\n%s", page)
	}
	if !strings.Contains(page, "| Name of the app (published suffix) |") {
		t.Errorf("parameters row does not carry the published suffix:\n%s", page)
	}

	// With no note after the table, the table follows every note.
	schema.Notes = schema.Notes[:1]
	page = renderPage(schema)
	if strings.Index(page, "PUBLISHED IDENTITY") > strings.Index(page, "## Parameters") {
		t.Errorf("parameters table printed before a note that precedes it:\n%s", page)
	}
}

// TestRenderedPagesMatchPublishedNotes asserts, for every registered task,
// that each published note appears on the page verbatim and each field's
// description cell is its description plus its published suffix, so the
// catalog and the reference pages cannot diverge.
func TestRenderedPagesMatchPublishedNotes(t *testing.T) {
	catalog, err := tasks.Catalog()
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}
	for _, schema := range catalog.Tasks {
		page := renderPage(schema)
		for _, note := range schema.Notes {
			if !strings.Contains(page, "\n\n"+note.Markdown+"\n\n") {
				t.Errorf("%s: page does not print the published %s note verbatim", schema.Type, note.Kind)
			}
		}
		for _, field := range schema.Fields {
			cell := field.Description
			if field.Note != nil {
				cell += field.Note.Suffix
			}
			if !strings.Contains(page, " | "+escapeCell(cell)+" |\n") {
				t.Errorf("%s: %s description cell is not the description plus its published suffix", schema.Type, field.Name)
			}
		}
	}
}
