package tasks

import (
	"reflect"
	"testing"
)

func TestTaskEnvelopeHasTag(t *testing.T) {
	env := &TaskEnvelope{Tags: []string{"api", "web"}}
	if !env.HasTag("api") {
		t.Error("expected HasTag(api) to be true")
	}
	if env.HasTag("worker") {
		t.Error("expected HasTag(worker) to be false")
	}
}

func TestTaskEnvelopeIntersectsTags(t *testing.T) {
	env := &TaskEnvelope{Tags: []string{"api", "web"}}
	if !env.IntersectsTags([]string{"web", "worker"}) {
		t.Error("expected intersect with [web worker]")
	}
	if env.IntersectsTags([]string{"worker"}) {
		t.Error("expected no intersect with [worker]")
	}
	if env.IntersectsTags(nil) {
		t.Error("expected no intersect with nil")
	}
}

func TestFilterByTagsNoFlagsReturnsAllKeys(t *testing.T) {
	m := buildEnvelopeMap(map[string][]string{
		"a": {"foo"},
		"b": {"bar"},
		"c": nil,
	})
	got := FilterByTags(m, nil, nil)
	if len(got) != 3 {
		t.Errorf("expected all 3 keys, got %v", got)
	}
}

func TestFilterByTagsIncludeIntersection(t *testing.T) {
	m := buildEnvelopeMap(map[string][]string{
		"a": {"foo"},
		"b": {"bar"},
		"c": nil,
	})
	got := FilterByTags(m, []string{"foo"}, nil)
	if !reflect.DeepEqual(got, []string{"a"}) {
		t.Errorf("got %v, want [a]", got)
	}
}

func TestFilterByTagsIncludeExcludesUntagged(t *testing.T) {
	m := buildEnvelopeMap(map[string][]string{
		"tagged":   {"foo"},
		"untagged": nil,
	})
	got := FilterByTags(m, []string{"foo"}, nil)
	if !reflect.DeepEqual(got, []string{"tagged"}) {
		t.Errorf("got %v, want [tagged]", got)
	}
}

func TestFilterByTagsSkipDropsMatching(t *testing.T) {
	m := buildEnvelopeMap(map[string][]string{
		"a": {"foo"},
		"b": {"bar"},
		"c": nil,
	})
	got := FilterByTags(m, nil, []string{"foo"})
	if !reflect.DeepEqual(got, []string{"b", "c"}) {
		t.Errorf("got %v, want [b c]", got)
	}
}

func TestFilterByTagsSkipKeepsUntagged(t *testing.T) {
	m := buildEnvelopeMap(map[string][]string{
		"tagged":   {"foo"},
		"untagged": nil,
	})
	got := FilterByTags(m, nil, []string{"foo"})
	if !reflect.DeepEqual(got, []string{"untagged"}) {
		t.Errorf("got %v, want [untagged]", got)
	}
}

func TestFilterByTagsCombinedNarrowsThenDrops(t *testing.T) {
	m := buildEnvelopeMap(map[string][]string{
		"a": {"foo", "skip"},
		"b": {"foo"},
		"c": {"bar"},
	})
	got := FilterByTags(m, []string{"foo"}, []string{"skip"})
	if !reflect.DeepEqual(got, []string{"b"}) {
		t.Errorf("got %v, want [b]", got)
	}
}

// TestEnvelopeContainsName covers the helper used by --start-at-task
// gating: a top-level name match, a recursive match through a block /
// rescue / always child, and a miss.
func TestEnvelopeContainsName(t *testing.T) {
	leaf := &TaskEnvelope{Name: "leaf"}
	if !EnvelopeContainsName(leaf, "leaf") {
		t.Errorf("leaf should self-match")
	}
	if EnvelopeContainsName(leaf, "other") {
		t.Errorf("leaf should not match unrelated name")
	}

	group := &TaskEnvelope{
		Name: "outer",
		Block: []*TaskEnvelope{
			{Name: "inner-a"},
			{Name: "inner-b", Rescue: []*TaskEnvelope{{Name: "deep"}}},
		},
		Always: []*TaskEnvelope{{Name: "always-a"}},
	}
	for _, want := range []string{"outer", "inner-a", "inner-b", "deep", "always-a"} {
		if !EnvelopeContainsName(group, want) {
			t.Errorf("group should contain %q", want)
		}
	}
	if EnvelopeContainsName(group, "missing") {
		t.Errorf("group must not contain unrelated name")
	}
	if EnvelopeContainsName(nil, "anything") {
		t.Errorf("nil env should never match")
	}
}

// TestCollectEnvelopeNames covers the helper that flattens a slice of
// envelopes into the source-ordered, de-duplicated name list used in
// the --start-at-task error hint.
func TestCollectEnvelopeNames(t *testing.T) {
	envs := []*TaskEnvelope{
		{Name: "a"},
		{Name: "b", Block: []*TaskEnvelope{{Name: "b-child"}, {Name: "a"}}},
		{Name: "c"},
	}
	got := CollectEnvelopeNames(envs)
	want := []string{"a", "b", "b-child", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CollectEnvelopeNames = %v, want %v", got, want)
	}
}

func buildEnvelopeMap(spec map[string][]string) OrderedStringEnvelopeMap {
	// spec maps key -> tag list. Insertion order follows the alphabetical
	// order of the keys so test assertions remain stable.
	keys := make([]string, 0, len(spec))
	for k := range spec {
		keys = append(keys, k)
	}
	// Bubble sort - good enough for handful-of-entries fixtures.
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}

	m := OrderedStringEnvelopeMap{}
	for _, k := range keys {
		m.Set(k, &TaskEnvelope{Name: k, Tags: spec[k], Task: mockTask{name: k}})
	}
	return m
}
