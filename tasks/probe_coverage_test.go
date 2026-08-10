package tasks

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestEveryTaskDeclaresProbeSupport asserts that every registered task declares
// whether Plan() can read its current state via ProbeSupport(). This is what
// makes "which tasks never converge" answerable from code rather than from a
// hand-maintained prose list: adding a new task without a ProbeSupport()
// declaration fails the build here rather than silently shipping without a
// probe decision.
func TestEveryTaskDeclaresProbeSupport(t *testing.T) {
	for name, task := range RegisteredTasks {
		support, ok := TaskProbeSupport(task)
		if !ok {
			t.Errorf("task %q does not implement ProbeDocer (add a ProbeSupport() declaration)", name)
			continue
		}
		switch support.Status {
		case ProbeSupported, ProbePartial, ProbeUnsupported:
			// valid
		default:
			t.Errorf("task %q declares an unknown probe status %q", name, support.Status)
		}
		if support.Status != ProbeSupported && support.Caveat == "" {
			t.Errorf("task %q is %q but has no caveat naming what cannot be read", name, support.Status)
		}
	}
}

// TestProbeSupportMatchesPlanWiring asserts that a task's declared probe
// support and its actual Plan() agree. The declaration would otherwise be
// decorative - the plan and apply paths never read it - which is exactly how
// dokku_service_property came to behave identically to the three tasks the docs
// named while being absent from every list (#426). The invariant is:
//
//   - ProbeUnsupported means Plan() can never report in sync.
//   - ProbeSupported and ProbePartial mean it can.
//
// "Can report in sync" is decided statically: a PlanResult composite literal
// with `InSync: true` reachable from the task's Plan() through intra-package
// calls. ProbePartial is not distinguishable from ProbeSupported this way -
// both converge for some inputs - so the caveat text stays a human judgement,
// but the always-drift case is machine-checked in both directions.
func TestProbeSupportMatchesPlanWiring(t *testing.T) {
	g := buildPlanGraph(t)

	for name, task := range RegisteredTasks {
		support, ok := TaskProbeSupport(task)
		if !ok {
			continue // TestEveryTaskDeclaresProbeSupport reports this
		}

		typ := reflect.TypeOf(task)
		for typ.Kind() == reflect.Ptr {
			typ = typ.Elem()
		}
		key := planFuncKey(typ.Name() + ".Plan")
		if !g.known(key) {
			t.Errorf("task %q: no Plan() method found for type %s while walking the tasks package", name, typ.Name())
			continue
		}

		capable := g.inSyncCapable(key)
		switch support.Status {
		case ProbeUnsupported:
			if capable {
				t.Errorf("task %q declares probe status %q but its Plan() can return an in-sync result, "+
					"so it does converge - declare %q or %q instead",
					name, support.Status, ProbeSupported, ProbePartial)
			}
		case ProbeSupported, ProbePartial:
			if !capable {
				t.Errorf("task %q declares probe status %q but no `InSync: true` result is reachable from its Plan(), "+
					"so it plans as drift on every run - declare %q instead",
					name, support.Status, ProbeUnsupported)
			}
		}
	}
}

// TestPlanResultInSyncImpliesStatusOK asserts that every `InSync: true`
// PlanResult also sets `Status: PlanStatusOK`. TestProbeSupportMatchesPlanWiring
// treats the two as the same statement about a task; this keeps them from
// drifting apart.
func TestPlanResultInSyncImpliesStatusOK(t *testing.T) {
	fs := token.NewFileSet()
	for _, path := range packageSourceFiles(t) {
		f, err := parser.ParseFile(fs, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", filepath.Base(path), err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok || !isIdent(lit.Type, "PlanResult") {
				return true
			}
			if !litHasIdentField(lit, "InSync", "true") {
				return true
			}
			if !litHasIdentField(lit, "Status", "PlanStatusOK") {
				pos := fs.Position(lit.Pos())
				t.Errorf("%s:%d: PlanResult sets `InSync: true` without `Status: PlanStatusOK`",
					filepath.Base(pos.Filename), pos.Line)
			}
			return true
		})
	}
}

// planFuncKey identifies a function the static analysis can reach: a
// package-level func ("planProperty") or a method ("GitAuthTask.Plan").
// Methods must be keyed by receiver type - every task's Execute() is
// `return ExecutePlan(t.Plan())`, so a bare "Plan" key would merge all 73
// method bodies into one node and the invariant would hold vacuously.
type planFuncKey string

// planGraph is the intra-package call graph restricted to functions that
// return a PlanResult. Restricting edges by return type is what excludes the
// `apply:` closures: they return TaskOutputState and call runExecInputs /
// applyPropertySet / applyToggle, none of which is a plan path.
type planGraph struct {
	inSync   map[planFuncKey]bool
	calls    map[planFuncKey][]planFuncKey
	planFunc map[string]bool
	memo     map[planFuncKey]bool
}

func (g *planGraph) known(k planFuncKey) bool {
	_, ok := g.calls[k]
	return ok
}

// inSyncCapable reports whether an `InSync: true` PlanResult is reachable from
// k. Memoized, and cycle-guarded via the memo table so mutual recursion
// terminates.
func (g *planGraph) inSyncCapable(k planFuncKey) bool {
	if v, ok := g.memo[k]; ok {
		return v
	}
	g.memo[k] = false // provisional: breaks cycles
	if g.inSync[k] {
		g.memo[k] = true
		return true
	}
	for _, callee := range g.calls[k] {
		if g.inSyncCapable(callee) {
			g.memo[k] = true
			return true
		}
	}
	return g.memo[k]
}

// packageSourceFiles returns every non-test .go file in the tasks package
// directory. Deliberately unfiltered: the in-sync results live in
// properties.go, toggle.go, pairs.go, resources.go, scheduler_k3s_scoped_pairs.go
// and scheduler_k3s_autoscaling_auth.go as well as in the *_task.go files, so
// any whitelist would miss most of the graph.
func packageSourceFiles(t *testing.T) []string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	all, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	var out []string
	for _, path := range all {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

// buildPlanGraph parses the tasks package and records, for every function
// returning a PlanResult, whether its body builds an in-sync result and which
// other plan-returning functions it calls.
func buildPlanGraph(t *testing.T) *planGraph {
	t.Helper()

	files := packageSourceFiles(t)
	fs := token.NewFileSet()
	parsed := make([]*ast.File, 0, len(files))
	for _, path := range files {
		f, err := parser.ParseFile(fs, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", filepath.Base(path), err)
		}
		parsed = append(parsed, f)
	}

	g := &planGraph{
		inSync:   map[planFuncKey]bool{},
		calls:    map[planFuncKey][]planFuncKey{},
		planFunc: map[string]bool{},
		memo:     map[planFuncKey]bool{},
	}

	// Pass 1: name every plan-returning function.
	type decl struct {
		key  planFuncKey
		body *ast.BlockStmt
	}
	var decls []decl
	for _, f := range parsed {
		for _, d := range f.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Body == nil || !returnsPlanResult(fn) {
				continue
			}
			if fn.Recv == nil {
				g.planFunc[fn.Name.Name] = true
				decls = append(decls, decl{key: planFuncKey(fn.Name.Name), body: fn.Body})
				continue
			}
			recv := receiverTypeName(fn)
			if recv == "" {
				t.Errorf("%s: cannot resolve receiver type for method %s returning PlanResult",
					filepath.Base(fs.Position(fn.Pos()).Filename), fn.Name.Name)
				continue
			}
			// The graph only follows bare identifiers, so a plan-returning
			// method other than Plan() would be an unfollowed edge and would
			// make a task look unable to converge. Fail with a pointed message
			// rather than letting an innocent task take the blame.
			if fn.Name.Name != "Plan" {
				t.Errorf("%s.%s returns PlanResult; only Plan() may be a plan-returning method, "+
					"otherwise TestProbeSupportMatchesPlanWiring cannot follow the call. "+
					"Make it a package-level func instead.", recv, fn.Name.Name)
				continue
			}
			decls = append(decls, decl{key: planFuncKey(recv + ".Plan"), body: fn.Body})
		}
	}

	// Pass 2: record in-sync results and call edges. ast.Inspect descends into
	// function literals, which is what covers the closures every task passes to
	// DispatchPlan.
	for _, d := range decls {
		if _, seen := g.calls[d.key]; !seen {
			g.calls[d.key] = nil
		}
		ast.Inspect(d.body, func(n ast.Node) bool {
			switch node := n.(type) {
			case *ast.CompositeLit:
				if isIdent(node.Type, "PlanResult") && litHasIdentField(node, "InSync", "true") {
					g.inSync[d.key] = true
				}
			case *ast.CallExpr:
				if id, ok := node.Fun.(*ast.Ident); ok && g.planFunc[id.Name] {
					g.calls[d.key] = append(g.calls[d.key], planFuncKey(id.Name))
				}
			}
			return true
		})
	}

	return g
}

// returnsPlanResult reports whether fn's result list is exactly one PlanResult.
func returnsPlanResult(fn *ast.FuncDecl) bool {
	res := fn.Type.Results
	if res == nil || len(res.List) != 1 || len(res.List[0].Names) > 1 {
		return false
	}
	return isIdent(res.List[0].Type, "PlanResult")
}

// receiverTypeName returns fn's receiver type name, dereferencing a pointer
// receiver so a future `func (t *FooTask) Plan()` still resolves.
func receiverTypeName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return ""
	}
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if id, ok := expr.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

func isIdent(expr ast.Expr, name string) bool {
	id, ok := expr.(*ast.Ident)
	return ok && id.Name == name
}

// litHasIdentField reports whether lit sets field to the bare identifier want
// (`InSync: true`, `Status: PlanStatusOK`).
func litHasIdentField(lit *ast.CompositeLit, field, want string) bool {
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok || !isIdent(kv.Key, field) {
			continue
		}
		if isIdent(kv.Value, want) {
			return true
		}
	}
	return false
}
