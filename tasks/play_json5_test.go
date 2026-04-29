package tasks

import (
	"strings"
	"testing"

	_ "github.com/gliderlabs/sigil/builtin"
)

func TestGetPlaysWithFormatJSON5SinglePlay(t *testing.T) {
	data := []byte(`[
  {
    tasks: [
      {
        name: "create app",
        dokku_app: {
          app: "test-app",
        },
      },
    ],
  },
]
`)
	plays, err := GetPlaysWithFormat(data, FormatNameJSON5, map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("GetPlaysWithFormat: %v", err)
	}
	if len(plays) != 1 {
		t.Fatalf("expected 1 play, got %d", len(plays))
	}
	if plays[0].Name != "tasks" {
		t.Errorf("auto-name = %q, want %q", plays[0].Name, "tasks")
	}
	if plays[0].Tasks.Get("create app") == nil {
		t.Error("task 'create app' missing")
	}
}

func TestGetPlaysWithFormatJSON5MultiPlay(t *testing.T) {
	data := []byte(`[
  {
    name: "api",
    tasks: [{ name: "create api", dokku_app: { app: "api" } }],
  },
  {
    name: "worker",
    tasks: [{ name: "create worker", dokku_app: { app: "worker" } }],
  },
]`)
	plays, err := GetPlaysWithFormat(data, FormatNameJSON5, map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("GetPlaysWithFormat: %v", err)
	}
	if len(plays) != 2 {
		t.Fatalf("expected 2 plays, got %d", len(plays))
	}
	if plays[0].Name != "api" || plays[1].Name != "worker" {
		t.Errorf("plays = [%q, %q]", plays[0].Name, plays[1].Name)
	}
}

func TestGetPlaysWithFormatJSON5HandlesComments(t *testing.T) {
	data := []byte(`[
  // top-level comment
  {
    /* play preface */
    tasks: [
      // before the task
      { name: "x", dokku_app: { app: "a" } }, // inline
    ],
  },
]`)
	plays, err := GetPlaysWithFormat(data, FormatNameJSON5, map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("GetPlaysWithFormat: %v", err)
	}
	if len(plays) != 1 || plays[0].Tasks.Get("x") == nil {
		t.Error("expected single play with task 'x'")
	}
}

func TestGetPlaysWithFormatJSON5SigilTemplate(t *testing.T) {
	data := []byte(`[
  {
    tasks: [
      { name: "render", dokku_app: { app: "{{ .name }}" } },
    ],
  },
]`)
	plays, err := GetPlaysWithFormat(data, FormatNameJSON5, map[string]interface{}{"name": "myapp"}, nil)
	if err != nil {
		t.Fatalf("GetPlaysWithFormat: %v", err)
	}
	if len(plays) != 1 {
		t.Fatalf("expected 1 play, got %d", len(plays))
	}
	if plays[0].Tasks.Get("render") == nil {
		t.Fatal("task 'render' missing")
	}
}

func TestGetPlaysWithFormatJSON5RejectsInvalid(t *testing.T) {
	data := []byte(`{"not": "an array"}`)
	_, err := GetPlaysWithFormat(data, FormatNameJSON5, map[string]interface{}{}, nil)
	if err == nil {
		t.Fatal("expected error for non-array root")
	}
}

func TestUnmarshalRecipeYAMLAndJSON5MatchStructurally(t *testing.T) {
	yamlData := []byte(`---
- name: api
  inputs:
    - name: app
      default: api
  tasks:
    - name: create
      dokku_app:
        app: api
`)
	jsonData := []byte(`[
  {
    name: "api",
    inputs: [{ name: "app", default: "api" }],
    tasks: [
      { name: "create", dokku_app: { app: "api" } },
    ],
  },
]`)
	yamlRecipe, err := UnmarshalRecipe(yamlData, FormatYAML)
	if err != nil {
		t.Fatalf("yaml UnmarshalRecipe: %v", err)
	}
	jsonRecipe, err := UnmarshalRecipe(jsonData, FormatNameJSON5)
	if err != nil {
		t.Fatalf("json5 UnmarshalRecipe: %v", err)
	}
	if len(yamlRecipe) != len(jsonRecipe) {
		t.Fatalf("play counts differ: yaml=%d json5=%d", len(yamlRecipe), len(jsonRecipe))
	}
	if yamlRecipe[0].Name != jsonRecipe[0].Name {
		t.Errorf("play name: yaml=%q json5=%q", yamlRecipe[0].Name, jsonRecipe[0].Name)
	}
	if len(yamlRecipe[0].Inputs) != len(jsonRecipe[0].Inputs) {
		t.Errorf("input count: yaml=%d json5=%d", len(yamlRecipe[0].Inputs), len(jsonRecipe[0].Inputs))
	}
	if len(yamlRecipe[0].Tasks) != len(jsonRecipe[0].Tasks) {
		t.Errorf("task count: yaml=%d json5=%d", len(yamlRecipe[0].Tasks), len(jsonRecipe[0].Tasks))
	}
}

func TestValidateAcceptsJSON5(t *testing.T) {
	data := []byte(`[
  {
    inputs: [{ name: "app", default: "api" }],
    tasks: [
      { name: "create", dokku_app: { app: "{{ .app }}" } },
    ],
  },
]`)
	problems := Validate(data, ValidateOptions{Format: FormatNameJSON5})
	if len(problems) != 0 {
		var msgs []string
		for _, p := range problems {
			msgs = append(msgs, p.Code+":"+p.Message)
		}
		t.Errorf("unexpected validation problems for JSON5 input: %s", strings.Join(msgs, " | "))
	}
}

func TestValidateRejectsMalformedJSON5(t *testing.T) {
	data := []byte(`[{ tasks: [`)
	problems := Validate(data, ValidateOptions{Format: FormatNameJSON5})
	if len(problems) == 0 {
		t.Fatal("expected at least one problem for malformed JSON5")
	}
	if problems[0].Code != "json5_parse" {
		t.Errorf("first problem code = %q, want json5_parse", problems[0].Code)
	}
}
