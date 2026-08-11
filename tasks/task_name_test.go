package tasks

import (
	"reflect"
	"testing"
)

// playTaskNames loads a single-play recipe and returns its task names in
// source order.
func playTaskNames(t *testing.T, recipe string) []string {
	t.Helper()
	envelopes, err := GetTasks([]byte(recipe), map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetTasks errored: %v", err)
	}
	return envelopes.Keys()
}

// TestGeneratedNamesAreStableAcrossRuns is the regression the issue is about.
// Before #427 an unnamed task's name carried eight random bytes, so two runs
// of one recipe emitted different `name` values for the same task and nothing
// consuming the --json stream could line them up.
func TestGeneratedNamesAreStableAcrossRuns(t *testing.T) {
	recipe := `---
- tasks:
    - dokku_app: { app: api }
    - dokku_config: { app: api, config: { A: "1" } }
    - name: hand written
      dokku_domains: { app: api, domains: [api.example.com] }
    - block:
        - dokku_app_lock: { app: api }
`
	first := playTaskNames(t, recipe)
	second := playTaskNames(t, recipe)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("names differ between runs:\n first = %v\nsecond = %v", first, second)
	}

	want := []string{
		"dokku_app[app=api]",
		"dokku_config[app=api]",
		"hand written",
		"group #4",
	}
	if !reflect.DeepEqual(first, want) {
		t.Errorf("names = %v, want %v", first, want)
	}
}

// TestGeneratedNamesDisambiguateCollisions covers two tasks addressing one
// resource, which is a normal thing for a recipe to do - setting some config
// keys and unsetting others - and which the old random suffix hid.
func TestGeneratedNamesDisambiguateCollisions(t *testing.T) {
	names := playTaskNames(t, `---
- tasks:
    - dokku_config: { app: api, config: { A: "1" } }
    - dokku_config: { app: api, state: absent, config: { B: "" } }
    - dokku_config: { app: api, config: { C: "3" } }
`)
	want := []string{
		"dokku_config[app=api]",
		"dokku_config[app=api] #2",
		"dokku_config[app=api] #3",
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
}

// TestUserNameWinsOverGeneratedCollision asserts the two-phase rename: a name
// the recipe author wrote is reserved before any generated name is assigned,
// so it keeps its name whichever order the two appear in.
func TestUserNameWinsOverGeneratedCollision(t *testing.T) {
	for _, tt := range []struct {
		name   string
		recipe string
	}{
		{
			name: "user name second",
			recipe: `---
- tasks:
    - dokku_app: { app: api }
    - name: dokku_app[app=api]
      dokku_app_lock: { app: api }
`,
		},
		{
			name: "user name first",
			recipe: `---
- tasks:
    - name: dokku_app[app=api]
      dokku_app_lock: { app: api }
    - dokku_app: { app: api }
`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			names := playTaskNames(t, tt.recipe)
			var lock, generated string
			for _, n := range names {
				if n == "dokku_app[app=api]" {
					lock = n
				}
				if n == "dokku_app[app=api] #2" {
					generated = n
				}
			}
			if lock == "" {
				t.Errorf("the hand-written name was renamed; got %v", names)
			}
			if generated == "" {
				t.Errorf("the generated name was not disambiguated; got %v", names)
			}
		})
	}
}

// TestGroupNamesArePathBased covers the one envelope kind with no resource to
// address. Group clauses restart their child indexes at 1, so a name derived
// from the index alone would collide across nesting levels.
func TestGroupNamesArePathBased(t *testing.T) {
	plays, err := GetPlays([]byte(`---
- tasks:
    - block:
        - dokku_app: { app: api }
        - block:
            - dokku_app_lock: { app: api }
`), map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("GetPlays errored: %v", err)
	}

	outer := plays[0].Tasks.GetEnvelope("group #1")
	if outer == nil {
		t.Fatalf("expected a top-level group named 'group #1', got %v", plays[0].Tasks.Keys())
	}
	if len(outer.Block) != 2 {
		t.Fatalf("expected 2 block children, got %d", len(outer.Block))
	}
	if got := outer.Block[0].Name; got != "dokku_app[app=api]" {
		t.Errorf("leaf child name = %q, want the resource address", got)
	}
	if got := outer.Block[1].Name; got != "group #1.block[2]" {
		t.Errorf("nested group name = %q, want 'group #1.block[2]'", got)
	}
}

// TestIdenticalLeavesInDifferentBlocksAreDisambiguated is the case group
// children used to escape entirely: they never enter play.Tasks, so nothing
// checked them for collisions, and --start-at-task would silently resolve to
// whichever came first.
func TestIdenticalLeavesInDifferentBlocksAreDisambiguated(t *testing.T) {
	plays, err := GetPlays([]byte(`---
- tasks:
    - block:
        - dokku_app: { app: api }
    - block:
        - dokku_app: { app: api }
`), map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("GetPlays errored: %v", err)
	}

	names := CollectEnvelopeNames([]*TaskEnvelope{
		plays[0].Tasks.GetEnvelope("group #1"),
		plays[0].Tasks.GetEnvelope("group #2"),
	})
	want := []string{"group #1", "dokku_app[app=api]", "group #2", "dokku_app[app=api] #2"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
}

// TestUnnamedLoopNamesIterationsByAddress covers the common loop shape, where
// the item feeds an identity field. Each iteration addresses a different
// resource, so each gets its own address - which is both more informative than
// `(item=…)` and the only form --start-at-task can resolve.
func TestUnnamedLoopNamesIterationsByAddress(t *testing.T) {
	names := playTaskNames(t, `---
- tasks:
    - loop: [api, web, worker]
      dokku_app: { app: "{{ .item }}" }
`)
	want := []string{
		"dokku_app[app=api]",
		"dokku_app[app=web]",
		"dokku_app[app=worker]",
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
}

// TestUnnamedLoopFallsBackWholesale covers a loop whose iterations all address
// the same resource - here one app's config, one key per iteration. The
// decision is made once for the whole loop, so the listing never mixes
// addresses with `(item=…)` suffixes.
func TestUnnamedLoopFallsBackWholesale(t *testing.T) {
	names := playTaskNames(t, `---
- tasks:
    - loop: [A, B]
      dokku_config:
        app: api
        config: { "{{ .item }}": "1" }
`)
	want := []string{
		"dokku_config (item=A)",
		"dokku_config (item=B)",
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
}

// TestNamedLoopKeepsItemSuffix asserts the unchanged half: a loop with a
// `name:` still reads `<name> (item=<value>)`, because the recipe author
// already said what to call it.
func TestNamedLoopKeepsItemSuffix(t *testing.T) {
	names := playTaskNames(t, `---
- tasks:
    - name: create apps
      loop: [api, web]
      dokku_app: { app: "{{ .item }}" }
`)
	want := []string{"create apps (item=api)", "create apps (item=web)"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
}

// TestParseErrorsQuoteTheEntryLabel asserts that a loader diagnostic names the
// entry's position in the file rather than its envelope name. The old form
// rendered the random auto-name into the message - `task #1 "task #1
// 3F2A9C1E4B7D0A55"` - and, now that naming happens after the body decodes,
// there would be no name to quote at all.
func TestParseErrorsQuoteTheEntryLabel(t *testing.T) {
	_, err := GetTasks([]byte(`---
- tasks:
    - when: "))"
      dokku_app: { app: api }
`), map[string]interface{}{})
	if err == nil {
		t.Fatal("expected a when compile error")
	}
	want := `task parse error: task #1: when compile error`
	if got := err.Error(); len(got) < len(want) || got[:len(want)] != want {
		t.Errorf("error = %q, want it to start with %q", got, want)
	}
}

// TestDisambiguateNames covers the pure rename helper the loader and the
// validator share, including the ordinal skipping a name already reserved.
func TestDisambiguateNames(t *testing.T) {
	names := []string{"a", "a", "a #2", "b"}
	generated := []bool{true, true, false, true}
	got := disambiguateNames(names, generated)
	want := []string{"a", "a #3", "a #2", "b"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("disambiguateNames() = %v, want %v", got, want)
	}
}
