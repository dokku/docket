package tasks

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlanResultCommandsContractStatic walks every *_task.go (and the shared
// helper files properties.go / toggle.go / resources.go) in the tasks package
// and asserts that every PlanResult composite literal carrying a non-empty
// `apply:` field also sets `Commands:`. Catches future tasks added without
// populating Commands when drift is reported - a regression would silently
// drop the `commands` array from `docket plan --json` output.
//
// The check is purely structural and runs without a Dokku server, complementing
// the integration tests in plan_integration_test.go that exercise Plan() end
// to end against a real server.
func TestPlanResultCommandsContractStatic(t *testing.T) {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatalf("glob: %v", err)
	}

	fs := token.NewFileSet()
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		base := filepath.Base(path)
		if !strings.HasSuffix(base, "_task.go") &&
			base != "properties.go" && base != "toggle.go" && base != "resources.go" {
			continue
		}

		f, err := parser.ParseFile(fs, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatalf("parse %s: %v", base, err)
		}

		ast.Inspect(f, func(n ast.Node) bool {
			lit, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			id, ok := lit.Type.(*ast.Ident)
			if !ok || id.Name != "PlanResult" {
				return true
			}
			hasApply := false
			hasCommands := false
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				key, ok := kv.Key.(*ast.Ident)
				if !ok {
					continue
				}
				switch key.Name {
				case "apply":
					hasApply = true
				case "Commands":
					hasCommands = true
				}
			}
			if hasApply && !hasCommands {
				pos := fs.Position(lit.Pos())
				t.Errorf("%s:%d: PlanResult composite literal sets `apply:` but not `Commands:` - "+
					"populate Commands via resolveCommands(inputs) so `docket plan --json` "+
					"reports the dokku command(s) apply would run.",
					filepath.Base(pos.Filename), pos.Line)
			}
			return true
		})
	}
}
