package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/tasks"

	"github.com/aymanbagabas/go-udiff"
	"golang.org/x/tools/go/packages"
)

// TestGeneratedSDKIsCurrent asserts that every generated sdk file is exactly
// what the generator would write today, so a task or an `//sdk:export` added
// without regenerating fails here rather than shipping a surface the sdk does
// not have.
func TestGeneratedSDKIsCurrent(t *testing.T) {
	rendered, err := render()
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, name := range []string{funcsFile, typesFile} {
		path := "../../sdk/" + name
		committed, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v (run make generate)", path, err)
		}
		if string(committed) == string(rendered[name]) {
			continue
		}
		diff, err := udiff.ToUnified(path, path+" (regenerated)", string(committed), udiff.Strings(string(committed), string(rendered[name])), udiff.DefaultContextLines)
		if err != nil {
			t.Fatalf("%s is out of date and the diff could not be rendered: %v", path, err)
		}
		t.Errorf("%s is out of date; run make generate\n%s", path, diff)
	}
}

// TestTypesByNameRefusesASharedName asserts that two aliased types sharing a
// name are refused, naming both packages in the same order every run.
func TestTypesByNameRefusesASharedName(t *testing.T) {
	url := types.NewPackage("net/url", "url")
	exec := types.NewPackage("os/exec", "exec")
	seen := map[*types.TypeName]bool{
		types.NewTypeName(token.NoPos, exec, "Error", nil): true,
		types.NewTypeName(token.NoPos, url, "Error", nil):  true,
		types.NewTypeName(token.NoPos, url, "URL", nil):    true,
		types.NewTypeName(token.NoPos, exec, "Cmd", nil):   true,
	}
	want := "type name Error is declared in both net/url and os/exec"
	for i := 0; i < 20; i++ {
		_, err := typesByName(seen)
		if err == nil || err.Error() != want {
			t.Fatalf("error = %v, want %q", err, want)
		}
	}
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
		if want := "type " + name + " = tasks." + name + "\n"; !strings.Contains(string(rendered[typesFile]), want) {
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
	s, err := buildSurface()
	if err != nil {
		t.Fatalf("surface: %v", err)
	}

	for owner := range s.aliased {
		receivers := []types.Type{owner.Type()}
		if !types.IsInterface(owner.Type()) {
			receivers = append(receivers, types.NewPointer(owner.Type()))
		}
		for _, receiver := range receivers {
			methods := types.NewMethodSet(receiver)
			for i := 0; i < methods.Len(); i++ {
				method := methods.At(i).Obj()
				if !method.Exported() {
					continue
				}
				for _, leaked := range unaliased(method.Type(), s.aliased) {
					t.Errorf("%s.%s uses %s.%s, which the sdk does not alias", receiver, method.Name(), leaked.Pkg().Path(), leaked.Name())
				}
			}
		}
	}
}

// engineType looks up an exported type in the tasks package.
func engineType(t *testing.T, name string) *types.TypeName {
	t.Helper()
	pkgs, err := loadEngine()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	obj, ok := pkgs[tasksPackage].Types.Scope().Lookup(name).(*types.TypeName)
	if !ok {
		t.Fatalf("tasks.%s is not a type", name)
	}
	return obj
}

// TestUnaliasedFindsEngineTypes covers the scan itself, since the real surface
// is expected to pass it and so never exercises the failure path.
func TestUnaliasedFindsEngineTypes(t *testing.T) {
	state := engineType(t, "State")
	doc := engineType(t, "Doc")
	table := engineType(t, "PropertyTable")
	recipe := engineType(t, "Recipe")
	aliased := map[*types.TypeName]bool{state: true}

	param := func(typ types.Type) *types.Var { return types.NewParam(token.NoPos, nil, "", typ) }
	callback := types.NewSignatureType(nil, nil, nil, types.NewTuple(param(types.NewPointer(table.Type()))), nil, false)
	leaky := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(param(state.Type()), param(types.NewSlice(doc.Type())), param(callback)),
		types.NewTuple(param(types.NewMap(types.Typ[types.String], recipe.Type()))),
		false)
	got := unaliased(leaky, aliased)
	want := []*types.TypeName{doc, table, recipe}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("unaliased = %v, want %v", got, want)
	}

	clean := types.NewSignatureType(nil, nil, nil,
		types.NewTuple(param(state.Type()), param(types.Typ[types.String])),
		types.NewTuple(param(types.Universe.Lookup("error").Type())),
		false)
	if got := unaliased(clean, aliased); len(got) != 0 {
		t.Errorf("unaliased = %v, want none", got)
	}
}

// TestCollectFollowsFields covers the walk itself: a named engine type reached
// only through a slice field is collected, while fields that are unexported
// or have no engine type are not followed.
func TestCollectFollowsFields(t *testing.T) {
	seen := map[*types.TypeName]bool{}
	collect(engineType(t, "PortsTask").Type(), seen)
	if !seen[engineType(t, "PortMapping")] {
		t.Errorf("PortMapping, reached through PortsTask.PortMappings, was not collected")
	}
	if !seen[engineType(t, "State")] {
		t.Errorf("State, reached through PortsTask.State, was not collected")
	}

	seen = map[*types.TypeName]bool{}
	collect(engineType(t, "ExportedTask").Type(), seen)
	if len(seen) != 1 {
		t.Errorf("ExportedTask has only a string and an interface{} field, want 1 collected type, got %v", seen)
	}

	seen = map[*types.TypeName]bool{}
	collect(engineType(t, "Recipe").Type(), seen)
	if !seen[engineType(t, "Recipe")] || !seen[engineType(t, "RecipeEntry")] {
		t.Errorf("a named slice should collect itself and its element, got %v", seen)
	}

	seen = map[*types.TypeName]bool{}
	fields := []*types.Var{types.NewField(token.NoPos, nil, "s", engineType(t, "State").Type(), false)}
	collect(types.NewStruct(fields, nil), seen)
	if len(seen) != 0 {
		t.Errorf("an unnamed struct with only an unexported field should collect nothing, got %v", seen)
	}
}

// checkedSource type-checks src as an engine package, for testing directives
// without editing the real engine.
func checkedSource(t *testing.T, src string) *packages.Package {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "engine.go", src, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	info := &types.Info{Defs: map[*ast.Ident]types.Object{}}
	pkg, err := (&types.Config{}).Check(tasksPackage, fset, []*ast.File{file}, info)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	return &packages.Package{PkgPath: tasksPackage, Fset: fset, Syntax: []*ast.File{file}, Types: pkg, TypesInfo: info}
}

// exportedNames returns the sdk names src's directives export, by group.
func exportedNames(t *testing.T, src string) ([][]string, error) {
	t.Helper()
	groups, err := collectExports(map[string]*packages.Package{tasksPackage: checkedSource(t, src)})
	if err != nil {
		return nil, err
	}
	var out [][]string
	for _, g := range groups {
		var names []string
		for _, m := range g.members {
			names = append(names, m.name)
		}
		out = append(out, names)
	}
	return out, nil
}

// TestDirectiveScope asserts what a directive exports: a whole block from the
// block's doc, one spec from the spec's doc, and a renamed single name.
func TestDirectiveScope(t *testing.T) {
	cases := map[string]struct {
		src  string
		want [][]string
	}{
		"block": {
			src:  "package tasks\n\n// Kinds.\n//\n//sdk:export\nconst (\n\tA = 1\n\tB = 2\n)\n",
			want: [][]string{{"A", "B"}},
		},
		"one spec": {
			src:  "package tasks\n\nconst (\n\t// A is one.\n\t//\n\t//sdk:export\n\tA = 1\n\tB = 2\n)\n",
			want: [][]string{{"A"}},
		},
		"renamed single": {
			src:  "package tasks\n\n// Old is a name.\n//\n//sdk:export New\nconst Old = 1\n",
			want: [][]string{{"New"}},
		},
		"func and type": {
			src:  "package tasks\n\n// F runs.\n//\n//sdk:export\nfunc F() {}\n\n// T is a type.\n//\n//sdk:export\ntype T struct{}\n\nfunc G() {}\n",
			want: [][]string{{"F"}, {"T"}},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := exportedNames(t, tc.src)
			if err != nil {
				t.Fatalf("collect: %v", err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("exported %v, want %v", got, tc.want)
			}
		})
	}
}

// TestDirectiveRenamesMentions asserts that a renamed export's engine name is
// rewritten everywhere a doc mentions it, starting with its own doc.
func TestDirectiveRenamesMentions(t *testing.T) {
	src := "package tasks\n\n// Old is a name.\n//\n//sdk:export New\nconst Old = 1\n\n// F returns Old.\n//\n//sdk:export\nfunc F() int { return Old }\n"
	groups, err := collectExports(map[string]*packages.Package{tasksPackage: checkedSource(t, src)})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if got := groups[0].doc; got != "New is a name." {
		t.Errorf("const doc = %q, want %q", got, "New is a name.")
	}
	if got := groups[1].doc; got != "F returns New." {
		t.Errorf("func doc = %q, want %q", got, "F returns New.")
	}
}

// TestDirectiveErrors asserts that a directive the generator cannot honour
// fails generation rather than exporting something unexpected.
func TestDirectiveErrors(t *testing.T) {
	cases := map[string]struct {
		src  string
		want string
	}{
		"unexported": {
			src:  "package tasks\n\n// f runs.\n//\n//sdk:export\nfunc f() {}\n",
			want: "directive on unexported f",
		},
		"method": {
			src:  "package tasks\n\ntype T struct{}\n\n// M runs.\n//\n//sdk:export\nfunc (T) M() {}\n",
			want: "directive on method M",
		},
		"malformed": {
			src:  "package tasks\n\n// F runs.\n//\n//sdk:exported\nfunc F() {}\n",
			want: "malformed directive",
		},
		"renamed block": {
			src:  "package tasks\n\n// Kinds.\n//\n//sdk:export New\nconst (\n\tA = 1\n)\n",
			want: "covers more than one name",
		},
		"bad rename": {
			src:  "package tasks\n\n// F runs.\n//\n//sdk:export new\nfunc F() {}\n",
			want: "not an exported identifier",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := exportedNames(t, tc.src)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want one containing %q", err, tc.want)
			}
		})
	}
}

// TestCheckNamesRefusesACollision asserts that two exports landing on one sdk
// name fail generation, whether two directives collide or a directive renames
// onto an aliased type's name.
func TestCheckNamesRefusesACollision(t *testing.T) {
	src := "package tasks\n\n// A runs.\n//\n//sdk:export\nfunc A() {}\n\n// B runs.\n//\n//sdk:export A\nfunc B() {}\n"
	groups, err := collectExports(map[string]*packages.Package{tasksPackage: checkedSource(t, src)})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	err = checkNames(&surface{groups: groups}, nil)
	if err == nil || !strings.Contains(err.Error(), "sdk name A is exported by both") {
		t.Errorf("error = %v, want a collision on A", err)
	}

	src = "package tasks\n\n// Lifecycle is a type.\ntype Lifecycle struct{}\n\n// F runs.\n//\n//sdk:export Lifecycle\nfunc F() {}\n"
	pkg := checkedSource(t, src)
	groups, err = collectExports(map[string]*packages.Package{tasksPackage: pkg})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	typ := pkg.Types.Scope().Lookup("Lifecycle").(*types.TypeName)
	err = checkNames(&surface{groups: groups}, map[string]*types.TypeName{"Lifecycle": typ})
	if err == nil || !strings.Contains(err.Error(), "sdk name Lifecycle is exported by both type") {
		t.Errorf("error = %v, want a collision with the aliased type", err)
	}
}
