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

// TestAliasedMethodsUseOnlyAliasedTypes asserts that no exported method on a
// type the sdk aliases takes or returns an engine type the sdk does not alias.
// Such a method shows up on the sdk type, but a caller outside the module can
// never name its argument or result, so it is on the public surface without
// being usable or covered by the stability promise. The fix is to unexport the
// method, as was done for withExecResult, examples and propertyTable.
func TestAliasedMethodsUseOnlyAliasedTypes(t *testing.T) {
	aliased, err := surface()
	if err != nil {
		t.Fatalf("surface: %v", err)
	}

	for owner := range aliased {
		receivers := []reflect.Type{owner}
		if owner.Kind() != reflect.Interface {
			receivers = append(receivers, reflect.PointerTo(owner))
		}
		for _, receiver := range receivers {
			for i := 0; i < receiver.NumMethod(); i++ {
				method := receiver.Method(i)
				if !method.IsExported() {
					continue
				}
				// A concrete type's method value takes the receiver first;
				// an interface's does not.
				first := 1
				if receiver.Kind() == reflect.Interface {
					first = 0
				}
				for _, leaked := range unaliased(method.Type, first, aliased) {
					t.Errorf("%s.%s uses %s, which the sdk does not alias", receiver, method.Name, leaked)
				}
			}
		}
	}
}

// unaliased returns the engine types in fn's parameters (from index first) and
// results that are not in aliased.
func unaliased(fn reflect.Type, first int, aliased map[reflect.Type]bool) []reflect.Type {
	var out []reflect.Type
	var visit func(reflect.Type)
	visit = func(t reflect.Type) {
		// A named type is either aliased or not; its underlying type is the
		// sdk's concern only through collect.
		if t.Name() != "" {
			if _, engine := aliasedPackages[t.PkgPath()]; engine && !aliased[t] {
				out = append(out, t)
			}
			return
		}
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Chan:
			visit(t.Elem())
		case reflect.Map:
			visit(t.Key())
			visit(t.Elem())
		case reflect.Func:
			for i := 0; i < t.NumIn(); i++ {
				visit(t.In(i))
			}
			for i := 0; i < t.NumOut(); i++ {
				visit(t.Out(i))
			}
		}
	}
	for i := first; i < fn.NumIn(); i++ {
		visit(fn.In(i))
	}
	for i := 0; i < fn.NumOut(); i++ {
		visit(fn.Out(i))
	}
	return out
}

// TestUnaliasedFindsEngineTypes covers the scan itself, since the real surface
// is expected to pass it and so never exercises the failure path.
func TestUnaliasedFindsEngineTypes(t *testing.T) {
	aliased := map[reflect.Type]bool{reflect.TypeOf(tasks.State("")): true}

	leaky := reflect.TypeOf(func(tasks.State, []tasks.Doc, func(*tasks.PropertyTable)) map[string]tasks.Recipe { return nil })
	got := unaliased(leaky, 0, aliased)
	want := []reflect.Type{reflect.TypeOf(tasks.Doc{}), reflect.TypeOf(tasks.PropertyTable{}), reflect.TypeOf(tasks.Recipe{})}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unaliased = %v, want %v", got, want)
	}

	clean := reflect.TypeOf(func(tasks.State, string) error { return nil })
	if got := unaliased(clean, 0, aliased); len(got) != 0 {
		t.Errorf("unaliased = %v, want none", got)
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
	collect(reflect.TypeOf(tasks.Recipe{}), seen)
	if !seen[reflect.TypeOf(tasks.Recipe{})] || !seen[reflect.TypeOf(tasks.RecipeEntry{})] {
		t.Errorf("a named slice should collect itself and its element, got %v", seen)
	}

	seen = map[reflect.Type]bool{}
	collect(reflect.TypeOf(struct{ s tasks.State }{}), seen)
	if len(seen) != 0 {
		t.Errorf("an unnamed struct with only an unexported field should collect nothing, got %v", seen)
	}
}
