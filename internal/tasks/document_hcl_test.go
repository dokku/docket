package tasks

import (
	"strings"
	"testing"
)

// hclToYAML converts an HCL recipe to canonical YAML, which is the readable
// way to assert what a block mapped onto.
func hclToYAML(t *testing.T, in string) string {
	t.Helper()
	out, err := Convert([]byte(in), hclCodec{}, yamlCodec{})
	if err != nil {
		t.Fatalf("Convert(%q) to yaml: %v", in, err)
	}
	return string(out)
}

// yamlToHCL is the other direction.
func yamlToHCL(t *testing.T, in string) string {
	t.Helper()
	out, err := Convert([]byte(in), yamlCodec{}, hclCodec{})
	if err != nil {
		t.Fatalf("Convert(%q) to hcl: %v", in, err)
	}
	return string(out)
}

// TestHCLLabelIsTheNameKey covers the decision the whole task spelling rests
// on: a block's label is the `name` key of the mapping it produces.
//
// For a play and an input that is the only name there is. For a task it is the
// ENVELOPE's name, which is what leaves a `name` attribute in the body
// unambiguously the task's own field - ten registered task types declare one.
func TestHCLLabelIsTheNameKey(t *testing.T) {
	t.Parallel()
	const in = `play "api" {
  input "app" {
    default = "api"
  }

  dokku_service_create "make the database" {
    name    = "mydb"
    service = "postgres"
  }
}
`
	const want = `- name: api
  inputs:
    - name: app
      default: api
  tasks:
    - name: make the database
      dokku_service_create:
        name: mydb
        service: postgres
`
	if got := hclToYAML(t, in); got != want {
		t.Errorf("hcl to yaml =\n%s\nwant\n%s", got, want)
	}
	if got := yamlToHCL(t, want); got != in {
		t.Errorf("yaml to hcl =\n%s\nwant\n%s", got, in)
	}
}

// TestHCLUnlabelledBlocks covers the other half: a label is optional
// everywhere, because YAML does not require a name and every existing recipe
// has to convert.
func TestHCLUnlabelledBlocks(t *testing.T) {
	t.Parallel()
	const in = `play {
  dokku_app {
    app = "web"
  }
}
`
	const want = `- tasks:
    - dokku_app:
        app: web
`
	if got := hclToYAML(t, in); got != want {
		t.Errorf("hcl to yaml =\n%s\nwant\n%s", got, want)
	}
	if got := yamlToHCL(t, want); got != in {
		t.Errorf("yaml to hcl =\n%s\nwant\n%s", got, in)
	}
}

// TestHCLDuplicateNamesAreAllowed pins the answer to the question #407 asked.
// A task name is neither required nor unique, because a YAML recipe's is
// neither, and refusing here would make a recipe that converts one way and not
// the other.
func TestHCLDuplicateNamesAreAllowed(t *testing.T) {
	t.Parallel()
	const in = `play "web" {
  dokku_app "create" {
    app = "a"
  }

  dokku_app "create" {
    app = "b"
  }
}
`
	recipe, err := UnmarshalRecipe([]byte(in), FormatNameHCL)
	if err != nil {
		t.Fatalf("UnmarshalRecipe: %v", err)
	}
	if len(recipe) != 1 || len(recipe[0].Tasks) != 2 {
		t.Fatalf("recipe = %d plays with %d tasks, want 1 play with 2", len(recipe), len(recipe[0].Tasks))
	}
	for i, task := range recipe[0].Tasks {
		if task["name"] != "create" {
			t.Errorf("task %d name = %v, want \"create\"", i, task["name"])
		}
	}
}

// TestHCLEnvelopeKeysShareTheTaskBody covers the shorthand's split: an
// envelope key in a task block's body is the envelope's, and everything else
// is the task's.
func TestHCLEnvelopeKeysShareTheTaskBody(t *testing.T) {
	t.Parallel()
	const in = `play "web" {
  dokku_app "create" {
    tags          = ["deploy"]
    when          = "env != \"preview\""
    register      = "result"
    ignore_errors = true
    app           = "web"
    state         = "present"
  }
}
`
	const want = `- name: web
  tasks:
    - name: create
      tags:
        - deploy
      when: env != "preview"
      register: result
      ignore_errors: true
      dokku_app:
        app: web
        state: present
`
	if got := hclToYAML(t, in); got != want {
		t.Errorf("hcl to yaml =\n%s\nwant\n%s", got, want)
	}
	if got := yamlToHCL(t, want); got != in {
		t.Errorf("yaml to hcl =\n%s\nwant\n%s", got, in)
	}
}

// TestHCLGroupClauses covers the try/catch/finally entry, which has no
// task-type key at all and so has to use the general `task` form.
func TestHCLGroupClauses(t *testing.T) {
	t.Parallel()
	const in = `play "web" {
  task "deploy" {
    block {
      dokku_app {
        app = "a"
      }

      dokku_config "inner" {
        app = "a"
      }
    }

    rescue {
      dokku_app {
        app = "b"
      }
    }

    always {
      dokku_app {
        app = "c"
      }
    }
  }
}
`
	const want = `- name: web
  tasks:
    - name: deploy
      block:
        - dokku_app:
            app: a
        - name: inner
          dokku_config:
            app: a
      rescue:
        - dokku_app:
            app: b
      always:
        - dokku_app:
            app: c
`
	if got := hclToYAML(t, in); got != want {
		t.Errorf("hcl to yaml =\n%s\nwant\n%s", got, want)
	}
	if got := yamlToHCL(t, want); got != in {
		t.Errorf("yaml to hcl =\n%s\nwant\n%s", got, in)
	}
}

// TestHCLGeneralTaskFormIsAccepted covers the general form used for an
// ordinary task - which `fmt` never writes, preferring the shorthand, but
// which a hand-written recipe may use and has to read the same way.
func TestHCLGeneralTaskFormIsAccepted(t *testing.T) {
	t.Parallel()
	const in = `play "web" {
  task "create" {
    when = "x"

    dokku_app {
      app = "web"
    }
  }
}
`
	const want = `- name: web
  tasks:
    - name: create
      when: x
      dokku_app:
        app: web
`
	if got := hclToYAML(t, in); got != want {
		t.Errorf("hcl to yaml =\n%s\nwant\n%s", got, want)
	}
	// Written back, it becomes the shorthand: `fmt` has one canonical
	// spelling per entry, and the general form is the fallback.
	const canonical = `play "web" {
  dokku_app "create" {
    when = "x"
    app  = "web"
  }
}
`
	if got := formatHCL(t, in); got != canonical {
		t.Errorf("FormatHCL =\n%s\nwant\n%s", got, canonical)
	}
}

// TestHCLValueTypes covers the scalar and collection mapping, in both
// directions. HCL has one numeric type, so the int/float split is made on
// whether the value has a fractional part.
func TestHCLValueTypes(t *testing.T) {
	t.Parallel()
	const in = `play {
  dokku_app {
    text     = "a"
    enabled  = true
    disabled = false
    nothing  = null
    whole    = 5
    negative = -5
    fraction = 1.5
    list     = [1, "two", true]
    nested   = {
      a = 1
      b = ["x"]
    }
  }
}
`
	const want = `- tasks:
    - dokku_app:
        text: a
        enabled: true
        disabled: false
        nothing: null
        whole: 5
        negative: -5
        fraction: 1.5
        list:
          - 1
          - two
          - true
        nested:
          a: 1
          b:
            - x
`
	if got := hclToYAML(t, in); got != want {
		t.Errorf("hcl to yaml =\n%s\nwant\n%s", got, want)
	}
}

// TestHCLNumbersNormaliseToDecimal covers the spellings YAML has and HCL does
// not, the way JSON5's conversion normalises the same ones.
func TestHCLNumbersNormaliseToDecimal(t *testing.T) {
	t.Parallel()
	got := yamlToHCL(t, "- tasks:\n    - dokku_app:\n        octal: 0o17\n        binary: 0b1010\n        grouped: 1_000\n")
	for _, want := range []string{"octal   = 15", "binary  = 10", "grouped = 1000"} {
		if !strings.Contains(got, want) {
			t.Errorf("hcl =\n%s\nwant it to contain %q", got, want)
		}
	}
}

// TestHCLCommentPlacement covers where each kind of comment lands.
//
// The cursor consumes strictly forwards, so a comment above an item is its
// head comment, one beside a block header belongs to the header, and one
// before a closing brace is the body's foot.
func TestHCLCommentPlacement(t *testing.T) {
	t.Parallel()
	const in = `# top of file
play "web" { # beside the header
  # above the task
  dokku_app "create" {
    app = "web" # beside the value
    # before the closing brace
  }
}
`
	const want = `# top of file
- name: web # beside the header
  tasks:
    # above the task
    - name: create
      dokku_app:
        app: web # beside the value
        # before the closing brace
`
	if got := hclToYAML(t, in); got != want {
		t.Errorf("hcl to yaml =\n%s\nwant\n%s", got, want)
	}
	if got := yamlToHCL(t, want); got != in {
		t.Errorf("yaml to hcl =\n%s\nwant\n%s", got, in)
	}
}

// TestHCLCommentSyntaxesAllRead covers the three spellings HCL accepts on the
// way in. They all come back as `#`, which is the one canonical output uses.
func TestHCLCommentSyntaxesAllRead(t *testing.T) {
	t.Parallel()
	const in = `// a line comment
/* a block comment */
# a hash comment
play {
  dokku_app {
    app = "web"
  }
}
`
	const want = `# a line comment
# a block comment
# a hash comment
play {
  dokku_app {
    app = "web"
  }
}
`
	if got := formatHCL(t, in); got != want {
		t.Errorf("FormatHCL =\n%s\nwant\n%s", got, want)
	}
}

// TestHCLAnchorsAreInlined covers what Convert does for a target with no way
// to write sharing down. It is the same flattening JSON5 gets, and it happens
// in Convert rather than in a codec for exactly that reason.
func TestHCLAnchorsAreInlined(t *testing.T) {
	t.Parallel()
	got := yamlToHCL(t, "- tasks:\n    - dokku_config:\n        config: &base {A: \"1\"}\n    - dokku_config:\n        config:\n          <<: *base\n          B: \"2\"\n")
	if strings.Contains(got, "&base") || strings.Contains(got, "*base") {
		t.Errorf("hcl =\n%s\nwant the anchor inlined", got)
	}
	if strings.Count(got, `A = "1"`) != 2 {
		t.Errorf("hcl =\n%s\nwant the merged value written out at both sites", got)
	}
}

// TestHCLCommentsInsideCollections covers a comment written inside an object
// or tuple expression rather than in a block body.
//
// It is where a comment about one config value goes, so it has to stay beside
// that value rather than being hoisted out to the enclosing body - which is
// what happens to a comment the walk does not claim.
func TestHCLCommentsInsideCollections(t *testing.T) {
	t.Parallel()
	const in = `play {
  dokku_config {
    config = {
      # about the prompt
      PROMPT = "$${USER}" # the value is ${USER}
      # before the closing brace
    }
    domains = [
      # the primary
      "a.example.com", # first
      "b.example.com",
    ]
  }
}
`
	const want = `- tasks:
    - dokku_config:
        config:
          # about the prompt
          PROMPT: ${USER} # the value is ${USER}
          # before the closing brace
        domains:
          # the primary
          - a.example.com # first
          - b.example.com
`
	if got := hclToYAML(t, in); got != want {
		t.Errorf("hcl to yaml =\n%s\nwant\n%s", got, want)
	}
	if got := yamlToHCL(t, want); got != in {
		t.Errorf("yaml to hcl =\n%s\nwant\n%s", got, in)
	}
	if got := formatHCL(t, in); got != in {
		t.Errorf("FormatHCL =\n%s\nwant it unchanged\n%s", got, in)
	}
}

// TestHCLTaskTypeCommentConverges covers where a comment written about the
// task-type key itself lands.
//
// The shorthand merges the envelope and the task body into one HCL body, so
// the `dokku_app:` key it was attached to no longer exists. It is emitted
// immediately above the first of the task's own fields, which is where the
// decoder looks for it - anywhere else and the next read would take it for an
// envelope attribute's comment and the round trip would drift.
func TestHCLTaskTypeCommentConverges(t *testing.T) {
	t.Parallel()
	const in = `- tasks:
    - name: x
      when: y
      # about the task type
      dokku_app:
        app: z
`
	const want = `play {
  dokku_app "x" {
    when = "y"
    # about the task type
    app = "z"
  }
}
`
	got := yamlToHCL(t, in)
	if got != want {
		t.Fatalf("yaml to hcl =\n%s\nwant\n%s", got, want)
	}
	back, err := Convert([]byte(got), hclCodec{}, yamlCodec{})
	if err != nil {
		t.Fatalf("Convert back: %v", err)
	}
	canonical, err := Format([]byte(in))
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if string(back) != string(canonical) {
		t.Errorf("round trip moved the comment:\nwant:\n%s\ngot:\n%s", canonical, back)
	}
}
