package tasks

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateDoesNotImportSubprocess is an integration-style guard that the
// validate code path stays offline. It parses validate.go's import block and
// asserts that the subprocess package is not referenced. If a future change
// pulls subprocess in (directly or transitively via a helper added to the
// tasks package), this test fails loudly.
//
// This is preferred over a runtime PATH-stripping check because Go's import
// graph is the ground truth: if subprocess is not in the import set,
// CallExecCommand cannot be invoked from within Validate.
func TestValidateDoesNotImportSubprocess(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("os.Getwd: %v", err)
	}
	path := filepath.Join(wd, "validate.go")

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("parser.ParseFile: %v", err)
	}

	for _, imp := range f.Imports {
		p := strings.Trim(imp.Path.Value, `"`)
		if strings.HasSuffix(p, "/subprocess") || p == "github.com/dokku/docket/subprocess" {
			t.Errorf("validate.go must not import subprocess (offline contract); found import %q", p)
		}
	}
}

// TestIntegrationValidateRunsOffline runs Validate against a multi-task
// fixture with PATH cleared so any accidental call to a `dokku` binary
// would fail and the validator must continue to succeed. This complements
// the import-graph guard above by exercising the actual function at
// runtime.
func TestIntegrationValidateRunsOffline(t *testing.T) {
	skipIfNotInShardT(t)
	original := os.Getenv("PATH")
	if err := os.Setenv("PATH", ""); err != nil {
		t.Fatalf("os.Setenv: %v", err)
	}
	defer os.Setenv("PATH", original)

	data := []byte(`---
- inputs:
    - name: app
      default: docket-validate-offline
  tasks:
    - name: create app
      dokku_app:
        app: {{ .app | default "" }}
    - name: set config
      dokku_config:
        app: {{ .app | default "" }}
        restart: false
        config:
          KEY: value
    - name: ensure storage
      dokku_storage_ensure:
        app: {{ .app | default "" }}
        chown: herokuish
`)

	problems := Validate(data, ValidateOptions{})
	if len(problems) != 0 {
		t.Fatalf("expected no problems with empty PATH, got: %+v", problems)
	}
}
