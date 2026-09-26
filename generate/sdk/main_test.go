package main

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/tasks"

	"github.com/aymanbagabas/go-udiff"
)

// generatedFile is the committed output, relative to this package. `go
// generate` writes it from the sdk directory.
const generatedFile = "../../sdk/types_gen.go"

// TestGeneratedSDKIsCurrent asserts that sdk/types_gen.go is exactly what the
// generator would write today, so a task added without regenerating fails here
// rather than shipping a task the sdk cannot name.
func TestGeneratedSDKIsCurrent(t *testing.T) {
	committed, err := os.ReadFile(generatedFile)
	if err != nil {
		t.Fatalf("read %s: %v (run make generate)", generatedFile, err)
	}
	rendered, err := render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if string(committed) == string(rendered) {
		return
	}
	diff, err := udiff.ToUnified(generatedFile, generatedFile+" (regenerated)", string(committed), udiff.Strings(string(committed), string(rendered)), udiff.DefaultContextLines)
	if err != nil {
		t.Fatalf("%s is out of date and the diff could not be rendered: %v", generatedFile, err)
	}
	t.Errorf("%s is out of date; run make generate\n%s", generatedFile, diff)
}

// TestRenderAliasesEveryTask asserts that every registered task type gets an
// alias, which is what makes sdk.As and a NewTask type assertion usable from
// outside the module.
func TestRenderAliasesEveryTask(t *testing.T) {
	rendered, err := render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, typeKey := range tasks.TaskTypes() {
		task, _ := tasks.Lookup(typeKey)
		name := reflect.TypeOf(task).Elem().Name()
		if want := "type " + name + " = tasks." + name + "\n"; !strings.Contains(string(rendered), want) {
			t.Errorf("%s: missing %q", typeKey, strings.TrimSpace(want))
		}
	}
}

// TestCollectFollowsFields covers the walk itself: a named engine type reached
// only through a slice field is collected, while fields that are unexported
// or have no engine type are not followed.
func TestCollectFollowsFields(t *testing.T) {
	seen := map[reflect.Type]bool{}
	collect(reflect.TypeOf(tasks.PortsTask{}), seen)
	if !seen[reflect.TypeOf(tasks.PortMapping{})] {
		t.Errorf("PortMapping, reached through PortsTask.PortMappings, was not collected")
	}
	if !seen[reflect.TypeOf(tasks.State(""))] {
		t.Errorf("State, reached through PortsTask.State, was not collected")
	}

	seen = map[reflect.Type]bool{}
	collect(reflect.TypeOf(tasks.ExportedTask{}), seen)
	if len(seen) != 1 {
		t.Errorf("ExportedTask has only a string and an interface{} field, want 1 collected type, got %v", seen)
	}

	seen = map[reflect.Type]bool{}
	collect(reflect.TypeOf(struct{ s tasks.State }{}), seen)
	if len(seen) != 0 {
		t.Errorf("an unnamed struct with only an unexported field should collect nothing, got %v", seen)
	}
}
