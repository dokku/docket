package tasks

import (
	"strconv"
	"strings"
	"testing"
)

// input_declaration_test.go covers checkInputDeclarations, the tasks-side half
// of #493. registerInputFlags used to be the only code that read an input's
// `type:`, and it could only report a problem through an error its every caller
// discarded - so a malformed declaration cost the recipe its whole input flag
// surface and said nothing. The checks live here now, beside the other
// pre-render input checks, so validate reports them with a source position and
// the loader rejects them offline.

func TestCheckInputDeclarationsFlagsUnknownType(t *testing.T) {
	data := []byte(`---
- inputs:
    - name: port
      type: intt
      default: "8080"
  tasks: []
`)
	problems := checkInputDeclarations(declaredInputs(data, FormatYAML))
	p := findProblem(problems, "invalid_input_type")
	if p == nil {
		t.Fatalf("expected invalid_input_type problem, got: %+v", problems)
	}
	if !strings.Contains(p.Message, `input "port" declares unknown type "intt"`) {
		t.Errorf("message = %q", p.Message)
	}
	if p.Hint != `did you mean "int"?` {
		t.Errorf("hint = %q, want a did-you-mean for int", p.Hint)
	}
	if p.Line == 0 || p.Column == 0 {
		t.Errorf("problem has no source position: line=%d column=%d", p.Line, p.Column)
	}
}

func TestCheckInputDeclarationsListsTypesWhenNoNearMiss(t *testing.T) {
	data := []byte(`---
- inputs:
    - { name: window, type: duration }
  tasks: []
`)
	p := findProblem(checkInputDeclarations(declaredInputs(data, FormatYAML)), "invalid_input_type")
	if p == nil {
		t.Fatal("expected invalid_input_type problem")
	}
	if p.Hint != "use one of bool, float, int, string" {
		t.Errorf("hint = %q, want the full type list", p.Hint)
	}
}

func TestCheckInputDeclarationsFlagsUnparseableDefault(t *testing.T) {
	tests := map[string]struct {
		recipe string
		want   string
	}{
		"int":   {`- { name: port, type: int, default: abc }`, `input "port" declares type int but its default "abc" is not a valid int`},
		"float": {`- { name: ratio, type: float, default: half }`, `input "ratio" declares type float but its default "half" is not a valid float`},
		"bool":  {`- { name: debug, type: bool, default: maybe }`, `input "debug" declares type bool but its default "maybe" is not a valid bool`},
		// A near miss of a real spelling is still a miss, widened table or not.
		"bool number": {`- { name: debug, type: bool, default: 2 }`, `input "debug" declares type bool but its default "2" is not a valid bool`},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			data := []byte("---\n- inputs:\n    " + tt.recipe + "\n  tasks: []\n")
			p := findProblem(checkInputDeclarations(declaredInputs(data, FormatYAML)), "invalid_input_default")
			if p == nil {
				t.Fatalf("expected invalid_input_default problem for %s", name)
			}
			if p.Message != tt.want {
				t.Errorf("message = %q, want %q", p.Message, tt.want)
			}
			if p.Hint == "" {
				t.Error("problem carries no remediation hint")
			}
		})
	}
}

// TestCheckInputDeclarationsAcceptsOmittedDefault: an omitted default is the
// zero value for the type, which is the whole point of #493.
func TestCheckInputDeclarationsAcceptsOmittedDefault(t *testing.T) {
	data := []byte(`---
- inputs:
    - { name: debug, type: bool }
    - { name: replicas, type: int }
    - { name: ratio, type: float }
    - { name: app_name }
  tasks: []
`)
	if problems := checkInputDeclarations(declaredInputs(data, FormatYAML)); len(problems) > 0 {
		t.Errorf("omitted defaults were flagged: %+v", problems)
	}
}

// TestCheckInputDeclarationsAcceptsWidenedBoolDefaults: #495. YAML hands the
// field the text that was written, so an unquoted `True` arrives as "True" and
// a `1` as "1"; both are the same value the command line spells that way, and
// the validator now reads them as such.
func TestCheckInputDeclarationsAcceptsWidenedBoolDefaults(t *testing.T) {
	for _, def := range []string{"True", "TRUE", "1", "0", "On", "off", "t", "F", "Y", "n"} {
		t.Run(def, func(t *testing.T) {
			data := []byte("---\n- inputs:\n    - { name: debug, type: bool, default: " + def + " }\n  tasks: []\n")
			if problems := checkInputDeclarations(declaredInputs(data, FormatYAML)); len(problems) > 0 {
				t.Errorf("default %q was flagged: %+v", def, problems)
			}
		})
	}
}

// TestCheckInputDeclarationsSkipsReservedNames keeps a reserved name surfacing
// as the more specific reserved_input_name, the way checkInputNames does.
func TestCheckInputDeclarationsSkipsReservedNames(t *testing.T) {
	data := []byte(`---
- inputs:
    - { name: json, type: bogus }
  tasks: []
`)
	if problems := checkInputDeclarations(declaredInputs(data, FormatYAML)); len(problems) > 0 {
		t.Errorf("reserved name produced an input-declaration problem: %+v", problems)
	}
}

// TestCheckInputDeclarationsTolerantOnUnparseableRecipe: the check runs before
// the recipe is parsed, so it must return nothing rather than mask the caller's
// own parse error.
func TestCheckInputDeclarationsTolerantOnUnparseableRecipe(t *testing.T) {
	if problems := checkInputDeclarations(declaredInputs([]byte("not valid yaml: [[["), FormatYAML)); problems != nil {
		t.Errorf("expected no problems from an unparseable recipe, got: %+v", problems)
	}
}

func TestGetPlaysRejectsInvalidInputDeclaration(t *testing.T) {
	tests := map[string]string{
		"unknown type": `---
- inputs:
    - { name: port, type: bogus }
  tasks:
    - dokku_app: { app: web }
`,
		"unparseable default": `---
- inputs:
    - { name: port, type: int, default: abc }
  tasks:
    - dokku_app: { app: web }
`,
	}
	for name, recipe := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := GetPlays([]byte(recipe), map[string]interface{}{}, nil)
			if err == nil {
				t.Fatal("expected the loader to reject the recipe")
			}
			if !strings.Contains(err.Error(), `input "port"`) {
				t.Errorf("loader error does not name the input: %v", err)
			}
		})
	}
}

func TestValidateReportsInvalidInputType(t *testing.T) {
	problems := Validate([]byte(`---
- inputs:
    - { name: port, type: intt }
  tasks:
    - dokku_app: { app: web }
`), ValidateOptions{})
	if findProblem(problems, "invalid_input_type") == nil {
		t.Fatalf("expected invalid_input_type problem, got: %+v", problems)
	}
}

// TestParseInputBoolSpellings pins the widened table from #495: docket's
// vocabulary is the union of its own and pflag's, matched case-insensitively,
// so every spelling `--debug=...` takes on the command line is also a spelling
// a `default:` or a --vars-file value may use.
func TestParseInputBoolSpellings(t *testing.T) {
	for _, s := range []string{"true", "yes", "on", "y", "t", "1", "True", "TRUE", "Yes", "ON", "Y", "T"} {
		if v, ok := ParseInputBool(s); !ok || !v {
			t.Errorf("ParseInputBool(%q) = (%v, %v), want (true, true)", s, v, ok)
		}
	}
	for _, s := range []string{"false", "no", "off", "n", "f", "0", "False", "FALSE", "No", "Off", "N", "F"} {
		if v, ok := ParseInputBool(s); !ok || v {
			t.Errorf("ParseInputBool(%q) = (%v, %v), want (false, true)", s, v, ok)
		}
	}
	// The empty string is not a spelling: a caller that treats an omitted
	// default as the zero value checks for it before asking. Widening the table
	// did not make it a free-for-all - a near miss is still a near miss.
	for _, s := range []string{"", "maybe", "2", "-1", "1.0", "yeah", "onn", " true", "true "} {
		if _, ok := ParseInputBool(s); ok {
			t.Errorf("ParseInputBool(%q) reported ok", s)
		}
	}
}

// TestParseInputBoolCoversPflag is the point of #495 stated directly: pflag
// parses the command line with strconv.ParseBool, so anything it accepts there
// has to be readable as a `default:` too, or the same spelling means two things
// in one recipe.
func TestParseInputBoolCoversPflag(t *testing.T) {
	for _, s := range []string{"1", "t", "T", "TRUE", "true", "True", "0", "f", "F", "FALSE", "false", "False"} {
		want, err := strconv.ParseBool(s)
		if err != nil {
			t.Fatalf("strconv.ParseBool(%q) failed, fix the test table: %v", s, err)
		}
		got, ok := ParseInputBool(s)
		if !ok {
			t.Errorf("ParseInputBool(%q) reported not-ok, but pflag accepts it", s)
			continue
		}
		if got != want {
			t.Errorf("ParseInputBool(%q) = %v, pflag reads it as %v", s, got, want)
		}
	}
}

func TestCanonicalInputType(t *testing.T) {
	for in, want := range map[string]string{
		"":       "string",
		"string": "string",
		"bool":   "bool",
		"int":    "int",
		"float":  "float",
	} {
		got, ok := CanonicalInputType(in)
		if !ok || got != want {
			t.Errorf("CanonicalInputType(%q) = (%q, %v), want (%q, true)", in, got, ok, want)
		}
	}
	for _, in := range []string{"bogus", "Bool", "integer"} {
		if _, ok := CanonicalInputType(in); ok {
			t.Errorf("CanonicalInputType(%q) reported ok", in)
		}
	}
}
