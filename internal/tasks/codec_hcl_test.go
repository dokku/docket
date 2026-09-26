package tasks

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// positionInText matches a line number spelled out in a message. A Problem
// carries its position in Line and Column, and the validator prints those, so
// a message repeating them reads as `line 3:12: … line 3, column 12: …`.
var positionInText = regexp.MustCompile(`line \d`)

// TestHCLSniff covers the content sniff that decides the format of a recipe
// with no filename - stdin.
//
// HCL is asked after JSON5, so the cases that matter are the ones JSON5 does
// not claim: a body opening with an identifier, and a `#` comment ahead of
// one. A recipe `docket fmt` wrote always comments with `#` for exactly this
// reason, so canonical HCL is always recognised.
func TestHCLSniff(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
		want string
	}{
		{name: "play block", data: "play \"web\" {\n}\n", want: FormatNameHCL},
		{name: "unlabelled block", data: "play {\n}\n", want: FormatNameHCL},
		{name: "attribute", data: "app = \"web\"\n", want: FormatNameHCL},
		{name: "hash comment then block", data: "# a recipe\nplay {\n}\n", want: FormatNameHCL},
		{name: "blank lines then block", data: "\n\n  play {\n}\n", want: FormatNameHCL},

		{name: "yaml sequence", data: "- tasks: []\n", want: FormatYAML},
		{name: "yaml mapping key", data: "tasks: []\n", want: FormatYAML},
		{name: "yaml document marker", data: "---\n- tasks: []\n", want: FormatYAML},
		{name: "yaml comment", data: "# a recipe\n- tasks: []\n", want: FormatYAML},
		{name: "json5 array", data: "[{tasks: []}]\n", want: FormatNameJSON5},
		{name: "json5 object", data: "{\"a\": 1}\n", want: FormatNameJSON5},
		{name: "empty", data: "", want: FormatYAML},
		{name: "two identifiers", data: "play web\n", want: FormatYAML},

		// JSON5 is asked first and claims any stream opening with its own
		// comment syntax, so a hand-written HCL recipe led by `//` needs
		// --tasks-format. docs/hcl.md says so.
		{name: "slash comment then block", data: "// a recipe\nplay {\n}\n", want: FormatNameJSON5},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := SniffCodec([]byte(tt.data)).Name(); got != tt.want {
				t.Errorf("SniffCodec(%q) = %q, want %q", tt.data, got, tt.want)
			}
		})
	}
}

// TestHCLDuplicateAttributeIsADuplicateKey pins the one place docket keys off
// the wording of an hcl/v2 diagnostic.
//
// A repeated attribute is HCL's duplicate key, and it has to report under the
// same code YAML and JSON5 use, because docs/json-output.md documents that
// code as a stable machine key. hclsyntax raises it as a parse error with the
// summary "Attribute redefined"; should a future version reword that, this
// fails rather than the code silently degrading to the generic hcl_parse.
func TestHCLDuplicateAttributeIsADuplicateKey(t *testing.T) {
	t.Parallel()
	const recipe = `play "web" {
  dokku_app "create" {
    app = "a"
    app = "b"
  }
}
`
	_, problem := hclCodec{}.ToYAML([]byte(recipe))
	if problem == nil {
		t.Fatal("ToYAML of a repeated attribute = nil problem, want a duplicate_key finding")
	}
	if problem.Code != "duplicate_key" {
		t.Errorf("problem code = %q, want duplicate_key", problem.Code)
	}
	if problem.Line != 4 {
		t.Errorf("problem line = %d, want 4", problem.Line)
	}
	if !strings.Contains(problem.Message, `"app"`) {
		t.Errorf("problem message = %q, want it to name the repeated key", problem.Message)
	}
}

// TestHCLParseProblemCarriesPosition pins the other half: a syntax error
// reports under hcl_parse with a line and column, and its message does not
// repeat them - the validator prints those itself.
func TestHCLParseProblemCarriesPosition(t *testing.T) {
	t.Parallel()
	_, problem := hclCodec{}.ToYAML([]byte("play \"web\" {\n  dokku_app \"create\"\n}\n"))
	if problem == nil {
		t.Fatal("ToYAML of malformed hcl = nil problem, want an hcl_parse finding")
	}
	if problem.Code != "hcl_parse" {
		t.Errorf("problem code = %q, want hcl_parse", problem.Code)
	}
	if problem.Line == 0 {
		t.Error("problem carries no line; the diagnostic's position was dropped")
	}
	if positionInText.MatchString(problem.Message) {
		t.Errorf("problem message = %q; the position belongs in Line/Column, not in the text", problem.Message)
	}
}

// TestHCLToYAMLRejectsANonRecipeBody covers the shapes HCL can hold but a
// recipe cannot.
func TestHCLToYAMLRejectsANonRecipeBody(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"top-level attribute": "app = \"web\"\n",
		"unknown block":       "task \"x\" {\n}\n",
		"labelled group":      "play {\n  task {\n    block \"nope\" {\n    }\n  }\n}\n",
		"attribute in group":  "play {\n  task {\n    block {\n      app = \"x\"\n    }\n  }\n}\n",
		"nested in shorthand": "play {\n  dokku_app {\n    config {\n      A = \"1\"\n    }\n  }\n}\n",
	}
	for name, recipe := range tests {
		name, recipe := name, recipe
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, problem := hclCodec{}.ToYAML([]byte(recipe))
			if problem == nil {
				t.Errorf("ToYAML(%q) = nil problem, want a refusal", recipe)
			}
		})
	}
}

// TestHCLVarsFile covers the vars-file pair beyond the registry round trip:
// the spelling it writes, and what it refuses to read.
func TestHCLVarsFile(t *testing.T) {
	t.Parallel()

	out, err := hclCodec{}.MarshalVars(map[string]interface{}{
		"app":   "inflector",
		"port":  8080,
		"debug": true,
	})
	if err != nil {
		t.Fatalf("MarshalVars: %v", err)
	}
	// yaml.Marshal sorts a map's keys, so the output is deterministic, and
	// hclwrite.Format aligns the `=` signs the way it does everywhere else.
	const want = "app   = \"inflector\"\ndebug = true\nport  = 8080\n"
	if string(out) != want {
		t.Errorf("MarshalVars =\n%s\nwant\n%s", out, want)
	}

	back, err := hclCodec{}.UnmarshalVars(out)
	if err != nil {
		t.Fatalf("UnmarshalVars: %v", err)
	}
	if want := map[string]interface{}{"app": "inflector", "port": 8080, "debug": true}; !reflect.DeepEqual(back, want) {
		t.Errorf("UnmarshalVars = %#v, want %#v", back, want)
	}

	if _, err := (hclCodec{}).UnmarshalVars([]byte("play \"web\" {\n}\n")); err == nil {
		t.Error("UnmarshalVars of a recipe = nil error, want a refusal; a vars-file is a flat mapping")
	}
}

// TestHCLVarsFileComments covers a vars-file written by hand: comments are
// skipped rather than tripping the reader, which is the whole reason someone
// keeps values in HCL rather than in a flag.
func TestHCLVarsFileComments(t *testing.T) {
	t.Parallel()
	back, err := hclCodec{}.UnmarshalVars([]byte("# production\napp = \"inflector\" # the app\n"))
	if err != nil {
		t.Fatalf("UnmarshalVars: %v", err)
	}
	if want := map[string]interface{}{"app": "inflector"}; !reflect.DeepEqual(back, want) {
		t.Errorf("UnmarshalVars = %#v, want %#v", back, want)
	}
}
