package tasks

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

// TestEveryPlanRecordsItsTask asserts the Plan of every registered task defers
// recordPlanned right after its gatePlan check, so every PlanResult it returns
// carries the planned task ExecutePlan registers secrets from. gatePlan's own
// refusal carries it through refusedPlan.
func TestEveryPlanRecordsItsTask(t *testing.T) {
	t.Parallel()
	registered := map[string]bool{}
	for _, task := range allRegisteredTasks() {
		registered[taskStructOf(task).Name()] = true
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	seen := map[string]bool{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "Plan" || len(fn.Recv.List) != 1 {
				continue
			}
			ident, ok := fn.Recv.List[0].Type.(*ast.Ident)
			if !ok || !registered[ident.Name] {
				continue
			}
			seen[ident.Name] = true
			if !defersRecordPlanned(fn) {
				t.Errorf("%s.Plan does not defer recordPlanned(&result, t) after its gatePlan check", ident.Name)
			}
		}
	}
	for name := range registered {
		if !seen[name] {
			t.Errorf("no value-receiver Plan method found for registered task %s", name)
		}
	}
}

// defersRecordPlanned reports whether fn's second statement is
// `defer recordPlanned(&result, t)`, where result is fn's named result and t
// its receiver.
func defersRecordPlanned(fn *ast.FuncDecl) bool {
	if fn.Body == nil || len(fn.Body.List) < 2 || fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
		return false
	}
	results := fn.Type.Results.List[0].Names
	receivers := fn.Recv.List[0].Names
	if len(results) != 1 || len(receivers) != 1 {
		return false
	}
	stmt, ok := fn.Body.List[1].(*ast.DeferStmt)
	if !ok || len(stmt.Call.Args) != 2 {
		return false
	}
	if name, ok := stmt.Call.Fun.(*ast.Ident); !ok || name.Name != "recordPlanned" {
		return false
	}
	addr, ok := stmt.Call.Args[0].(*ast.UnaryExpr)
	if !ok || addr.Op != token.AND {
		return false
	}
	if result, ok := addr.X.(*ast.Ident); !ok || result.Name != results[0].Name {
		return false
	}
	recv, ok := stmt.Call.Args[1].(*ast.Ident)
	return ok && recv.Name == receivers[0].Name
}

// TestPlanCarriesThePlannedTask checks the recorded task on each kind of
// result: a gatePlan refusal and a plan that ran.
func TestPlanCarriesThePlannedTask(t *testing.T) {
	t.Parallel()
	refused := HttpAuthUserTask{App: "api", State: StatePresent, Users: []HttpAuthUser{{Username: "admin", Password: "pw", Hash: "admin:$apr1$x"}}}
	if r := refused.Plan(context.Background()); r.Error == nil || !reflect.DeepEqual(r.planned, Task(refused)) {
		t.Errorf("refused plan: error %v, planned %#v, want an error carrying the task", r.Error, r.planned)
	}

	ctx := subprocess.ContextWithRunner(context.Background(), func(context.Context, subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		return subprocess.ExecCommandResponse{Stdout: "{}"}, nil
	})
	cfg := ConfigTask{App: "api", State: StatePresent, Config: map[string]string{"TOKEN": "pw"}}
	if r := cfg.Plan(ctx); r.Error != nil || !reflect.DeepEqual(r.planned, Task(cfg)) {
		t.Errorf("plan: error %v, planned %#v, want the task", r.Error, r.planned)
	}
}

// TestRegisterTaskSensitiveValuesWithoutAMasker checks a context with no
// masker, and a nil task, register nothing and do not panic.
func TestRegisterTaskSensitiveValuesWithoutAMasker(t *testing.T) {
	t.Parallel()
	registerTaskSensitiveValues(context.Background(), ConfigTask{Config: map[string]string{"TOKEN": "pw"}})

	masker := subprocess.NewMasker()
	ctx := subprocess.ContextWithMasker(context.Background(), masker)
	registerTaskSensitiveValues(ctx, nil)
	if got := masker.Values(); len(got) != 0 {
		t.Errorf("Values = %q after registering a nil task, want none", got)
	}
}

