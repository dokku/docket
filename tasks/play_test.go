package tasks

import (
	"strings"
	"testing"

	_ "github.com/gliderlabs/sigil/builtin"
)

// TestGetPlaysSinglePlayBackCompat ensures a single-play file - the legacy
// shape - produces one Play with the expected name/tasks. This is the
// shape every existing tasks/*_test.go file relies on through the
// GetTasks back-compat shim.
func TestGetPlaysSinglePlayBackCompat(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: create app
      dokku_app:
        app: test-app
`)
	plays, err := GetPlays(data, map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("GetPlays: %v", err)
	}
	if len(plays) != 1 {
		t.Fatalf("expected 1 play, got %d", len(plays))
	}
	if plays[0].Name != "tasks" {
		t.Errorf("auto-name = %q, want %q (single-play files keep the legacy header)", plays[0].Name, "tasks")
	}
	if len(plays[0].Tasks.Keys()) != 1 {
		t.Errorf("expected 1 task in play, got %d", len(plays[0].Tasks.Keys()))
	}
	if plays[0].Tasks.Get("create app") == nil {
		t.Error("task 'create app' missing")
	}
}

func TestGetPlaysMultiPlayBuildsBoth(t *testing.T) {
	data := []byte(`---
- name: api
  tasks:
    - name: create api
      dokku_app:
        app: api
- name: worker
  tasks:
    - name: create worker
      dokku_app:
        app: worker
`)
	plays, err := GetPlays(data, map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("GetPlays: %v", err)
	}
	if len(plays) != 2 {
		t.Fatalf("expected 2 plays, got %d", len(plays))
	}
	if plays[0].Name != "api" || plays[1].Name != "worker" {
		t.Errorf("play names = [%q, %q], want [api, worker]", plays[0].Name, plays[1].Name)
	}
	if plays[0].Tasks.Get("create api") == nil {
		t.Error("api play missing 'create api' task")
	}
	if plays[1].Tasks.Get("create worker") == nil {
		t.Error("worker play missing 'create worker' task")
	}
}

// TestGetPlaysPerPlayInputsScopeAcrossPlays is the spec's headline example:
// two plays both declare an `app` input with different defaults; the task
// in each play must see its own play's value.
func TestGetPlaysPerPlayInputsScopeAcrossPlays(t *testing.T) {
	data := []byte(`---
- name: api
  inputs:
    - name: app
      default: api
  tasks:
    - name: create
      dokku_app:
        app: "{{ .app }}"
- name: worker
  inputs:
    - name: app
      default: worker
  tasks:
    - name: create
      dokku_app:
        app: "{{ .app }}"
`)
	plays, err := GetPlays(data, map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("GetPlays: %v", err)
	}
	if len(plays) != 2 {
		t.Fatalf("expected 2 plays, got %d", len(plays))
	}

	apiTask, ok := plays[0].Tasks.Get("create").(*AppTask)
	if !ok {
		t.Fatalf("api task is %T, want *AppTask", plays[0].Tasks.Get("create"))
	}
	if apiTask.App != "api" {
		t.Errorf("api task app = %q, want %q (per-play default must win in play scope)", apiTask.App, "api")
	}

	workerTask, ok := plays[1].Tasks.Get("create").(*AppTask)
	if !ok {
		t.Fatalf("worker task is %T, want *AppTask", plays[1].Tasks.Get("create"))
	}
	if workerTask.App != "worker" {
		t.Errorf("worker task app = %q, want %q (per-play default must win in play scope)", workerTask.App, "worker")
	}
}

// TestGetPlaysUserOverrideBeatsPlayInputs pins the precedence rule that
// CLI / vars-file overrides win over play-level inputs even when both
// declare the same key. The userSet map carries the user-overridden keys.
func TestGetPlaysUserOverrideBeatsPlayInputs(t *testing.T) {
	data := []byte(`---
- name: api
  inputs:
    - name: app
      default: api
  tasks:
    - name: create
      dokku_app:
        app: "{{ .app }}"
`)
	context := map[string]interface{}{"app": "from-cli"}
	userSet := map[string]bool{"app": true}

	plays, err := GetPlays(data, context, userSet)
	if err != nil {
		t.Fatalf("GetPlays: %v", err)
	}
	task, ok := plays[0].Tasks.Get("create").(*AppTask)
	if !ok {
		t.Fatalf("task is %T, want *AppTask", plays[0].Tasks.Get("create"))
	}
	if task.App != "from-cli" {
		t.Errorf("app = %q, want %q (user override must beat play default)", task.App, "from-cli")
	}
}

func TestGetPlaysAutoNamesPlay(t *testing.T) {
	data := []byte(`---
- tasks:
    - dokku_app:
        app: a
- tasks:
    - dokku_app:
        app: b
`)
	plays, err := GetPlays(data, map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("GetPlays: %v", err)
	}
	want := []string{"play #1", "play #2"}
	for i, p := range plays {
		if p.Name != want[i] {
			t.Errorf("plays[%d].Name = %q, want %q", i, p.Name, want[i])
		}
	}
}

func TestGetPlaysWhenCompiledAndExposed(t *testing.T) {
	data := []byte(`---
- name: api
  when: 'env == "prod"'
  tasks:
    - dokku_app:
        app: api
`)
	plays, err := GetPlays(data, map[string]interface{}{"env": "prod"}, nil)
	if err != nil {
		t.Fatalf("GetPlays: %v", err)
	}
	if !plays[0].HasWhen() {
		t.Fatal("HasWhen should be true")
	}
	if plays[0].WhenProgram() == nil {
		t.Error("WhenProgram should be non-nil after compile")
	}
}

func TestGetPlaysInvalidWhenReturnsParseError(t *testing.T) {
	data := []byte(`---
- name: api
  when: 'this is not valid expr ('
  tasks:
    - dokku_app:
        app: api
`)
	_, err := GetPlays(data, map[string]interface{}{}, nil)
	if err == nil {
		t.Fatal("expected parse error for malformed when:")
	}
	if !strings.Contains(err.Error(), "when compile error") {
		t.Errorf("expected when compile error, got: %v", err)
	}
}

// TestGetPlaysTagsPropagateToEnvelopes verifies that the play's tags get
// folded into every task envelope so the existing FilterByTags helper
// treats them uniformly. The api play's task ends up with both its
// per-task tag (`deploy`) and the play tag (`api`).
func TestGetPlaysTagsPropagateToEnvelopes(t *testing.T) {
	data := []byte(`---
- name: api
  tags: [api]
  tasks:
    - name: create
      tags: [deploy]
      dokku_app:
        app: api
`)
	plays, err := GetPlays(data, map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("GetPlays: %v", err)
	}
	env := plays[0].Tasks.GetEnvelope("create")
	if env == nil {
		t.Fatal("envelope missing")
	}
	if !env.HasTag("api") || !env.HasTag("deploy") {
		t.Errorf("envelope tags = %v, want both api and deploy", env.Tags)
	}
}

func TestGetPlaysTagsScalarFormDecodes(t *testing.T) {
	data := []byte(`---
- name: api
  tags: api
  tasks:
    - dokku_app:
        app: api
`)
	plays, err := GetPlays(data, map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("GetPlays: %v", err)
	}
	if len(plays[0].Tags) != 1 || plays[0].Tags[0] != "api" {
		t.Errorf("tags = %v, want [api]", plays[0].Tags)
	}
}

// TestGetPlaysEmptyRecipe maps the legacy GetTasks "no recipe found"
// error onto the new entrypoint.
func TestGetPlaysEmptyRecipe(t *testing.T) {
	_, err := GetPlays([]byte("---\n"), map[string]interface{}{}, nil)
	if err == nil {
		t.Fatal("expected error for empty recipe")
	}
	if !strings.Contains(err.Error(), "no recipe found") {
		t.Errorf("expected 'no recipe found' error, got: %v", err)
	}
}

// TestBuildPerPlayContextRespectsUserSet covers the precedence merge in
// isolation: file-level value stays when no play override; play override
// wins when key is unset by user; user override always wins.
func TestBuildPerPlayContextRespectsUserSet(t *testing.T) {
	base := map[string]interface{}{
		"file_only": "file_value",
		"shared":    "file_shared",
		"user_set":  "from_cli",
	}
	playInputs := []Input{
		{Name: "shared", Default: "play_shared"},
		{Name: "user_set", Default: "play_user_set"},
		{Name: "play_only", Default: "play_only_value"},
	}
	userSet := map[string]bool{"user_set": true}

	out := BuildPerPlayContext(base, playInputs, userSet)

	cases := map[string]string{
		"file_only": "file_value",      // untouched
		"shared":    "play_shared",     // play overrides file default
		"user_set":  "from_cli",        // user override beats play default
		"play_only": "play_only_value", // play-only value layers in
	}
	for k, want := range cases {
		if got, _ := out[k].(string); got != want {
			t.Errorf("ctx[%q] = %v, want %q", k, out[k], want)
		}
	}

	// Base must not be mutated.
	if base["shared"] != "file_shared" {
		t.Errorf("base mutated: shared = %v", base["shared"])
	}
	if _, ok := base["play_only"]; ok {
		t.Errorf("base must not gain play_only key; got %v", base["play_only"])
	}
}

func TestMergePlayTagsDedupsAndPreservesOrder(t *testing.T) {
	got := mergePlayTags([]string{"deploy"}, []string{"api", "deploy"})
	want := []string{"deploy", "api"}
	if len(got) != len(want) {
		t.Fatalf("len = %d, want %d (got %v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("got[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestMergePlayTagsEmptyPlay(t *testing.T) {
	in := []string{"a", "b"}
	got := mergePlayTags(in, nil)
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Errorf("got %v, want %v unchanged", got, in)
	}
}
