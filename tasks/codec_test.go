package tasks

import (
	"reflect"
	"strings"
	"testing"
)

// codecTestRecipe is a minimal well-formed recipe used to exercise a
// codec's Marshal / Format / ToYAML round trip.
var codecTestRecipe = []map[string]interface{}{
	{
		"name": "web",
		"tasks": []map[string]interface{}{
			{"name": "create app", "dokku_app": map[string]interface{}{"app": "web", "state": "present"}},
		},
	},
}

// TestCodecConformance is the contract a new codec has to satisfy. It runs
// over the registry rather than over a hardcoded pair, so adding a format
// is checked the moment it joins the codecs slice - which is the whole
// point of the seam.
func TestCodecConformance(t *testing.T) {
	t.Parallel()

	seenName := map[string]string{}
	seenExt := map[string]string{}
	for _, codec := range Codecs() {
		name := codec.Name()
		if strings.TrimSpace(name) == "" {
			t.Fatalf("codec %T has an empty Name()", codec)
		}
		if prior, dup := seenName[name]; dup {
			t.Errorf("codec %T claims the name %q already taken by %s", codec, name, prior)
		}
		seenName[name] = name

		for _, alias := range codec.Aliases() {
			if prior, dup := seenName[alias]; dup {
				t.Errorf("codec %q claims the alias %q already taken by %s", name, alias, prior)
			}
			seenName[alias] = name
		}

		if len(codec.Extensions()) == 0 {
			t.Errorf("codec %q claims no file extensions, so no path can ever select it", name)
		}
		for _, ext := range codec.Extensions() {
			if prior, dup := seenExt[ext]; dup {
				t.Errorf("codec %q claims the extension %q already taken by %q", name, ext, prior)
			}
			seenExt[ext] = name
		}

		t.Run(name+" round trip", func(t *testing.T) {
			marshalled, err := codec.Marshal(codecTestRecipe)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}

			// What Marshal emits must already be canonical, or `docket
			// export` would write a file `docket fmt --check` rejects.
			formatted, err := codec.Format(marshalled)
			if err != nil {
				t.Fatalf("Format of Marshal output: %v", err)
			}
			if string(formatted) != string(marshalled) {
				t.Errorf("Marshal output is not canonical:\nmarshalled:\n%s\nformatted:\n%s", marshalled, formatted)
			}

			// And it must read back, or the format could be written but
			// never applied.
			if _, problem := codec.ToYAML(marshalled); problem != nil {
				t.Fatalf("ToYAML of Marshal output: %s", problem.Message)
			}
			if problems := codec.Lint(marshalled); len(problems) > 0 {
				t.Errorf("Lint of Marshal output = %v, want none", problems)
			}

			recipe, err := UnmarshalRecipe(marshalled, name)
			if err != nil {
				t.Fatalf("UnmarshalRecipe: %v", err)
			}
			if len(recipe) != 1 {
				t.Errorf("round-tripped recipe = %d plays, want 1", len(recipe))
			}
		})
	}
}

// TestLookupCodec pins the spellings the --tasks-format / --format flags
// accept. The empty string is a miss on purpose: it means "flag not set",
// and parseRecipeFormatFlag has to answer that before it asks here.
func TestLookupCodec(t *testing.T) {
	t.Parallel()
	hits := map[string]string{
		"yaml":  FormatYAML,
		"yml":   FormatYAML,
		"YAML":  FormatYAML,
		"YML":   FormatYAML,
		" yaml": FormatYAML,
		"yaml ": FormatYAML,
		"json":  FormatNameJSON5,
		"json5": FormatNameJSON5,
		"JSON5": FormatNameJSON5,
	}
	for spelling, want := range hits {
		codec, ok := LookupCodec(spelling)
		if !ok {
			t.Errorf("LookupCodec(%q) = miss, want %q", spelling, want)
			continue
		}
		if codec.Name() != want {
			t.Errorf("LookupCodec(%q) = %q, want %q", spelling, codec.Name(), want)
		}
	}

	for _, spelling := range []string{"", "   ", "toml", "hcl", "ini", "yamlish", "js"} {
		if codec, ok := LookupCodec(spelling); ok {
			t.Errorf("LookupCodec(%q) = %q, want a miss", spelling, codec.Name())
		}
	}
}

// TestCodecForFallsBackToDefault pins the explicit default that replaced
// the IsJSON5Format("") fallthrough: an absent or unrecognised format is
// read as the default codec rather than deciding anything by accident.
func TestCodecForFallsBackToDefault(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"", "toml", "hcl", "nonsense"} {
		if got := CodecFor(format).Name(); got != DefaultCodec().Name() {
			t.Errorf("CodecFor(%q) = %q, want the default codec %q", format, got, DefaultCodec().Name())
		}
	}
	if got := CodecFor("json").Name(); got != FormatNameJSON5 {
		t.Errorf("CodecFor(\"json\") = %q, want %q; the lenient lookup must still resolve aliases", got, FormatNameJSON5)
	}
}

func TestCodecForExtension(t *testing.T) {
	t.Parallel()
	hits := map[string]string{
		"yml":   FormatYAML,
		"yaml":  FormatYAML,
		".yml":  FormatYAML,
		".YML":  FormatYAML,
		"json":  FormatNameJSON5,
		"json5": FormatNameJSON5,
		".JSON": FormatNameJSON5,
	}
	for ext, want := range hits {
		codec, ok := CodecForExtension(ext)
		if !ok {
			t.Errorf("CodecForExtension(%q) = miss, want %q", ext, want)
			continue
		}
		if codec.Name() != want {
			t.Errorf("CodecForExtension(%q) = %q, want %q", ext, codec.Name(), want)
		}
	}

	for _, ext := range []string{"", ".", "txt", ".txt", "toml"} {
		if codec, ok := CodecForExtension(ext); ok {
			t.Errorf("CodecForExtension(%q) = %q, want a miss so the caller can fall back", ext, codec.Name())
		}
	}
}

// TestSniffCodec covers the content sniff that decides the format of a
// recipe with no filename - stdin. The comment and end-of-input cases are
// the ones a careless implementation gets wrong: a lone "/" must not be
// read as the start of a comment.
func TestSniffCodec(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		data string
		want string
	}{
		{name: "array opens json5", data: "[{tasks: []}]", want: FormatNameJSON5},
		{name: "object opens json5", data: `{"a": 1}`, want: FormatNameJSON5},
		{name: "line comment opens json5", data: "// a recipe\n[]", want: FormatNameJSON5},
		{name: "block comment opens json5", data: "/* a recipe */\n[]", want: FormatNameJSON5},
		{name: "leading whitespace before array", data: "  \n\t[]", want: FormatNameJSON5},
		{name: "leading whitespace before comment", data: "\r\n  // hi\n[]", want: FormatNameJSON5},
		{name: "document marker is yaml", data: "---\n- tasks: []\n", want: FormatYAML},
		{name: "bare key is yaml", data: "tasks: []\n", want: FormatYAML},
		{name: "sequence dash is yaml", data: "- tasks: []\n", want: FormatYAML},
		{name: "yaml comment is yaml", data: "# a recipe\n- tasks: []\n", want: FormatYAML},
		{name: "empty is yaml", data: "", want: FormatYAML},
		{name: "whitespace only is yaml", data: "  \n\t\r\n", want: FormatYAML},
		{name: "lone slash at end of input is yaml", data: "/", want: FormatYAML},
		{name: "slash then non-comment is yaml", data: "/x", want: FormatYAML},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SniffCodec([]byte(tt.data)).Name(); got != tt.want {
				t.Errorf("SniffCodec(%q) = %q, want %q", tt.data, got, tt.want)
			}
		})
	}
}

// TestCodecNamesAndExtensions pins the ordered lists the CLI surfaces
// verbatim: CodecNames drives shell completion and the "must be one of
// ..." rejection, CodecExtensions drives file completion and the
// positional-recipe-path check. Reordering either is user-visible.
func TestCodecNamesAndExtensions(t *testing.T) {
	t.Parallel()
	if got, want := CodecNames(), []string{"yaml", "json5"}; !reflect.DeepEqual(got, want) {
		t.Errorf("CodecNames() = %v, want %v", got, want)
	}
	if got, want := CodecExtensions(), []string{"yml", "yaml", "json", "json5"}; !reflect.DeepEqual(got, want) {
		t.Errorf("CodecExtensions() = %v, want %v", got, want)
	}
	if got := DefaultCodec().Name(); got != FormatYAML {
		t.Errorf("DefaultCodec() = %q, want %q; YAML is what an unknown format resolves to", got, FormatYAML)
	}
}

// TestCodecsIsACopy stops a caller from reordering the registry - and so
// changing the default codec - through the slice Codecs hands back.
func TestCodecsIsACopy(t *testing.T) {
	t.Parallel()
	got := Codecs()
	if len(got) < 2 {
		t.Fatalf("Codecs() = %d codecs, want at least 2", len(got))
	}
	got[0] = got[1]
	if DefaultCodec().Name() != FormatYAML {
		t.Errorf("mutating the Codecs() result changed DefaultCodec() to %q", DefaultCodec().Name())
	}
}

// TestCodecMarshalVarsRoundTrip covers the vars-file pair, which had no
// direct test before: every codec must read back what it writes.
func TestCodecMarshalVarsRoundTrip(t *testing.T) {
	t.Parallel()
	vars := map[string]interface{}{"app": "web", "domain": "example.com"}
	for _, codec := range Codecs() {
		t.Run(codec.Name(), func(t *testing.T) {
			out, err := codec.MarshalVars(vars)
			if err != nil {
				t.Fatalf("MarshalVars: %v", err)
			}
			back, err := codec.UnmarshalVars(out)
			if err != nil {
				t.Fatalf("UnmarshalVars(%q): %v", out, err)
			}
			if !reflect.DeepEqual(back, vars) {
				t.Errorf("round trip = %#v, want %#v (via %q)", back, vars, out)
			}
		})
	}
}

// TestYAMLCodecUnmarshalVarsShapes pins the edge cases the --vars-file
// reader has always handled: an empty document is an empty mapping, and a
// non-mapping document is a named error rather than a nil map.
func TestYAMLCodecUnmarshalVarsShapes(t *testing.T) {
	t.Parallel()
	codec := yamlCodec{}

	got, err := codec.UnmarshalVars([]byte(""))
	if err != nil {
		t.Fatalf("UnmarshalVars(empty): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("UnmarshalVars(empty) = %#v, want an empty mapping", got)
	}

	if _, err := codec.UnmarshalVars([]byte("- one\n- two\n")); err == nil {
		t.Error("UnmarshalVars(sequence) = nil error, want a rejection")
	} else if !strings.Contains(err.Error(), "must be a mapping") {
		t.Errorf("UnmarshalVars(sequence) error %q should say the document must be a mapping", err)
	}
}

// TestJSON5CodecUnmarshalVarsAcceptsJSON5 is the behaviour change in #519:
// a vars-file with JSON5 syntax used to reach the YAML parser and fail.
// Plain JSON still decodes to the same Go values, since json5.Unmarshal is
// a superset of encoding/json.
func TestJSON5CodecUnmarshalVarsAcceptsJSON5(t *testing.T) {
	t.Parallel()
	codec := json5Codec{}
	want := map[string]interface{}{"app": "web", "port": float64(8080)}

	for _, data := range []string{
		`{"app": "web", "port": 8080}`,
		"{\n  // the app\n  app: 'web',\n  port: 8080,\n}\n",
	} {
		got, err := codec.UnmarshalVars([]byte(data))
		if err != nil {
			t.Errorf("UnmarshalVars(%q): %v", data, err)
			continue
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("UnmarshalVars(%q) = %#v, want %#v", data, got, want)
		}
	}
}

// TestJSON5CodecToYAMLReportsParseProblem pins the diagnostic a malformed
// document produces. docs/json-output.md documents json5_parse as a stable
// machine key, so the Code matters as much as the failure.
func TestJSON5CodecToYAMLReportsParseProblem(t *testing.T) {
	t.Parallel()
	converted, problem := json5Codec{}.ToYAML([]byte("[{tasks: }"))
	if problem == nil {
		t.Fatal("ToYAML(malformed) = nil problem, want a json5_parse finding")
	}
	if problem.Code != "json5_parse" {
		t.Errorf("problem code = %q, want json5_parse", problem.Code)
	}
	if converted != nil {
		t.Errorf("ToYAML(malformed) returned %q, want nil bytes so a caller cannot parse a half-conversion", converted)
	}
}

// TestJSON5CodecLintReadsOriginalBytes is why Lint is a separate method:
// ToYAML's output has already lost the duplicate, so linting the converted
// bytes would never find one.
func TestJSON5CodecLintReadsOriginalBytes(t *testing.T) {
	t.Parallel()
	codec := json5Codec{}
	src := []byte("[{name: 'a', name: 'b', tasks: []}]")

	problems := codec.Lint(src)
	if len(problems) != 1 {
		t.Fatalf("Lint(duplicate) = %d problems, want 1", len(problems))
	}
	if problems[0].Code != "duplicate_key" {
		t.Errorf("problem code = %q, want duplicate_key", problems[0].Code)
	}

	converted, problem := codec.ToYAML(src)
	if problem != nil {
		t.Fatalf("ToYAML: %s", problem.Message)
	}
	if got := codec.Lint(converted); len(got) != 0 {
		t.Errorf("Lint(converted) = %v; the duplicate should already be gone, which is why Lint takes the original", got)
	}
}
