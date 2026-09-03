package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dokku/docket/tasks"
)

// vars_file_format_test.go covers how a --vars-file picks its decoder.
//
// That used to be a bare `filepath.Ext(path) == ".json"` compare sitting
// outside the format stack, so it never learned about .json5: a vars-file
// written in JSON5 - or simply named after a .json5 recipe, which is what
// deriveVarsOutput does - reached the YAML parser and died on the first
// comment or trailing comma. It now resolves through the codec registry
// (#519).

// TestParseVarsFileJSON5 is the behaviour change: a .json5 vars-file
// decodes as JSON5 instead of being handed to the YAML parser.
func TestParseVarsFileJSON5(t *testing.T) {
	t.Parallel()
	got, err := parseVarsFile("vars.json5", []byte("{\n  // the app to deploy\n  app: 'web',\n  replicas: 3,\n}\n"))
	if err != nil {
		t.Fatalf("parseVarsFile(.json5): %v", err)
	}
	if got["app"] != "web" {
		t.Errorf("app = %#v, want \"web\"", got["app"])
	}
	if got["replicas"] != float64(3) {
		t.Errorf("replicas = %#v, want 3", got["replicas"])
	}
}

// TestParseVarsFileJSONUnchanged pins what a plain .json vars-file has
// always decoded to. json5.Unmarshal is a superset of encoding/json, so
// the values - including the float64 a JSON number lands as - must not
// have moved.
func TestParseVarsFileJSONUnchanged(t *testing.T) {
	t.Parallel()
	got, err := parseVarsFile("vars.json", []byte(`{"app": "web", "replicas": 3, "debug": true}`))
	if err != nil {
		t.Fatalf("parseVarsFile(.json): %v", err)
	}
	want := map[string]interface{}{"app": "web", "replicas": float64(3), "debug": true}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %#v (%T), want %#v (%T)", k, got[k], got[k], v, v)
		}
	}
}

// TestParseVarsFileUnknownExtensionIsYAML pins the fallback. A vars-file
// is normally named after its recipe rather than after a format, so most
// of them carry an extension no codec claims - and every one of those has
// always been read as YAML.
func TestParseVarsFileUnknownExtensionIsYAML(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"vars.yml", "vars.yaml", "vars.txt", "vars", "tasks.vars.yml"} {
		got, err := parseVarsFile(path, []byte("app: web\nreplicas: 3\n"))
		if err != nil {
			t.Errorf("parseVarsFile(%q): %v", path, err)
			continue
		}
		if got["app"] != "web" || got["replicas"] != 3 {
			t.Errorf("parseVarsFile(%q) = %#v, want the YAML decoding", path, got)
		}
	}
}

// TestParseVarsFileErrorsNameThePath keeps the message shape: whatever the
// codec says, the user is told which --vars-file it was about.
func TestParseVarsFileErrorsNameThePath(t *testing.T) {
	t.Parallel()
	cases := map[string][]byte{
		"broken.json": []byte("{not: valid"),
		"broken.yml":  []byte("- one\n- two\n"),
	}
	for path, data := range cases {
		_, err := parseVarsFile(path, data)
		if err == nil {
			t.Errorf("parseVarsFile(%q) = nil error, want a rejection", path)
			continue
		}
		if !strings.Contains(err.Error(), "--vars-file "+path) {
			t.Errorf("error %q should name the flag and the path", err)
		}
	}
}

// TestVarsFileFollowsEveryCodec walks the registry so a new format's
// vars-file is exercised the moment it is registered, rather than whenever
// someone remembers to add a case here.
func TestVarsFileFollowsEveryCodec(t *testing.T) {
	t.Parallel()
	vars := map[string]interface{}{"app": "web"}
	for _, codec := range tasks.Codecs() {
		for _, ext := range codec.Extensions() {
			data, err := codec.MarshalVars(vars)
			if err != nil {
				t.Fatalf("%s MarshalVars: %v", codec.Name(), err)
			}
			got, err := parseVarsFile("vars."+ext, data)
			if err != nil {
				t.Errorf("parseVarsFile(vars.%s) written by %q: %v", ext, codec.Name(), err)
				continue
			}
			if got["app"] != "web" {
				t.Errorf("vars.%s written by %q read back as %#v", ext, codec.Name(), got)
			}
		}
	}
}

// TestVarsFileDerivedFromRecipeIsReadable closes the loop deriveVarsOutput
// opens: export names the vars-file after the recipe, so the extension it
// splices in has to be one parseVarsFile can decode with the same codec
// MarshalVars wrote it with.
func TestVarsFileDerivedFromRecipeIsReadable(t *testing.T) {
	t.Parallel()
	vars := map[string]interface{}{"app": "web"}
	for _, codec := range tasks.Codecs() {
		for _, ext := range codec.Extensions() {
			derived := deriveVarsOutput("tasks." + ext)
			data, err := codec.MarshalVars(vars)
			if err != nil {
				t.Fatalf("%s MarshalVars: %v", codec.Name(), err)
			}
			got, err := parseVarsFile(derived, data)
			if err != nil {
				t.Errorf("%q derived from tasks.%s is not readable: %v", derived, ext, err)
				continue
			}
			if got["app"] != "web" {
				t.Errorf("%q read back as %#v", derived, got)
			}
		}
	}
}

// TestVarsFileJSON5RecipePairEndToEnd is the reason the change is not just
// tidying: `docket export --format json5 --output tasks.json5` derives
// tasks.vars.json5, and reading that pair back must work.
func TestVarsFileJSON5RecipePairEndToEnd(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json5")
	recipe := []byte("[\n  {\n    // deploy the app\n    inputs: [{ name: 'app', default: 'web' }],\n    tasks: [{ dokku_app: { app: '{{ .app }}' } }],\n  },\n]\n")
	if err := os.WriteFile(path, recipe, 0o644); err != nil {
		t.Fatalf("write recipe: %v", err)
	}

	varsPath := filepath.Join(dir, deriveVarsOutput("tasks.json5"))
	if err := os.WriteFile(varsPath, []byte("{\n  // override the default\n  app: 'api',\n}\n"), 0o644); err != nil {
		t.Fatalf("write vars: %v", err)
	}

	stdout, stderr, exit := runApply(t, path, "--list-tasks", "--vars-file", varsPath)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "dokku_app[app=api]") {
		t.Errorf("the JSON5 vars-file did not reach the listing; got:\n%s", stdout)
	}
}
