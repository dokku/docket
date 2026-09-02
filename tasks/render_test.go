package tasks

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
)

// TestRenderTemplateLeavesTheEnvironmentAlone is the regression lock on #517.
// sigil.Execute exports template variables with os.Setenv and restores the
// environment through os.Clearenv, so rendering used to be destructive: two at
// once dropped variables for good - an unlocked concurrent render emptied the
// whole environment - and even one left a window where it was empty. docket
// renders without touching the environment at all now, so this asserts exact
// equality across concurrent renders and needs no serial phase to hold.
//
// The assertion is on the environment rather than on the rendered output
// because the corruption is the damage: a render that loses PATH has already
// broken every later command in the process, whatever it returned.
func TestRenderTemplateLeavesTheEnvironmentAlone(t *testing.T) {
	t.Parallel()
	before := snapshotEnv()

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Each goroutine renders with its own variable names, which is
			// what made the old interleaving destructive rather than merely
			// racy: a restore replayed a snapshot that never held the other's.
			for n := 0; n < 40; n++ {
				vars := map[string]interface{}{
					fmt.Sprintf("app_%d_%d", i, n):  "app",
					fmt.Sprintf("host_%d_%d", i, n): "host",
				}
				body := fmt.Sprintf("name: {{ .app_%d_%d }}-{{ .host_%d_%d }}\n", i, n, i, n)
				if _, err := RenderTemplate([]byte(body), vars, "test"); err != nil {
					t.Errorf("RenderTemplate: %v", err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	if after := snapshotEnv(); after != before {
		t.Errorf("concurrent renders changed the process environment\nlost: %v", lostKeys(before, after))
	}
}

// TestRenderTemplateDoesNotExportVarsToTheEnvironment is the direct lock on
// the write #517 is about. sigil.Execute called os.Setenv for every string
// variable so its POSIX preprocessor could see them, which is what made the
// restore - and the Clearenv in it - necessary in the first place.
//
// `var` is sigil's own builtin for reading the environment, so it observes
// exactly what a render exported. Under the old renderer this came back
// "leaked"; it must now come back empty.
func TestRenderTemplateDoesNotExportVarsToTheEnvironment(t *testing.T) {
	t.Parallel()
	const key = "docket_render_probe"
	if _, ok := os.LookupEnv(key); ok {
		t.Skipf("%s is set in the environment; the probe needs it absent", key)
	}

	out, err := RenderTemplate([]byte(`{{ var "`+key+`" }}`), map[string]interface{}{key: "leaked"}, "test")
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	if got := out.String(); got != "" {
		t.Errorf("render exported %q to the process environment; a render must not write it", got)
	}
	if v, ok := os.LookupEnv(key); ok {
		t.Errorf("%s survived the render as %q", key, v)
	}
}

// TestRenderTemplateKeepsSigilBuiltins pins the function map docket now owns.
// Sigil kept one package-global map filled by an init in `sigil/builtin`, so
// which builtins existed depended on whether something in the binary imported
// that package - the CLI did, the commands test binary did not. Losing one
// here would break recipes that no test in this package happens to use.
func TestRenderTemplateKeepsSigilBuiltins(t *testing.T) {
	t.Parallel()
	// Every name sigil/builtin registers, plus docket's own.
	names := []string{
		"include", "default", "var", "render",
		"capitalize", "lower", "upper", "replace", "trim", "indent", "match",
		"stdin", "substr", "base64enc", "base64dec",
		"file", "exists", "dir", "dirs", "files", "text",
		"sh", "httpget",
		"pointer", "json", "jmespath", "tojson", "yaml", "toyaml", "uniq",
		"drop", "append", "seq", "join", "joinkv", "split", "splitkv",
		"dq",
	}
	for _, name := range names {
		if _, ok := renderFuncs[name]; !ok {
			t.Errorf("template function %q is missing from renderFuncs", name)
		}
	}
	if got, want := len(renderFuncs), len(names); got != want {
		t.Errorf("renderFuncs holds %d functions, want %d; update this list when adding one", got, want)
	}
}

// TestRenderTemplateAppliesBuiltinsAndFilters is the behavioural half: the map
// having a key proves nothing about the template actually resolving it.
func TestRenderTemplateAppliesBuiltinsAndFilters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		vars map[string]interface{}
		want string
	}{
		{"builtin upper", `{{ .app | upper }}`, map[string]interface{}{"app": "api"}, "API"},
		{"builtin default fills an absent var", `{{ $missing | default "fallback" }}`, map[string]interface{}{}, "fallback"},
		{"docket dq escapes a quote", `{{ .app | dq }}`, map[string]interface{}{"app": `ab"cd`}, `ab\"cd`},
		{"nested render", `{{ render "x=1" "{{ .x }}" }}`, map[string]interface{}{}, "1"},
		{"plain interpolation", `{{ .app }}`, map[string]interface{}{"app": "api"}, "api"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, err := RenderTemplate([]byte(tt.body), tt.vars, "test")
			if err != nil {
				t.Fatalf("RenderTemplate: %v", err)
			}
			if got := out.String(); got != tt.want {
				t.Errorf("render = %q, want %q", got, tt.want)
			}
		})
	}
}

// snapshotEnv sorts because os.Environ()'s order is not stable across a
// Setenv/Unsetenv round trip, and a reordering is not the damage being pinned.
func snapshotEnv() string {
	env := os.Environ()
	sort.Strings(env)
	return strings.Join(env, "\n")
}

func lostKeys(before, after string) []string {
	have := map[string]bool{}
	for _, kv := range strings.Split(after, "\n") {
		have[kv] = true
	}
	var lost []string
	for _, kv := range strings.Split(before, "\n") {
		if !have[kv] {
			lost = append(lost, strings.SplitN(kv, "=", 2)[0])
		}
	}
	return lost
}
