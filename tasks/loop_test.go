package tasks

import (
	"strings"
	"testing"
)

func TestLoopLiteralListExpandsOneEnvelopePerItem(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: deploy app
      loop: [api, worker, web]
      dokku_app:
        app: "{{ .item }}"
`)
	out, err := GetTasks(data, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetTasks: %v", err)
	}
	keys := out.Keys()
	if len(keys) != 3 {
		t.Fatalf("expected 3 expansions, got %d (%v)", len(keys), keys)
	}
	wantSuffixes := []string{"(item=api)", "(item=worker)", "(item=web)"}
	for i, k := range keys {
		if !strings.HasSuffix(k, wantSuffixes[i]) {
			t.Errorf("key[%d] = %q; want suffix %q", i, k, wantSuffixes[i])
		}
	}

	want := []string{"api", "worker", "web"}
	for i, k := range keys {
		env := out.GetEnvelope(k)
		if !env.IsLoopExpansion {
			t.Errorf("key[%d] %q is not flagged as a loop expansion", i, k)
		}
		if env.LoopIndex != i {
			t.Errorf("key[%d] LoopIndex = %d, want %d", i, env.LoopIndex, i)
		}
		appTask, ok := env.Task.(*AppTask)
		if !ok {
			t.Fatalf("key[%d] task type %T, want *AppTask", i, env.Task)
		}
		if appTask.App != want[i] {
			t.Errorf("key[%d] app = %q, want %q", i, appTask.App, want[i])
		}
	}
}

func TestLoopExprListExpandsByEvaluation(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: deploy app
      loop: '["alpha", "beta"]'
      dokku_app:
        app: "{{ .item }}"
`)
	out, err := GetTasks(data, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetTasks: %v", err)
	}
	keys := out.Keys()
	if len(keys) != 2 {
		t.Fatalf("expected 2 expansions, got %d (%v)", len(keys), keys)
	}
	if got := out.GetEnvelope(keys[0]).Task.(*AppTask).App; got != "alpha" {
		t.Errorf("first app = %q, want alpha", got)
	}
}

func TestLoopExprNonListErrors(t *testing.T) {
	data := []byte(`---
- tasks:
    - loop: '"not a list"'
      dokku_app:
        app: x
`)
	_, err := GetTasks(data, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for non-list loop expression")
	}
	if !strings.Contains(err.Error(), "list") {
		t.Errorf("expected message to mention list, got: %v", err)
	}
}

func TestLoopComplexItemsFallBackToIndexSuffix(t *testing.T) {
	// Using a list of mappings means renderItemForName cannot produce a
	// readable suffix and the fallback `(item=#<index>)` form kicks in.
	data := []byte(`---
- tasks:
    - name: deploy app
      loop:
        - {app: api, port: 8080}
        - {app: worker, port: 8081}
      dokku_app:
        app: "{{ .item.app }}"
`)
	out, err := GetTasks(data, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetTasks: %v", err)
	}
	keys := out.Keys()
	if len(keys) != 2 {
		t.Fatalf("expected 2 expansions, got %d", len(keys))
	}
	if !strings.HasSuffix(keys[0], "(item=#0)") || !strings.HasSuffix(keys[1], "(item=#1)") {
		t.Errorf("expected #0 / #1 suffix, got %v", keys)
	}
}

func TestLoopIndexExposedToBody(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: deploy
      loop: [a, b, c]
      dokku_app:
        app: "app-{{ .index }}"
`)
	out, err := GetTasks(data, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetTasks: %v", err)
	}
	keys := out.Keys()
	if len(keys) != 3 {
		t.Fatalf("expected 3 expansions, got %d", len(keys))
	}
	for i, k := range keys {
		got := out.GetEnvelope(k).Task.(*AppTask).App
		want := []string{"app-0", "app-1", "app-2"}[i]
		if got != want {
			t.Errorf("expansion %d app = %q, want %q", i, got, want)
		}
	}
}

func TestLoopEnvelopesShareWhen(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: deploy
      loop: [a, b, c]
      when: 'item != "b"'
      dokku_app:
        app: "{{ .item }}"
`)
	out, err := GetTasks(data, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetTasks: %v", err)
	}
	for _, k := range out.Keys() {
		env := out.GetEnvelope(k)
		if env.When != `item != "b"` {
			t.Errorf("expansion %q lost when, got %q", k, env.When)
		}
		if env.WhenProgram() == nil {
			t.Errorf("expansion %q missing pre-compiled when program", k)
		}
	}
}

func TestLoopVarOutsideLoopRejectedAtParse(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: deploy
      dokku_app:
        app: "{{ .item }}"
`)
	_, err := GetTasks(data, map[string]interface{}{})
	if err == nil {
		t.Fatal("expected error for .item outside loop")
	}
	if !strings.Contains(err.Error(), ".item is only available inside a loop body") {
		t.Errorf("got: %v", err)
	}
}

// TestLoopOnGroupExpandsEntireGroup pins #211: a `loop:` on a group
// duplicates the whole group N times so each iteration's children
// share that iteration's `.item` / `.index`.
func TestLoopOnGroupExpandsEntireGroup(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: deploy each
      loop: [api, worker, web]
      block:
        - dokku_app:
            app: "deploy-{{ .item }}"
`)
	out, err := GetTasks(data, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetTasks: %v", err)
	}
	keys := out.Keys()
	if len(keys) != 3 {
		t.Fatalf("expected 3 group expansions, got %d (%v)", len(keys), keys)
	}
	wantApps := []string{"deploy-api", "deploy-worker", "deploy-web"}
	for i, k := range keys {
		env := out.GetEnvelope(k)
		if !env.IsGroup() {
			t.Errorf("expansion %d %q must be a group", i, k)
			continue
		}
		if !env.IsLoopExpansion {
			t.Errorf("expansion %d %q must carry IsLoopExpansion", i, k)
		}
		if len(env.Block) != 1 {
			t.Fatalf("expansion %d block: want 1 child, got %d", i, len(env.Block))
		}
		appTask, ok := env.Block[0].Task.(*AppTask)
		if !ok {
			t.Fatalf("expansion %d block[0] task type %T, want *AppTask", i, env.Block[0].Task)
		}
		if appTask.App != wantApps[i] {
			t.Errorf("expansion %d block[0] app = %q, want %q", i, appTask.App, wantApps[i])
		}
	}
}
