package commands

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// serveRecipe starts an httptest server that returns body for any GET and
// returns the base URL plus a `/tasks.yml` recipe URL under it. The caller
// gets a URL whose path carries a .yml extension so detectTaskFileFormat
// resolves YAML.
func serveRecipe(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/yaml")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestReadTaskFileDataFetchesURL: readTaskFileData GETs an http(s) URL and
// returns the response body verbatim.
func TestReadTaskFileDataFetchesURL(t *testing.T) {
	const recipe = "---\n- tasks:\n    - name: x\n      dokku_app: { app: demo }\n"
	srv := serveRecipe(t, recipe)

	data, err := readTaskFileData(srv.URL + "/tasks.yml")
	if err != nil {
		t.Fatalf("readTaskFileData(url) error: %v", err)
	}
	if string(data) != recipe {
		t.Errorf("fetched body = %q, want %q", string(data), recipe)
	}
}

// TestReadTaskFileDataURLNon2xx: a non-2xx response is a clear error that
// names the status and the URL rather than a silent empty read.
func TestReadTaskFileDataURLNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	url := srv.URL + "/missing.yml"
	_, err := readTaskFileData(url)
	if err == nil {
		t.Fatal("expected an error for a 404 response")
	}
	if !strings.Contains(err.Error(), "unexpected status") || !strings.Contains(err.Error(), "404") {
		t.Errorf("error = %q, want it to mention the 404 status", err.Error())
	}
	if !strings.Contains(err.Error(), url) {
		t.Errorf("error = %q, want it to name the URL", err.Error())
	}
}

// TestReadTaskFileDataLocalFile: a non-URL path still reads from disk.
func TestReadTaskFileDataLocalFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	const body = "---\n- tasks: []\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	data, err := readTaskFileData(path)
	if err != nil {
		t.Fatalf("readTaskFileData(path) error: %v", err)
	}
	if string(data) != body {
		t.Errorf("read body = %q, want %q", string(data), body)
	}
}

// TestReadTaskFileDataLocalMissing: a missing local path surfaces the
// familiar os.ReadFile error (not a URL fetch error).
func TestReadTaskFileDataLocalMissing(t *testing.T) {
	_, err := readTaskFileData(filepath.Join(t.TempDir(), "nope.yml"))
	if err == nil {
		t.Fatal("expected an error for a missing local file")
	}
	if strings.Contains(err.Error(), "fetch ") {
		t.Errorf("missing local file should not report a fetch error: %v", err)
	}
}

// TestDetectTaskFileFormatURL: URLs resolve their format from the path
// component so a trailing query string does not corrupt the extension.
func TestDetectTaskFileFormatURL(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"http://h/docket/example.yml", taskFileFormatYAML},
		{"https://h/recipes/tasks.yaml", taskFileFormatYAML},
		{"http://h/tasks.json", taskFileFormatJSON5},
		{"http://h/tasks.json5", taskFileFormatJSON5},
		{"http://h/tasks.json?ref=main", taskFileFormatJSON5},
		{"https://h/a/b/tasks.yml?token=abc#frag", taskFileFormatYAML},
		{"http://h/tasks.json?download=recipe.yml", taskFileFormatJSON5},
	}
	for _, tc := range cases {
		if got := detectTaskFileFormat(tc.path); got != tc.want {
			t.Errorf("detectTaskFileFormat(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

// TestPlanFetchesRecipeFromURL: plan reads a recipe served over HTTP and
// runs it to completion (a read failure would exit 1 with "read error").
func TestPlanFetchesRecipeFromURL(t *testing.T) {
	defer stubReset()
	stubSet("url-plan", StubFixture{})

	srv := serveRecipe(t, `---
- tasks:
    - name: stub task
      dokku_stub:
        key: url-plan
`)

	stdout, stderr, exit := runPlan(t, srv.URL+"/tasks.yml")
	if exit != 0 {
		t.Fatalf("plan over URL exit = %d, want 0; stderr=%s", exit, stderr)
	}
	if !strings.Contains(stdout, "==> Play:") || !strings.Contains(stdout, "Plan:") {
		t.Errorf("plan output did not complete a run over URL:\n%s", stdout)
	}
}

// TestApplyURLRecipePreregistersInputFlags: a URL recipe's own inputs are
// pre-registered as flags (which requires FlagSet fetching the URL), so an
// --input override both parses and propagates into the rendered recipe.
// The stub key resolves to the overridden app value; only the "override"
// key carries a changed fixture, so a propagated override yields 1 changed.
func TestApplyURLRecipePreregistersInputFlags(t *testing.T) {
	defer stubReset()
	stubSet("override", StubFixture{Changed: true})

	srv := serveRecipe(t, `---
- inputs:
    - { name: app, default: baseline }
  tasks:
    - name: stub task
      dokku_stub:
        key: "{{ .app }}"
`)

	stdout, stderr, exit := runApply(t, srv.URL+"/tasks.yml", "--app=override")
	if exit != 0 {
		t.Fatalf("apply over URL with --app override exit = %d, want 0; stderr=%s", exit, stderr)
	}
	if !strings.Contains(stdout, "1 changed") {
		t.Errorf("expected the --app override to propagate (1 changed); output:\n%s", stdout)
	}
}
