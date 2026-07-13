package tasks

import (
	"strings"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

func TestFormatNormalizesIndent(t *testing.T) {
	in := `---
- tasks:
            - dokku_app:
                  app: x
`
	out, err := Format([]byte(in))
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	body := string(out)
	// yaml.v3's SetIndent(2) renders the recipe sequence at column 0,
	// the per-play tasks: key at column 2, and each task entry at
	// column 4 - the indent step is 2, the visual offset for nested
	// sequence-of-mappings is twice that.
	wantLines := []string{
		"- tasks:",
		"    - dokku_app:",
		"        app: x",
	}
	for _, want := range wantLines {
		if !strings.Contains(body, want+"\n") && !strings.HasSuffix(body, want) {
			t.Errorf("expected canonical line %q in output:\n%s", want, body)
		}
	}
}

func TestFormatReordersEnvelopeKeys(t *testing.T) {
	in := `---
- tasks:
    - dokku_app:
        app: web
      name: configure web
`
	out, err := Format([]byte(in))
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	body := string(out)
	idxName := strings.Index(body, "name:")
	idxType := strings.Index(body, "dokku_app:")
	if idxName < 0 || idxType < 0 {
		t.Fatalf("missing expected keys:\n%s", body)
	}
	if !(idxName < idxType) {
		t.Errorf("name should sort before task-type key:\n%s", body)
	}
}

func TestFormatReordersPlayKeys(t *testing.T) {
	in := `---
- tasks:
    - dokku_app:
        app: web
  inputs:
    - name: app
      default: web
`
	out, err := Format([]byte(in))
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	body := string(out)
	idxInputs := strings.Index(body, "inputs:")
	idxTasks := strings.Index(body, "tasks:")
	if idxInputs < 0 || idxTasks < 0 {
		t.Fatalf("missing expected keys:\n%s", body)
	}
	if !(idxInputs < idxTasks) {
		t.Errorf("inputs should sort before tasks:\n%s", body)
	}
}

func TestFormatPreservesHeadComment(t *testing.T) {
	in := `---
- tasks:
    # a comment about the next task
    - dokku_app:
        app: web
`
	out, err := Format([]byte(in))
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	body := string(out)
	if !strings.Contains(body, "# a comment about the next task") {
		t.Errorf("head comment not preserved:\n%s", body)
	}
}

func TestFormatPreservesLineComment(t *testing.T) {
	in := `---
- tasks:
    - dokku_app:
        app: web # inline note
`
	out, err := Format([]byte(in))
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	body := string(out)
	if !strings.Contains(body, "# inline note") {
		t.Errorf("line comment not preserved:\n%s", body)
	}
}

func TestFormatAlreadyCanonicalIsByteIdentical(t *testing.T) {
	canonical := mustFormat(t, `---
- inputs:
    - name: app
      default: web
  tasks:
    - name: configure web
      dokku_app:
        app: web
`)
	out, err := Format(canonical)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if string(out) != string(canonical) {
		t.Errorf("already-canonical input changed on second Format pass:\nbefore:\n%s\nafter:\n%s", canonical, out)
	}
}

func TestFormatStripsTrailingWhitespace(t *testing.T) {
	in := "---\n- tasks:   \n    - dokku_app:\n        app: x   \n"
	out, err := Format([]byte(in))
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	for i, line := range strings.Split(string(out), "\n") {
		if strings.HasSuffix(line, " ") || strings.HasSuffix(line, "\t") {
			t.Errorf("line %d has trailing whitespace: %q", i, line)
		}
	}
}

func TestFormatEnsuresTrailingNewline(t *testing.T) {
	in := "---\n- tasks:\n    - dokku_app:\n        app: x"
	out, err := Format([]byte(in))
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if !strings.HasSuffix(string(out), "\n") {
		t.Errorf("output missing trailing newline:\n%q", out)
	}
	if strings.HasSuffix(string(out), "\n\n") {
		t.Errorf("output ends with multiple newlines:\n%q", out)
	}
}

func TestFormatBlankLineBetweenPlays(t *testing.T) {
	in := `---
- tasks:
    - dokku_app:
        app: a
- tasks:
    - dokku_app:
        app: b
`
	out, err := Format([]byte(in))
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	body := string(out)
	// Find both top-level "- tasks:" lines and confirm at least one
	// blank line separates them.
	if !strings.Contains(body, "- tasks:\n\n- tasks:") &&
		!strings.Contains(body, "        app: a\n\n- tasks:") {
		t.Errorf("expected blank line between top-level plays:\n%s", body)
	}
}

func TestFormatBlankLineBetweenTasks(t *testing.T) {
	in := `---
- tasks:
    - dokku_app:
        app: a
    - dokku_config:
        app: a
        config:
          KEY: value
`
	out, err := Format([]byte(in))
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	body := string(out)
	if !strings.Contains(body, "\n\n    - dokku_config:") {
		t.Errorf("expected blank line between top-level task entries:\n%s", body)
	}
}

func TestEquivalentNodesEqual(t *testing.T) {
	a := mustParse(t, "{a: 1, b: [x, y]}")
	b := mustParse(t, "{b: [x, y], a: 1}")
	if !equivalentNodes(documentBody(a), documentBody(b)) {
		t.Error("expected mappings with same key set to be equivalent regardless of order")
	}
}

func TestEquivalentNodesDifferentValue(t *testing.T) {
	a := mustParse(t, "{a: 1, b: [x, y]}")
	b := mustParse(t, "{a: 1, b: [x, z]}")
	if equivalentNodes(documentBody(a), documentBody(b)) {
		t.Error("sequences with different elements should not be equivalent")
	}
}

func TestEquivalentNodesDifferentKeys(t *testing.T) {
	a := mustParse(t, "{a: 1, b: 2}")
	b := mustParse(t, "{a: 1, c: 2}")
	if equivalentNodes(documentBody(a), documentBody(b)) {
		t.Error("mappings with different key sets should not be equivalent")
	}
}

func TestFormatRoundTripStaysValidatable(t *testing.T) {
	in := `---
- inputs:
    - name: app
      required: true
  tasks:
    - dokku_app:
        app: "{{ .app }}"
`
	formatted, err := Format([]byte(in))
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	// re-format must converge: the second pass equals the first.
	again, err := Format(formatted)
	if err != nil {
		t.Fatalf("Format (second pass): %v", err)
	}
	if string(again) != string(formatted) {
		t.Errorf("formatter is not idempotent:\nfirst:\n%s\nsecond:\n%s", formatted, again)
	}
}

func TestFormatRejectsBrokenYAML(t *testing.T) {
	_, err := Format([]byte("- a: [b\n"))
	if err == nil {
		t.Error("expected parse error on malformed YAML")
	}
}

func TestFormatKeepsTaskHeadCommentAttached(t *testing.T) {
	in := `---
- tasks:
    - dokku_app:
        app: a
    # comment for b
    - dokku_app:
        app: b
`
	body := string(mustFormat(t, in))
	// The blank separator must sit above the comment, keeping it attached to
	// the task it documents.
	if !strings.Contains(body, "\n\n    # comment for b\n") {
		t.Errorf("expected a blank line above the head comment:\n%s", body)
	}
	if strings.Contains(body, "# comment for b\n\n") {
		t.Errorf("blank line was inserted between the comment and its task:\n%s", body)
	}
	// Idempotent: fmt --check right after fmt must pass.
	if again := string(mustFormat(t, body)); again != body {
		t.Errorf("formatter is not idempotent:\nfirst:\n%s\nsecond:\n%s", body, again)
	}
}

func TestFormatKeepsPlayHeadCommentAttached(t *testing.T) {
	in := `---
- name: play1
  tasks:
    - dokku_app:
        app: a
# comment for play2
- name: play2
  tasks:
    - dokku_app:
        app: b
`
	body := string(mustFormat(t, in))
	if !strings.Contains(body, "\n\n# comment for play2\n") {
		t.Errorf("expected a blank line above the play head comment:\n%s", body)
	}
	if strings.Contains(body, "# comment for play2\n\n") {
		t.Errorf("blank line was inserted between the comment and its play:\n%s", body)
	}
	if again := string(mustFormat(t, body)); again != body {
		t.Errorf("formatter is not idempotent:\nfirst:\n%s\nsecond:\n%s", body, again)
	}
}

func TestFormatDoesNotSplitBlockScalarWithHashLine(t *testing.T) {
	// A block scalar whose last line starts with "#" is content, not a YAML
	// comment; the inter-task blank must go between the two tasks, never
	// inside the block scalar.
	in := `---
- tasks:
    - dokku_command:
        command: |
          echo hi
          # not a yaml comment
    - dokku_app:
        app: b
`
	body := string(mustFormat(t, in))
	if strings.Contains(body, "echo hi\n\n") {
		t.Errorf("blank line was spliced into the block scalar:\n%s", body)
	}
	if !strings.Contains(body, "# not a yaml comment") {
		t.Errorf("block scalar content was lost:\n%s", body)
	}
	if again := string(mustFormat(t, body)); again != body {
		t.Errorf("formatter is not idempotent:\nfirst:\n%s\nsecond:\n%s", body, again)
	}
}

func mustFormat(t *testing.T, in string) []byte {
	t.Helper()
	out, err := Format([]byte(in))
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	return out
}

func mustParse(t *testing.T, in string) *yaml.Node {
	t.Helper()
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(in), &n); err != nil {
		t.Fatalf("yaml.Unmarshal: %v", err)
	}
	return &n
}
