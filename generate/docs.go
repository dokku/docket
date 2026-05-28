//go:generate go run docs.go docs/tasks
package main

import (
	"fmt"
	"log"
	"github.com/dokku/docket/tasks"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

func main() {
	docsFolderName := "../" + os.Args[1]
	// expand docsFolderName
	docsFolderName, err := filepath.Abs(docsFolderName)
	if err != nil {
		log.Fatalf("failed to expand docs folder name: %v", err)
	}

	// create docs folder if it doesn't exist
	if _, err := os.Stat(docsFolderName); os.IsNotExist(err) {
		err = os.MkdirAll(docsFolderName, 0755)
		if err != nil {
			log.Fatalf("failed to create docs folder: %v", err)
		}
	}

	markdownTemplate := `
# %s

%s
%s
`

	sectionTemplate := `
## %s

%syaml
%s
%s`

	codefence := "```"

	// read in all registered tasks
	registeredTasks := tasks.RegisteredTasks

	// for each registered task, generate a docs file
	for taskName, task := range registeredTasks {
		fmt.Println(taskName)

		examples, err := task.Examples()
		if err != nil {
			log.Fatalf("failed to get examples for task %s: %v", taskName, err)
		}

		docblock := task.Doc()

		var exampleSections []string
		for _, example := range examples {
			example := fmt.Sprintf(sectionTemplate, example.Name, codefence, strings.TrimSpace(example.Codeblock), codefence)
			exampleSections = append(exampleSections, example)
		}

		examplesYaml := strings.Join(exampleSections, "\n")
		markdown := fmt.Sprintf(markdownTemplate, taskName, docblock, examplesYaml)
		output := strings.TrimSpace(markdown) + "\n"

		taskDocsFile := filepath.Join(docsFolderName, taskName+".md")
		err = os.WriteFile(taskDocsFile, []byte(output), 0644)
		if err != nil {
			log.Fatalf("failed to write docblock: %v", err)
		}
	}

	// Emit an index listing every task with a one-line summary, sorted by name
	// so the output is stable across runs.
	names := make([]string, 0, len(registeredTasks))
	for name := range registeredTasks {
		names = append(names, name)
	}
	sort.Strings(names)

	var index strings.Builder
	index.WriteString("# Tasks\n\n")
	index.WriteString("Reference for every task type docket can run inside a recipe. Each page lists the task's fields and example usage. These pages are generated from the task definitions with `make docs`.\n\n")
	for _, name := range names {
		index.WriteString(fmt.Sprintf("- [%s](%s.md) - %s\n", name, name, summarize(registeredTasks[name].Doc())))
	}

	indexFile := filepath.Join(docsFolderName, "README.md")
	if err := os.WriteFile(indexFile, []byte(index.String()), 0644); err != nil {
		log.Fatalf("failed to write tasks index: %v", err)
	}
}
