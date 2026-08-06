package tasks

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// docs/ansible-dokku.md tells a wrapper author which docket task each
// ansible-dokku module maps to, and which task types have no module at
// all. Between the two lists it has to account for every registered task
// type, or a wrapper reading it will silently believe docket cannot do
// something it can.
//
// Nothing regenerates that page (unlike docs/tasks/*.md, which `make
// docs` writes from the task definitions), so this test is what keeps it
// honest. It sits next to TestRegisteredTaskCount deliberately: adding a
// task type trips both at once, and the failure names the file to edit.
// The prior art for what happens without a guard is
// TestRegisteredTasksExist, whose hardcoded allowlist drifted to 56 of
// 73 without anyone noticing.

const ansibleMappingDoc = "../docs/ansible-dokku.md"

const (
	mappingHeading  = "## Module mapping"
	noModuleHeading = "## docket tasks with no ansible-dokku module"
)

// backtickedTask matches a `dokku_...` token inside backticks.
var backtickedTask = regexp.MustCompile("`(dokku_[a-z0-9_]+)`")

// sectionOf returns the lines of doc between the given "## " heading and
// the next one.
func sectionOf(t *testing.T, doc []string, heading string) []string {
	t.Helper()
	start := -1
	for i, line := range doc {
		if strings.TrimSpace(line) == heading {
			start = i + 1
			break
		}
	}
	if start == -1 {
		t.Fatalf("%s: heading %q not found; the mapping guard keys off it", ansibleMappingDoc, heading)
	}
	for i := start; i < len(doc); i++ {
		if strings.HasPrefix(doc[i], "## ") {
			return doc[start:i]
		}
	}
	return doc[start:]
}

// tasksInMappingTable pulls the docket-task column out of the module
// mapping table. Column 0 holds ansible-dokku module names, which
// deliberately share the dokku_ prefix, so only column 1 is read.
func tasksInMappingTable(t *testing.T, doc []string) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	rows := 0
	for _, line := range sectionOf(t, doc, mappingHeading) {
		if !strings.HasPrefix(strings.TrimSpace(line), "|") {
			continue
		}
		cells := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
		if len(cells) < 2 {
			continue
		}
		header := strings.TrimSpace(cells[0])
		if header == "ansible-dokku module" || strings.Trim(header, "-: ") == "" {
			continue
		}
		rows++
		for _, m := range backtickedTask.FindAllStringSubmatch(cells[1], -1) {
			found[m[1]] = true
		}
	}
	if rows == 0 {
		t.Fatalf("%s: no rows parsed under %q", ansibleMappingDoc, mappingHeading)
	}
	return found
}

// tasksInNoModuleSection pulls every backticked task name out of the
// "no ansible-dokku module" section.
func tasksInNoModuleSection(t *testing.T, doc []string) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	for _, line := range sectionOf(t, doc, noModuleHeading) {
		for _, m := range backtickedTask.FindAllStringSubmatch(line, -1) {
			found[m[1]] = true
		}
	}
	return found
}

// TestAnsibleMappingCoversEveryRegisteredTask asserts that the two lists
// in docs/ansible-dokku.md together name every registered task type, and
// name nothing that is not registered.
func TestAnsibleMappingCoversEveryRegisteredTask(t *testing.T) {
	raw, err := os.ReadFile(ansibleMappingDoc)
	if err != nil {
		t.Fatalf("read %s: %v", ansibleMappingDoc, err)
	}
	doc := strings.Split(string(raw), "\n")

	documented := tasksInMappingTable(t, doc)
	for name := range tasksInNoModuleSection(t, doc) {
		documented[name] = true
	}

	var missing, unknown []string
	for name := range RegisteredTasks {
		if !documented[name] {
			missing = append(missing, name)
		}
	}
	for name := range documented {
		if _, ok := RegisteredTasks[name]; !ok {
			unknown = append(unknown, name)
		}
	}
	sort.Strings(missing)
	sort.Strings(unknown)

	if len(missing) > 0 {
		t.Errorf("%s does not account for %d registered task(s): %s\n"+
			"Add each one to the module mapping table (if an ansible-dokku module reaches it) "+
			"or to the %q section.",
			ansibleMappingDoc, len(missing), strings.Join(missing, ", "), noModuleHeading)
	}
	if len(unknown) > 0 {
		t.Errorf("%s names %d task(s) that are not registered: %s\n"+
			"Fix the typo, or drop the row if the task was removed.",
			ansibleMappingDoc, len(unknown), strings.Join(unknown, ", "))
	}
}
