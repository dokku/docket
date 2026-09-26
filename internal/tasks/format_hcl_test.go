package tasks

import (
	"strings"
	"testing"
)

// formatHCL is FormatHCL with the error folded into a fatal, for the many
// cases that only care about the output.
func formatHCL(t *testing.T, in string) string {
	t.Helper()
	out, err := FormatHCL([]byte(in))
	if err != nil {
		t.Fatalf("FormatHCL(%q): %v", in, err)
	}
	return string(out)
}

// TestFormatHCLCanonicalForm covers the layout `docket fmt` imposes: two-space
// indentation, aligned `=` signs, a blank line before each nested block, and
// the canonical key order the other two formats already use.
func TestFormatHCLCanonicalForm(t *testing.T) {
	t.Parallel()
	const in = `play    "web"   {
tasks_unknown = 1
when = "x"
input "app" {
default = "web"
description = "the app"
}
dokku_app "create" {
state = "present"
app = "web"
}
}
`
	// The key order is canonicalPlayKeys, shared with the other two formats,
	// so a key it does not name sorts after the ones it does - `tasks` here
	// included. An HCL recipe and its YAML twin lay out identically.
	const want = `play "web" {
  when = "x"

  input "app" {
    default     = "web"
    description = "the app"
  }

  dokku_app "create" {
    state = "present"
    app   = "web"
  }
  tasks_unknown = 1
}
`
	if got := formatHCL(t, in); got != want {
		t.Errorf("FormatHCL =\n%s\nwant\n%s", got, want)
	}
}

// TestFormatHCLIsIdempotent is the property every formatter owes: running it
// twice changes nothing the first run did not.
func TestFormatHCLIsIdempotent(t *testing.T) {
	t.Parallel()
	for name, in := range map[string]string{
		"fixture":  convertFixtureHCL,
		"heredoc":  "play {\n  dokku_app {\n    note = <<EOT\na\nb\nEOT\n  }\n}\n",
		"general":  "play {\n  task \"deploy\" {\n    block {\n      dokku_app { app = \"a\" }\n    }\n  }\n}\n",
		"comments": "# top\nplay \"web\" { # beside\n  # about\n  dokku_app { app = \"a\" }\n  # trailing\n}\n",
	} {
		name, in := name, in
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			once := formatHCL(t, in)
			twice := formatHCL(t, once)
			if once != twice {
				t.Errorf("FormatHCL is not idempotent:\nfirst:\n%s\nsecond:\n%s", once, twice)
			}
		})
	}
}

// TestFormatHCLEmptyAndCommentOnly pins that a file with no recipe in it is
// left exactly as it was, following YAML rather than JSON5's refusal.
func TestFormatHCLEmptyAndCommentOnly(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "\n\n", "# just a note\n", "// a note\n/* and another */\n"} {
		if got := formatHCL(t, in); got != in {
			t.Errorf("FormatHCL(%q) = %q, want it untouched", in, got)
		}
	}
}

// TestFormatHCLHeredocRule covers the one choice the emitter makes about a
// string: a multi-line value is written as a heredoc, and everything else as a
// quoted scalar.
//
// The rule is read off the value rather than off how the source spelled it,
// which is what makes it idempotent - so these cases are all about which
// values qualify.
func TestFormatHCLHeredocRule(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		yaml string
		want string
	}{
		{
			name: "multi-line block scalar becomes a heredoc",
			yaml: "- tasks:\n    - dokku_app:\n        note: |\n          a\n          b\n",
			want: "note = <<EOT\na\nb\nEOT",
		},
		{
			name: "single line stays quoted",
			yaml: "- tasks:\n    - dokku_app:\n        note: \"a\"\n",
			want: `note = "a"`,
		},
		{
			name: "no trailing newline stays quoted",
			yaml: "- tasks:\n    - dokku_app:\n        note: \"a\\nb\"\n",
			want: `note = "a\nb"`,
		},
		{
			name: "a line equal to the delimiter stays quoted",
			yaml: "- tasks:\n    - dokku_app:\n        note: |\n          a\n          EOT\n          b\n",
			want: `note = "a\nEOT\nb\n"`,
		},
		{
			name: "trailing whitespace stays quoted",
			yaml: "- tasks:\n    - dokku_app:\n        note: \"a \\nb\\n\"\n",
			want: `note = "a \nb\n"`,
		},
		{
			name: "an interpolation stays quoted so dq still works",
			yaml: "- tasks:\n    - dokku_app:\n        note: \"{{ .app | dq }}\\nb\\n\"\n",
			want: `note = "{{ .app | dq }}\nb\n"`,
		},
		{
			name: "a heredoc escapes hcl's own template openers",
			yaml: "- tasks:\n    - dokku_app:\n        note: |\n          ${a}\n          %{b}\n",
			want: "note = <<EOT\n$${a}\n%%{b}\nEOT",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			out, err := Convert([]byte(tt.yaml), yamlCodec{}, hclCodec{})
			if err != nil {
				t.Fatalf("Convert: %v", err)
			}
			if !strings.Contains(string(out), tt.want) {
				t.Errorf("hcl =\n%s\nwant it to contain\n%s", out, tt.want)
			}
		})
	}
}

// TestFormatHCLRefusesAHeredocInterpolation covers the quoting refusal.
//
// Canonical HCL writes a string holding an interpolation as a quoted scalar,
// and a heredoc processes no backslash escapes at all - so `| dq` inside one
// would land as literal backslashes. It is the same refusal FormatJSON5 makes
// over a single-quoted string, and it names the same fix.
func TestFormatHCLRefusesAHeredocInterpolation(t *testing.T) {
	t.Parallel()
	_, err := FormatHCL([]byte("play {\n  dokku_app {\n    note = <<EOT\nhello {{ .app }}\nEOT\n  }\n}\n"))
	if err == nil {
		t.Fatal("FormatHCL of a heredoc holding an interpolation = nil error, want a refusal")
	}
	for _, want := range []string{"line 3", "{{ .app }}", "in a heredoc", "| dq"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q should mention %q", err, want)
		}
	}
}

// TestFormatHCLAcceptsAnEscapedHeredocInterpolation is the other side of it: a
// heredoc that substitutes nothing has no quoting to lose.
func TestFormatHCLAcceptsAnEscapedHeredocInterpolation(t *testing.T) {
	t.Parallel()
	const in = "play {\n  dokku_app {\n    note = <<EOT\nweb{{ if .debug }}-verbose{{ end }}\nEOT\n  }\n}\n"
	if _, err := FormatHCL([]byte(in)); err != nil {
		t.Errorf("FormatHCL of a control-only interpolation: %v", err)
	}
}

// TestFormatHCLRejectsExpressions pins that a recipe is data. HCL's variables,
// functions and templates all have a reading docket has no use for, and
// evaluating them against an empty scope would fold `%{if true}` away to
// nothing rather than say so.
func TestFormatHCLRejectsExpressions(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"variable":         "play {\n  dokku_app {\n    app = somevar\n  }\n}\n",
		"function call":    "play {\n  dokku_app {\n    app = upper(\"web\")\n  }\n}\n",
		"interpolation":    "play {\n  dokku_app {\n    app = \"${somevar}\"\n  }\n}\n",
		"control template": "play {\n  dokku_app {\n    app = \"%{if true}web%{endif}\"\n  }\n}\n",
		"operator":         "play {\n  dokku_app {\n    app = !true\n  }\n}\n",
	}
	for name, in := range tests {
		name, in := name, in
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := FormatHCL([]byte(in))
			if err == nil {
				t.Fatalf("FormatHCL(%q) = nil error, want a refusal", in)
			}
			if !strings.Contains(err.Error(), "{{ .name }}") {
				t.Errorf("error %q should point at docket's own substitution syntax", err)
			}
		})
	}
}

// TestFormatHCLAcceptsNegativeNumbers is the exception to the rule above: HCL
// parses `-5` as a unary operator rather than as a literal, so a recipe with a
// negative number would be refused by a blanket ban on operators.
func TestFormatHCLAcceptsNegativeNumbers(t *testing.T) {
	t.Parallel()
	got := formatHCL(t, "play {\n  dokku_app {\n    offset = -5\n    ratio  = -1.5\n  }\n}\n")
	for _, want := range []string{"offset = -5", "ratio  = -1.5"} {
		if !strings.Contains(got, want) {
			t.Errorf("FormatHCL =\n%s\nwant it to contain %q", got, want)
		}
	}
}

// TestFormatHCLRefusesAKeyItCannotSpell covers the one shape an HCL recipe has
// no attribute name for: a PLAY key that is not an identifier. HCL identifiers
// already allow `-` and non-ASCII, so this reaches only a key with a space, a
// dot, or a leading digit.
func TestFormatHCLRefusesAKeyItCannotSpell(t *testing.T) {
	t.Parallel()
	_, err := Convert([]byte("- \"two words\": 1\n  tasks: []\n"), yamlCodec{}, hclCodec{})
	if err == nil {
		t.Fatal("Convert = nil error, want a refusal naming the key")
	}
	if !strings.Contains(err.Error(), "two words") {
		t.Errorf("error %q should name the key it cannot spell", err)
	}
}

// TestFormatHCLKeepsAnAwkwardKeyInsideAnObject is the other half, and the
// reason the refusal above is narrow: an object expression may quote its keys,
// so a task body carrying one falls back to the general form rather than
// failing. Nothing below the recipe's own structure can be unspellable.
func TestFormatHCLKeepsAnAwkwardKeyInsideAnObject(t *testing.T) {
	t.Parallel()
	for _, in := range []string{
		"- tasks:\n    - dokku_app:\n        \"two words\": 1\n",
		"- tasks:\n    - dokku_config:\n        config:\n          \"two words\": \"1\"\n",
	} {
		out, err := Convert([]byte(in), yamlCodec{}, hclCodec{})
		if err != nil {
			t.Fatalf("Convert(%q): %v", in, err)
		}
		if !strings.Contains(string(out), `"two words" = `) {
			t.Errorf("hcl =\n%s\nwant a quoted object key", out)
		}
		if _, err := Convert(out, hclCodec{}, yamlCodec{}); err != nil {
			t.Errorf("the result does not convert back: %v", err)
		}
	}
}

// TestFormatHCLFallsBackRatherThanRefusing covers the shapes the shorthand
// cannot spell but the general form can. `fmt` is not a validator and has to
// round-trip any recipe that parses, so each of these has to come out as HCL
// that reads back the same.
func TestFormatHCLFallsBackRatherThanRefusing(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"tasks is not a list of mappings":  "- tasks:\n    - web\n",
		"inputs is not a list of mappings": "- inputs: [web]\n  tasks: []\n",
		"two task-type keys":               "- tasks:\n    - dokku_app: {app: a}\n      dokku_config: {app: a}\n",
		"no task-type key":                 "- tasks:\n    - when: \"x\"\n",
		"null task body":                   "- tasks:\n    - dokku_app:\n",
		"non-string name":                  "- tasks:\n    - name: 5\n      dokku_app: {app: a}\n",
		"task type is a reserved block":    "- tasks:\n    - input: {app: a}\n",
	}
	for name, in := range tests {
		name, in := name, in
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, err := Convert([]byte(in), yamlCodec{}, hclCodec{})
			if err != nil {
				t.Fatalf("Convert(%q): %v", in, err)
			}
			back, err := Convert(out, hclCodec{}, yamlCodec{})
			if err != nil {
				t.Fatalf("Convert back: %v\nvia:\n%s", err, out)
			}
			// Structurally rather than byte for byte: a conversion is
			// allowed to normalise a spelling (a flow sequence becomes a
			// block one, an implicit null becomes an explicit one), and
			// what has to survive is the recipe. Some of these inputs are
			// recipes the loader would reject, which is the point - `fmt`
			// is not a validator.
			want, err := yamlCodec{}.DecodeDocument([]byte(in))
			if err != nil {
				t.Fatalf("DecodeDocument(in): %v", err)
			}
			got, err := yamlCodec{}.DecodeDocument(back)
			if err != nil {
				t.Fatalf("DecodeDocument(back): %v", err)
			}
			if !equivalentConvertedNodes(documentBody(want), documentBody(got)) {
				t.Errorf("round trip changed the recipe:\nwant:\n%s\ngot:\n%s\nvia:\n%s", in, back, out)
			}
		})
	}
}

// TestYAMLDocumentToHCLRefusesAnEmptyRecipe pins the one recipe HCL has no
// spelling for: an empty list of plays, which would be written as an empty
// file and read back as no recipe at all.
func TestYAMLDocumentToHCLRefusesAnEmptyRecipe(t *testing.T) {
	t.Parallel()
	_, err := Convert([]byte("[]\n"), yamlCodec{}, hclCodec{})
	if err == nil {
		t.Fatal("Convert of an empty recipe = nil error, want a refusal")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error %q should say what it cannot spell", err)
	}
}

// TestFormatHCLRefusesANonRecipeShape covers the shapes the encoder cannot
// make blocks out of. `fmt` has to round-trip any recipe that parses, so these
// have to fail cleanly rather than write something that reads back differently.
func TestFormatHCLRefusesANonRecipeShape(t *testing.T) {
	t.Parallel()
	for name, in := range map[string]string{
		"scalar root":    "just a string\n",
		"mapping root":   "name: web\n",
		"scalar play":    "- web\n",
		"infinite float": "- tasks:\n    - dokku_app:\n        ratio: .inf\n",
	} {
		name, in := name, in
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, err := Convert([]byte(in), yamlCodec{}, hclCodec{})
			if err == nil {
				t.Errorf("Convert(%q) = nil error and %q, want a refusal", in, out)
			}
		})
	}
}
