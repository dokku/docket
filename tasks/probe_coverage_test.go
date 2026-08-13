package tasks

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
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
//   - ProbeSupported and ProbePartial mean every state it accepts can.
//
// "Can report in sync" is decided statically: a PlanResult composite literal
// with `InSync: true` reachable through intra-package calls. ProbePartial is
// not distinguishable from ProbeSupported this way - both converge - so the
// caveat text stays a human judgement, but the always-drift case is
// machine-checked in both directions.
//
// The converging direction is asked per `state:` branch, not per Plan() (#447).
// Almost every task dispatches on state and the branches are independent, so a
// whole-Plan() check would pass a task that probes `present` and not `absent` -
// `state: absent` would report drift forever while the `present` branch kept
// the method as a whole looking convergent. Branches reached through
// planProperty, planToggle and planResource count as the calling task's own,
// which is what covers the 34 tasks whose Plan() body has no DispatchPlan.
func TestProbeSupportMatchesPlanWiring(t *testing.T) {
	g := buildPlanGraph(t)

	// A shared helper's branches are reached by dozens of tasks, so a broken
	// one is reported once against the first task that reaches it rather than
	// once per task.
	type branchFailure struct {
		branch planBranch
		task   string
	}
	failures := map[planFuncKey]branchFailure{}

	for _, name := range sortedTaskNames() {
		task := RegisteredTasks[name]
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

		// Without a dispatch map there is nothing to check per state, and the
		// per-branch invariant would hold vacuously for the task. Every task
		// routes through DispatchPlan today, directly or via planProperty /
		// planToggle / planResource; hand-rolling the state switch instead
		// would quietly opt out of the check.
		branches := g.reachableBranches(key)
		if len(branches) == 0 {
			t.Errorf("task %q reaches no DispatchPlan branch from its Plan(), so the per-branch probe "+
				"check cannot see its states - dispatch on state with DispatchPlan", name)
			continue
		}

		capable := g.inSyncCapable(key)
		switch support.Status {
		case ProbeUnsupported:
			if !capable {
				continue
			}
			var converging []string
			for _, b := range branches {
				if g.inSyncCapable(b.key) {
					converging = append(converging, fmt.Sprintf("%s (%s:%d)",
						b.state, filepath.Base(b.pos.Filename), b.pos.Line))
				}
			}
			if len(converging) == 0 {
				t.Errorf("task %q declares probe status %q but its Plan() can return an in-sync result, "+
					"so it does converge - declare %q or %q instead",
					name, support.Status, ProbeSupported, ProbePartial)
				continue
			}
			t.Errorf("task %q declares probe status %q but its %s branch(es) can return an in-sync result, "+
				"so it does converge - declare %q or %q instead",
				name, support.Status, strings.Join(converging, ", "), ProbeSupported, ProbePartial)
		case ProbeSupported, ProbePartial:
			if !capable {
				t.Errorf("task %q declares probe status %q but no `InSync: true` result is reachable from its Plan(), "+
					"so it plans as drift on every run - declare %q instead",
					name, support.Status, ProbeUnsupported)
				continue
			}
			for _, b := range branches {
				if g.inSyncCapable(b.key) {
					continue
				}
				if _, seen := failures[b.key]; !seen {
					failures[b.key] = branchFailure{branch: b, task: name}
				}
			}
		}
	}

	keys := make([]planFuncKey, 0, len(failures))
	for k := range failures {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, k := range keys {
		f := failures[k]
		t.Errorf("%s:%d: the %q branch of %s has no reachable `InSync: true` result, so a task using "+
			"`state: %s` plans as drift on every run - probe that state, or the task cannot claim it converges "+
			"(reached from task %q)",
			filepath.Base(f.branch.pos.Filename), f.branch.pos.Line, f.branch.state, f.branch.owner,
			f.branch.state, f.task)
	}
}

// sortedTaskNames returns the registered task names in a stable order, so the
// task blamed for a shared helper's broken branch does not change run to run.
func sortedTaskNames() []string {
	names := make([]string, 0, len(RegisteredTasks))
	for name := range RegisteredTasks {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
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

// planFuncKey identifies a node the static analysis can reach: a package-level
// func ("planProperty"), a method ("GitAuthTask.Plan"), or one branch of a
// DispatchPlan map ("GitAuthTask.Plan[present]"). Methods must be keyed by
// receiver type - every task's Execute() is `return ExecutePlan(t.Plan())`, so
// a bare "Plan" key would merge all 73 method bodies into one node and the
// invariant would hold vacuously. Branch keys use a spelling no Go identifier
// can collide with.
type planFuncKey string

// planBranch is one entry of a `map[State]func() PlanResult` passed to
// DispatchPlan: the state a recipe would write in `state:`, the function the
// map literal lives in, and the node key the closure's body was recorded under.
type planBranch struct {
	key   planFuncKey
	owner planFuncKey
	state string
	pos   token.Position
}

// branchKey spells the node key for one dispatch branch.
func branchKey(owner planFuncKey, state string) planFuncKey {
	return planFuncKey(string(owner) + "[" + state + "]")
}

// planGraph is the intra-package call graph restricted to functions that
// return a PlanResult, plus one node per DispatchPlan branch. Restricting edges
// by return type is what excludes the `apply:` closures: they return
// TaskOutputState and call runExecInputs / applyPropertySet / applyToggle, none
// of which is a plan path.
//
// Each branch closure is its own node rather than part of its enclosing
// function, which is what lets the coverage test ask "can `state: absent`
// converge" instead of only "can this task converge at all". A branch is also
// recorded as a call edge of its enclosing function, so reachability for a
// whole function still means "some branch of it converges".
type planGraph struct {
	inSync   map[planFuncKey]bool
	calls    map[planFuncKey][]planFuncKey
	planFunc map[string]bool
	branches map[planFuncKey]planBranch
	memo     map[planFuncKey]bool
}

func (g *planGraph) known(k planFuncKey) bool {
	_, ok := g.calls[k]
	return ok
}

// reachableBranches returns every dispatch branch reachable from k, in source
// order. The walk crosses call edges, so a task delegating to planProperty,
// planToggle or planResource reports that helper's branches as its own - which
// is the only way the 34 tasks whose Plan() body has no DispatchPlan of its own
// get per-branch coverage.
func (g *planGraph) reachableBranches(k planFuncKey) []planBranch {
	seen := map[planFuncKey]bool{}
	var out []planBranch
	var walk func(planFuncKey)
	walk = func(node planFuncKey) {
		if seen[node] {
			return
		}
		seen[node] = true
		if b, ok := g.branches[node]; ok {
			out = append(out, b)
		}
		for _, callee := range g.calls[node] {
			walk(callee)
		}
	}
	walk(k)
	sort.Slice(out, func(i, j int) bool {
		if out[i].pos.Filename != out[j].pos.Filename {
			return out[i].pos.Filename < out[j].pos.Filename
		}
		return out[i].pos.Offset < out[j].pos.Offset
	})
	return out
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

// reaches reports whether target is reachable from k across call edges, which
// is how TestToggleTasksDeclareTheSharedFields asks whether a task's Plan()
// delegates to planToggle. Delegation is the only honest way to ask what shape a
// task is: keying on the `_toggle` name suffix instead would miss
// dokku_maintenance, a toggle that is not spelled like one.
func (g *planGraph) reaches(k, target planFuncKey) bool {
	seen := map[planFuncKey]bool{}
	var walk func(planFuncKey) bool
	walk = func(node planFuncKey) bool {
		if node == target {
			return true
		}
		if seen[node] {
			return false
		}
		seen[node] = true
		for _, callee := range g.calls[node] {
			if walk(callee) {
				return true
			}
		}
		return false
	}
	return walk(k)
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

// buildPlanGraph parses the tasks package and builds the plan graph over it,
// reporting every structural problem newPlanGraph found as a test failure.
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

	g, problems := newPlanGraph(fs, parsed)
	for _, p := range problems {
		t.Error(p)
	}
	return g
}

// newPlanGraph records, for every function carrying a PlanResult and every
// DispatchPlan branch inside one, whether its body builds an in-sync result and
// which other plan nodes it reaches. Structural problems - a shape the walk
// cannot follow, which would silently weaken the invariant or blame an innocent
// branch - are returned rather than reported, so the analysis can be exercised
// against synthetic sources.
func newPlanGraph(fs *token.FileSet, parsed []*ast.File) (*planGraph, []string) {
	g := &planGraph{
		inSync:   map[planFuncKey]bool{},
		calls:    map[planFuncKey][]planFuncKey{},
		planFunc: map[string]bool{},
		branches: map[planFuncKey]planBranch{},
		memo:     map[planFuncKey]bool{},
	}
	var problems []string

	states := stateConstants(parsed)

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
				problems = append(problems, fmt.Sprintf(
					"%s: cannot resolve receiver type for method %s returning PlanResult",
					filepath.Base(fs.Position(fn.Pos()).Filename), fn.Name.Name))
				continue
			}
			// The graph only follows bare identifiers, so a plan-returning
			// method other than Plan() would be an unfollowed edge and would
			// make a task look unable to converge. Fail with a pointed message
			// rather than letting an innocent task take the blame.
			if fn.Name.Name != "Plan" {
				problems = append(problems, fmt.Sprintf(
					"%s.%s returns PlanResult; only Plan() may be a plan-returning method, "+
						"otherwise TestProbeSupportMatchesPlanWiring cannot follow the call. "+
						"Make it a package-level func instead.", recv, fn.Name.Name))
				continue
			}
			decls = append(decls, decl{key: planFuncKey(recv + ".Plan"), body: fn.Body})
		}
	}

	// Pass 2: record in-sync results, call edges, and dispatch branches. Each
	// branch closure becomes its own node; the walk of the enclosing body skips
	// the closure so its in-sync result is attributed to the branch alone.
	for _, d := range decls {
		problems = append(problems, g.walkBody(fs, states, d.key, d.body)...)
	}

	return g, problems
}

// walkBody records one node's in-sync results, call edges and dispatch
// branches, recursing into each branch closure as a node of its own.
func (g *planGraph) walkBody(fs *token.FileSet, states map[string]string, key planFuncKey, body ast.Node) []string {
	if _, seen := g.calls[key]; !seen {
		g.calls[key] = nil
	}

	var problems []string
	branches, branchProblems := dispatchBranches(fs, states, key, body)
	problems = append(problems, branchProblems...)

	skip := map[ast.Node]bool{}
	for _, b := range branches {
		skip[b.lit] = true
	}

	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil || skip[n] {
			return false
		}
		switch node := n.(type) {
		case *ast.CompositeLit:
			if isIdent(node.Type, "PlanResult") && litHasIdentField(node, "InSync", "true") {
				g.inSync[key] = true
			}
		case *ast.CallExpr:
			if id, ok := node.Fun.(*ast.Ident); ok && g.planFunc[id.Name] {
				g.calls[key] = append(g.calls[key], planFuncKey(id.Name))
			}
		}
		return true
	})

	for _, b := range branches {
		g.branches[b.branch.key] = b.branch
		g.calls[key] = append(g.calls[key], b.branch.key)
		problems = append(problems, g.walkBody(fs, states, b.branch.key, b.lit.Body)...)
	}
	return problems
}

// dispatchBranchLit pairs a branch with the closure its body lives in.
type dispatchBranchLit struct {
	branch planBranch
	lit    *ast.FuncLit
}

// dispatchBranches finds every DispatchPlan call in body and returns one entry
// per state in its map. The map has to be readable statically: either inline in
// the call or a local assigned a single map literal. Anything else is reported
// rather than skipped - a branch the walk cannot see is a branch the coverage
// test would never check.
func dispatchBranches(fs *token.FileSet, states map[string]string, owner planFuncKey, body ast.Node) ([]dispatchBranchLit, []string) {
	var out []dispatchBranchLit
	var problems []string

	ast.Inspect(body, func(n ast.Node) bool {
		// Stop at closure boundaries: a branch's own body is walked as its own
		// node, so descending here would register a nested dispatch twice,
		// once under each owner.
		if _, ok := n.(*ast.FuncLit); ok {
			return false
		}
		call, ok := n.(*ast.CallExpr)
		if !ok || !isIdent(call.Fun, "DispatchPlan") || len(call.Args) != 2 {
			return true
		}
		where := fs.Position(call.Pos())
		lit, problem := resolveDispatchMap(fs, body, call.Args[1])
		if problem != "" {
			problems = append(problems, problem)
			return true
		}
		for _, elt := range lit.Elts {
			kv, ok := elt.(*ast.KeyValueExpr)
			if !ok {
				problems = append(problems, fmt.Sprintf(
					"%s:%d: DispatchPlan map entry in %s is not a `State: func() PlanResult` pair, "+
						"so the per-branch probe check cannot name the state it covers",
					filepath.Base(where.Filename), fs.Position(elt.Pos()).Line, owner))
				continue
			}
			fn, ok := kv.Value.(*ast.FuncLit)
			if !ok {
				problems = append(problems, fmt.Sprintf(
					"%s:%d: DispatchPlan map entry in %s is not a func literal; "+
						"write the branch inline so the per-branch probe check can walk it",
					filepath.Base(where.Filename), fs.Position(kv.Pos()).Line, owner))
				continue
			}
			state, ok := stateName(states, kv.Key)
			if !ok {
				problems = append(problems, fmt.Sprintf(
					"%s:%d: DispatchPlan map key in %s is not a State constant or string literal, "+
						"so the per-branch probe check cannot name the state it covers",
					filepath.Base(where.Filename), fs.Position(kv.Pos()).Line, owner))
				continue
			}
			out = append(out, dispatchBranchLit{
				branch: planBranch{
					key:   branchKey(owner, state),
					owner: owner,
					state: state,
					pos:   fs.Position(kv.Pos()),
				},
				lit: fn,
			})
		}
		return true
	})

	return out, problems
}

// resolveDispatchMap returns the map literal DispatchPlan was handed. An inline
// literal is the usual shape; a local holding exactly one map literal, never
// reassigned and never index-assigned, resolves to that literal so naming the
// map does not blind the check.
func resolveDispatchMap(fs *token.FileSet, scope ast.Node, arg ast.Expr) (*ast.CompositeLit, string) {
	where := filepath.Base(fs.Position(arg.Pos()).Filename)
	line := fs.Position(arg.Pos()).Line

	if lit, ok := arg.(*ast.CompositeLit); ok {
		return lit, ""
	}

	id, ok := arg.(*ast.Ident)
	if !ok {
		return nil, fmt.Sprintf("%s:%d: DispatchPlan was passed an expression the per-branch probe "+
			"check cannot resolve to a map literal; pass the map inline", where, line)
	}

	var found *ast.CompositeLit
	assignments := 0
	mutated := false
	ast.Inspect(scope, func(n ast.Node) bool {
		switch node := n.(type) {
		case *ast.AssignStmt:
			for i, lhs := range node.Lhs {
				if index, ok := lhs.(*ast.IndexExpr); ok && isIdent(index.X, id.Name) {
					mutated = true
					continue
				}
				if !isIdent(lhs, id.Name) {
					continue
				}
				assignments++
				if i < len(node.Rhs) {
					if lit, ok := node.Rhs[i].(*ast.CompositeLit); ok {
						found = lit
					}
				}
			}
		case *ast.ValueSpec:
			for i, name := range node.Names {
				if name.Name != id.Name {
					continue
				}
				assignments++
				if i < len(node.Values) {
					if lit, ok := node.Values[i].(*ast.CompositeLit); ok {
						found = lit
					}
				}
			}
		}
		return true
	})

	switch {
	case found == nil || assignments == 0:
		return nil, fmt.Sprintf("%s:%d: DispatchPlan was passed %q, which the per-branch probe check "+
			"cannot trace to a map literal in the same function; pass the map inline",
			where, line, id.Name)
	case assignments > 1 || mutated:
		return nil, fmt.Sprintf("%s:%d: the DispatchPlan map %q is assembled after its literal, so the "+
			"per-branch probe check cannot enumerate its states; build it in one map literal",
			where, line, id.Name)
	}
	return found, ""
}

// stateConstants maps each `State` constant to its value, so a branch keyed
// StateAbsent is reported as `absent` - what a recipe actually writes.
func stateConstants(parsed []*ast.File) map[string]string {
	out := map[string]string{}
	for _, f := range parsed {
		for _, d := range f.Decls {
			gen, ok := d.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs, ok := spec.(*ast.ValueSpec)
				if !ok || !isIdent(vs.Type, "State") {
					continue
				}
				for i, name := range vs.Names {
					if i >= len(vs.Values) {
						continue
					}
					if lit, ok := vs.Values[i].(*ast.BasicLit); ok && lit.Kind == token.STRING {
						if value, err := strconv.Unquote(lit.Value); err == nil {
							out[name.Name] = value
						}
					}
				}
			}
		}
	}
	return out
}

// stateName renders a dispatch map key as the state a recipe would write,
// falling back to the identifier for a key that is not a known constant. A key
// that is neither an identifier nor a string reports false: it cannot be named
// in a failure message, so the caller raises it as a structural problem.
func stateName(states map[string]string, key ast.Expr) (string, bool) {
	switch k := key.(type) {
	case *ast.Ident:
		if value, ok := states[k.Name]; ok {
			return value, true
		}
		return k.Name, true
	case *ast.BasicLit:
		if k.Kind == token.STRING {
			if value, err := strconv.Unquote(k.Value); err == nil {
				return value, true
			}
		}
	}
	return "", false
}

// returnsPlanResult reports whether any of fn's results is a PlanResult. A
// pointer or a multi-value return counts: readHttpAuthUserState hands one back
// as its third result, and a plan edge the graph does not follow would fail the
// per-branch check on an innocent branch.
func returnsPlanResult(fn *ast.FuncDecl) bool {
	res := fn.Type.Results
	if res == nil {
		return false
	}
	for _, field := range res.List {
		expr := field.Type
		if star, ok := expr.(*ast.StarExpr); ok {
			expr = star.X
		}
		if isIdent(expr, "PlanResult") {
			return true
		}
	}
	return false
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

// planGraphFixturePrologue gives every fixture the State constants the walk
// resolves branch keys against, so a branch keyed StatePresent is reported as
// `present` - what a recipe writes.
const planGraphFixturePrologue = `package tasks

const (
	StatePresent State = "present"
	StateAbsent  State = "absent"
)
`

// planGraphFixture builds the graph over a synthetic one-file package. The
// analysis is purely syntactic, so a fixture only needs the declarations the
// walk reads - it never has to compile.
func planGraphFixture(t *testing.T, body string) (*planGraph, []string) {
	t.Helper()
	fs := token.NewFileSet()
	f, err := parser.ParseFile(fs, "fixture.go", planGraphFixturePrologue+body, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return newPlanGraph(fs, []*ast.File{f})
}

// branchStates lists the states of every branch reachable from key, and the
// subset that cannot reach an in-sync result, both in source order.
func branchStates(g *planGraph, key planFuncKey) (all, nonConverging []string) {
	for _, b := range g.reachableBranches(key) {
		all = append(all, b.state)
		if !g.inSyncCapable(b.key) {
			nonConverging = append(nonConverging, b.state)
		}
	}
	return all, nonConverging
}

// TestPlanGraphBranchConvergence exercises the per-branch analysis that
// TestProbeSupportMatchesPlanWiring relies on. Every branch in the real package
// converges, so running against the package proves only that the check does not
// misfire - these fixtures are what prove it fires at all, and that it fires on
// the right state.
func TestPlanGraphBranchConvergence(t *testing.T) {
	const inSync = "PlanResult{InSync: true, Status: PlanStatusOK}"
	const drift = "PlanResult{InSync: false, Status: PlanStatusModify}"

	tests := []struct {
		name          string
		src           string
		key           planFuncKey
		all           []string
		nonConverging []string
	}{
		{
			name: "every branch converges",
			src: `
func (t FooTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return ` + inSync + ` },
		StateAbsent:  func() PlanResult { return ` + inSync + ` },
	})
}`,
			key: "FooTask.Plan",
			all: []string{"present", "absent"},
		},
		{
			name: "one branch never converges",
			src: `
func (t FooTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return ` + inSync + ` },
		StateAbsent:  func() PlanResult { return ` + drift + ` },
	})
}`,
			key:           "FooTask.Plan",
			all:           []string{"present", "absent"},
			nonConverging: []string{"absent"},
		},
		{
			name: "no branch converges",
			src: `
func (t FooTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return ` + drift + ` },
		StateAbsent:  func() PlanResult { return ` + drift + ` },
	})
}`,
			key:           "FooTask.Plan",
			all:           []string{"present", "absent"},
			nonConverging: []string{"present", "absent"},
		},
		{
			// The planProperty / planToggle / planResource shape: 34 tasks own
			// no dispatch map of their own and inherit the helper's branches.
			name: "branches inherited from a shared helper",
			src: `
func (t FooTask) Plan() PlanResult {
	return planShared(t.State)
}

func planShared(state State) PlanResult {
	return DispatchPlan(state, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return ` + inSync + ` },
		StateAbsent:  func() PlanResult { return ` + drift + ` },
	})
}`,
			key:           "FooTask.Plan",
			all:           []string{"present", "absent"},
			nonConverging: []string{"absent"},
		},
		{
			// A branch that delegates converges through the call edge.
			name: "branch converges through a called plan func",
			src: `
func (t FooTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planFooPresent(t) },
	})
}

func planFooPresent(t FooTask) PlanResult {
	return ` + inSync + `
}`,
			key: "FooTask.Plan",
			all: []string{"present"},
		},
		{
			// readHttpAuthUserState hands a PlanResult back as its third
			// result; a graph keyed on a lone PlanResult return would miss the
			// edge and blame the branch.
			name: "branch converges through a multi-result pointer helper",
			src: `
func (t FooTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			_, res := readFooState(t)
			if res != nil {
				return *res
			}
			return ` + drift + `
		},
	})
}

func readFooState(t FooTask) (bool, *PlanResult) {
	return true, &` + inSync + `
}`,
			key: "FooTask.Plan",
			all: []string{"present"},
		},
		{
			name: "dispatch map held in a local",
			src: `
func (t FooTask) Plan() PlanResult {
	handlers := map[State]func() PlanResult{
		StatePresent: func() PlanResult { return ` + inSync + ` },
		StateAbsent:  func() PlanResult { return ` + drift + ` },
	}
	return DispatchPlan(t.State, handlers)
}`,
			key:           "FooTask.Plan",
			all:           []string{"present", "absent"},
			nonConverging: []string{"absent"},
		},
		{
			name: "dispatch map declared with var",
			src: `
func (t FooTask) Plan() PlanResult {
	var handlers = map[State]func() PlanResult{
		StatePresent: func() PlanResult { return ` + inSync + ` },
		StateAbsent:  func() PlanResult { return ` + drift + ` },
	}
	return DispatchPlan(t.State, handlers)
}`,
			key:           "FooTask.Plan",
			all:           []string{"present", "absent"},
			nonConverging: []string{"absent"},
		},
		{
			// An unmapped key still names something a reader can act on.
			name: "keys that are not State constants fall back to their spelling",
			src: `
func (t FooTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		"deployed":   func() PlanResult { return ` + inSync + ` },
		StateUnknown: func() PlanResult { return ` + drift + ` },
	})
}`,
			key:           "FooTask.Plan",
			all:           []string{"deployed", "StateUnknown"},
			nonConverging: []string{"StateUnknown"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, problems := planGraphFixture(t, tc.src)
			if len(problems) > 0 {
				t.Fatalf("unexpected structural problems: %v", problems)
			}
			all, nonConverging := branchStates(g, tc.key)
			if !reflect.DeepEqual(all, tc.all) {
				t.Errorf("reachable branches: got %v, want %v", all, tc.all)
			}
			if !reflect.DeepEqual(nonConverging, tc.nonConverging) {
				t.Errorf("non-converging branches: got %v, want %v", nonConverging, tc.nonConverging)
			}
		})
	}
}

// TestPlanGraphReachesFunc exercises the delegation lookup that
// TestToggleTasksDeclareTheSharedFields uses to decide which tasks are toggles.
// Every planToggle caller in the real package is one, so running against the
// package proves only that the lookup does not over-report - these fixtures are
// what prove it finds a helper a hop away and, more importantly, that it says no.
func TestPlanGraphReachesFunc(t *testing.T) {
	const drift = "PlanResult{InSync: false, Status: PlanStatusModify}"

	tests := []struct {
		name string
		src  string
		want bool
	}{
		{
			name: "calls the helper directly",
			src: `
func (t FooTask) Plan() PlanResult {
	return planToggle(t.State, t.App)
}

func planToggle(state State, app string) PlanResult {
	return ` + drift + `
}`,
			want: true,
		},
		{
			name: "reaches the helper one hop away",
			src: `
func (t FooTask) Plan() PlanResult {
	return planFooToggle(t)
}

func planFooToggle(t FooTask) PlanResult {
	return planToggle(t.State, t.App)
}

func planToggle(state State, app string) PlanResult {
	return ` + drift + `
}`,
			want: true,
		},
		{
			name: "delegates elsewhere",
			src: `
func (t FooTask) Plan() PlanResult {
	return planProperty(t.State)
}

func planProperty(state State) PlanResult {
	return ` + drift + `
}

func planToggle(state State, app string) PlanResult {
	return ` + drift + `
}`,
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			g, problems := planGraphFixture(t, tc.src)
			if len(problems) > 0 {
				t.Fatalf("unexpected structural problems: %v", problems)
			}
			if got := g.reaches("FooTask.Plan", "planToggle"); got != tc.want {
				t.Errorf("reaches(FooTask.Plan, planToggle) = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestPlanGraphReportsUnwalkableShapes asserts the walk fails loudly on a shape
// it cannot follow. Silently skipping one would leave the branch unchecked
// while the suite stayed green, which is the failure mode the per-branch
// invariant exists to close.
func TestPlanGraphReportsUnwalkableShapes(t *testing.T) {
	const inSync = "PlanResult{InSync: true, Status: PlanStatusOK}"

	tests := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "map assembled after its literal",
			src: `
func (t FooTask) Plan() PlanResult {
	handlers := map[State]func() PlanResult{
		StatePresent: func() PlanResult { return ` + inSync + ` },
	}
	handlers[StateAbsent] = func() PlanResult { return ` + inSync + ` }
	return DispatchPlan(t.State, handlers)
}`,
			want: "assembled after its literal",
		},
		{
			name: "map reassigned",
			src: `
func (t FooTask) Plan() PlanResult {
	handlers := map[State]func() PlanResult{}
	handlers = map[State]func() PlanResult{
		StatePresent: func() PlanResult { return ` + inSync + ` },
	}
	return DispatchPlan(t.State, handlers)
}`,
			want: "assembled after its literal",
		},
		{
			name: "map comes from outside the function",
			src: `
func planShared(state State, handlers map[State]func() PlanResult) PlanResult {
	return DispatchPlan(state, handlers)
}`,
			want: "cannot trace to a map literal",
		},
		{
			name: "branch is not a func literal",
			src: `
func (t FooTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: presentBranch,
	})
}`,
			want: "not a func literal",
		},
		{
			name: "branch key is computed",
			src: `
func (t FooTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		State("present"): func() PlanResult { return ` + inSync + ` },
	})
}`,
			want: "not a State constant or string literal",
		},
		{
			name: "plan-returning method other than Plan",
			src: `
func (t FooTask) planPresent() PlanResult {
	return ` + inSync + `
}`,
			want: "only Plan() may be a plan-returning method",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, problems := planGraphFixture(t, tc.src)
			for _, p := range problems {
				if strings.Contains(p, tc.want) {
					return
				}
			}
			t.Errorf("expected a problem containing %q, got %v", tc.want, problems)
		})
	}
}
