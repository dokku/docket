package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/dokku/docket/tasks"

	"github.com/aymanbagabas/go-udiff"
)

// docsDir is where the generated reference pages live, relative to this
// package. `go test` runs with the package directory as the working directory,
// which is the same place `go generate` runs main() from.
const docsDir = "../docs/tasks"

// TestGeneratedDocsAreCurrent asserts that every committed page under
// docs/tasks/ is exactly what the generator would write today.
//
// The pages are generated but committed, and nothing in CI ran `make docs` and
// diffed the result, so a task whose fields or declarations changed could ship
// with a stale reference page indefinitely. This closes that gap in `go test`,
// without needing a `go generate` step in the workflow.
//
// It is also the safety net for rendering the pages from tasks.Catalog()
// instead of from the generator's own reflection (#422): the refactor is
// correct exactly insofar as this test passes unchanged.
func TestGeneratedDocsAreCurrent(t *testing.T) {
	catalog, err := tasks.Catalog()
	if err != nil {
		t.Fatalf("build catalog: %v", err)
	}

	for _, schema := range catalog.Tasks {
		path := filepath.Join(docsDir, schema.Type+".md")
		committed, err := os.ReadFile(path)
		if err != nil {
			t.Errorf("read %s: %v (run make docs)", path, err)
			continue
		}
		assertRendered(t, path, string(committed), renderPage(schema))
	}

	indexPath := filepath.Join(docsDir, "README.md")
	committed, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("read %s: %v (run make docs)", indexPath, err)
	}
	assertRendered(t, indexPath, string(committed), renderIndex(catalog))
}

// assertRendered compares a committed page against freshly rendered markdown,
// reporting a unified diff rather than two walls of text.
func assertRendered(t *testing.T, path, committed, rendered string) {
	t.Helper()
	if committed == rendered {
		return
	}
	diff, err := udiff.ToUnified(path, path+" (regenerated)", committed, udiff.Strings(committed, rendered), udiff.DefaultContextLines)
	if err != nil {
		t.Errorf("%s is out of date and the diff could not be rendered: %v", path, err)
		return
	}
	t.Errorf("%s is out of date; run make docs\n%s", path, diff)
}

// TestGeneratedDocsCoverEveryTask asserts that the set of pages on disk is
// exactly the set of registered tasks. TestGeneratedDocsAreCurrent only looks
// at pages the registry names, so a task that was removed or renamed would
// otherwise leave an orphan page behind - the generator never deletes.
func TestGeneratedDocsCoverEveryTask(t *testing.T) {
	entries, err := os.ReadDir(docsDir)
	if err != nil {
		t.Fatalf("read %s: %v", docsDir, err)
	}

	onDisk := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".md") || name == "README.md" {
			continue
		}
		onDisk[strings.TrimSuffix(name, ".md")] = true
	}

	for name := range tasks.RegisteredTasks {
		if !onDisk[name] {
			t.Errorf("task %q has no page at %s/%s.md (run make docs)", name, docsDir, name)
		}
		delete(onDisk, name)
	}

	orphans := make([]string, 0, len(onDisk))
	for name := range onDisk {
		orphans = append(orphans, name)
	}
	sort.Strings(orphans)
	for _, name := range orphans {
		t.Errorf("%s/%s.md describes no registered task; delete it", docsDir, name)
	}
}
