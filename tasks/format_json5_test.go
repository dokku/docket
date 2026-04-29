package tasks

import (
	"strings"
	"testing"
)

func TestFormatJSON5IdempotentOnCanonicalInput(t *testing.T) {
	in := `[
  {
    name: "api",
    tasks: [
      {
        name: "create",
        dokku_app: {
          app: "api",
        },
      },
    ],
  },
]
`
	out, err := FormatJSON5([]byte(in))
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	if string(out) != in {
		t.Errorf("non-idempotent format:\nwant:\n%s\ngot:\n%s", in, string(out))
	}
}

func TestFormatJSON5ReordersPlayKeys(t *testing.T) {
	in := []byte(`[{ tasks: [], inputs: [], name: "api" }]`)
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	// name should come first, inputs second, tasks last among canonical keys.
	idxName := strings.Index(string(out), "name:")
	idxInputs := strings.Index(string(out), "inputs:")
	idxTasks := strings.Index(string(out), "tasks:")
	if !(idxName < idxInputs && idxInputs < idxTasks) {
		t.Errorf("canonical key order not enforced:\n%s", out)
	}
}

func TestFormatJSON5ReordersTaskEnvelopeKeys(t *testing.T) {
	in := []byte(`[{ tasks: [{ dokku_app: { app: "api" }, name: "create" }] }]`)
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	idxName := strings.Index(string(out), "name:")
	idxDokku := strings.Index(string(out), "dokku_app:")
	if idxName < 0 || idxDokku < 0 || idxName >= idxDokku {
		t.Errorf("envelope key order not enforced (name should come before task-type):\n%s", out)
	}
}

func TestFormatJSON5PreservesLineComments(t *testing.T) {
	in := []byte(`[
  // top of recipe
  {
    tasks: [
      {
        name: "create", // inline comment
        dokku_app: { app: "api" },
      },
    ],
  },
]`)
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	if !strings.Contains(string(out), "// top of recipe") {
		t.Errorf("head comment lost:\n%s", out)
	}
	if !strings.Contains(string(out), "// inline comment") {
		t.Errorf("trailing line comment lost:\n%s", out)
	}
}

func TestFormatJSON5PreservesBlockComments(t *testing.T) {
	in := []byte(`[
  /* preface */
  { tasks: [] },
]`)
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	if !strings.Contains(string(out), "/* preface */") {
		t.Errorf("block comment lost:\n%s", out)
	}
}

func TestFormatJSON5BlankLinesBetweenPlays(t *testing.T) {
	in := []byte(`[
  { name: "a", tasks: [] },
  { name: "b", tasks: [] },
]`)
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	// Expect exactly one blank line between the two plays.
	if !strings.Contains(string(out), "},\n\n  {") {
		t.Errorf("blank line between plays missing:\n%s", out)
	}
}

func TestFormatJSON5RejectsInvalidInput(t *testing.T) {
	in := []byte(`[{ tasks: [`)
	_, err := FormatJSON5(in)
	if err == nil {
		t.Fatal("expected error on truncated input")
	}
	if !strings.Contains(err.Error(), "json5 parse error") {
		t.Errorf("error = %q, want json5 parse error", err.Error())
	}
}

func TestFormatJSON5HandlesEmptyArray(t *testing.T) {
	in := []byte(`[]`)
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	got := strings.TrimSpace(string(out))
	if got != "[]" {
		t.Errorf("empty array not preserved: %q", got)
	}
}

func TestFormatJSON5InlinesScalarArrays(t *testing.T) {
	in := []byte(`[{ tasks: [{ dokku_domains: { app: "api", domains: ["a.example.com", "b.example.com"] } }] }]`)
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	if !strings.Contains(string(out), `["a.example.com", "b.example.com"]`) {
		t.Errorf("scalar array should be inlined:\n%s", out)
	}
}

func TestFormatJSON5RoundTripsTrailingCommas(t *testing.T) {
	in := []byte(`[{ tasks: [{ dokku_app: { app: "api" }, }, ], }, ]`)
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	// Re-parse to confirm valid JSON5.
	if _, err := parseJSON5(out); err != nil {
		t.Fatalf("formatted output does not re-parse: %v\n%s", err, out)
	}
}

func TestFormatJSON5SigilTemplatesSurvive(t *testing.T) {
	in := []byte(`[{ tasks: [{ dokku_app: { app: "{{ .app }}" } }] }]`)
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	if !strings.Contains(string(out), `"{{ .app }}"`) {
		t.Errorf("sigil template lost:\n%s", out)
	}
}
