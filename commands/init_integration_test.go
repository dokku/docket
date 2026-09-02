package commands

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestInitDoesNotImportSubprocess is a guard that the init code path stays
// offline. It parses init.go's import block and asserts that the
// subprocess package is not referenced. The issue's acceptance criteria
// state that init must not contact any subprocess; the package import
// graph is the ground truth.
func TestInitDoesNotImportSubprocess(t *testing.T) {
	t.Parallel()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	path := filepath.Join(wd, "init.go")

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parser.ParseFile: %v", err)
	}

	for _, imp := range f.Imports {
		p := strings.Trim(imp.Path.Value, `"`)
		if strings.HasSuffix(p, "/subprocess") || p == "github.com/dokku/docket/subprocess" {
			t.Errorf("init.go must not import subprocess (offline contract); found import %q", p)
		}
		if p == "os/exec" {
			t.Errorf("init.go must not import os/exec (offline contract); found import %q", p)
		}
	}
}
