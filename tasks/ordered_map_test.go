package tasks

import (
	"context"
	"fmt"
	"testing"
)

type mockTask struct {
	name  string
	state State
}

func (m mockTask) Doc() string              { return "" }
func (m mockTask) Examples() ([]Doc, error) { return nil, nil }
func (m mockTask) Plan(ctx context.Context) PlanResult {
	return PlanResult{InSync: true, Status: PlanStatusOK}
}
func (m mockTask) Execute(ctx context.Context) TaskOutputState {
	return TaskOutputState{State: m.state}
}

func envelopeOf(name string, state State) *TaskEnvelope {
	return &TaskEnvelope{Name: name, Task: mockTask{name: name, state: state}}
}

func TestOrderedStringEnvelopeMapSetAndGet(t *testing.T) {
	m := OrderedStringEnvelopeMap{}
	m.Set("key1", envelopeOf("test", StatePresent))

	got := m.Get("key1")
	if got == nil {
		t.Fatal("Get returned nil for existing key")
	}
	if got.Execute(testCtx()).State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", got.Execute(testCtx()).State)
	}
	if env := m.GetEnvelope("key1"); env == nil {
		t.Fatal("GetEnvelope returned nil for existing key")
	}
}

func TestOrderedStringEnvelopeMapGetMissing(t *testing.T) {
	m := OrderedStringEnvelopeMap{}

	if got := m.Get("nonexistent"); got != nil {
		t.Error("Get returned non-nil for nonexistent key on empty map")
	}

	m.Set("key1", envelopeOf("test", StatePresent))
	if got := m.Get("nonexistent"); got != nil {
		t.Error("Get returned non-nil for nonexistent key")
	}
}

func TestOrderedStringEnvelopeMapKeysPreservesOrder(t *testing.T) {
	m := OrderedStringEnvelopeMap{}

	m.Set("third", envelopeOf("3", StatePresent))
	m.Set("first", envelopeOf("1", StatePresent))
	m.Set("second", envelopeOf("2", StatePresent))

	keys := m.Keys()
	expected := []string{"third", "first", "second"}
	if len(keys) != len(expected) {
		t.Fatalf("expected %d keys, got %d", len(expected), len(keys))
	}
	for i, key := range keys {
		if key != expected[i] {
			t.Errorf("key[%d] = %q, want %q", i, key, expected[i])
		}
	}
}

func TestOrderedStringEnvelopeMapEmptyKeys(t *testing.T) {
	m := OrderedStringEnvelopeMap{}
	keys := m.Keys()
	if keys != nil && len(keys) != 0 {
		t.Errorf("expected nil or empty keys, got %v", keys)
	}
}

// TestOrderedStringEnvelopeMapOverwriteValue covers the deduped-order
// invariant: Set under an existing key replaces the value but does not
// append a duplicate entry to Keys(). Loop expansion relies on this so
// `<name> (item=a)` and `<name> (item=b)` each appear exactly once.
func TestOrderedStringEnvelopeMapOverwriteValue(t *testing.T) {
	m := OrderedStringEnvelopeMap{}
	m.Set("key", envelopeOf("first", StatePresent))
	m.Set("key", envelopeOf("second", StateAbsent))

	got := m.Get("key")
	if got == nil {
		t.Fatal("Get returned nil for overwritten key")
	}
	if got.Execute(testCtx()).State != StateAbsent {
		t.Errorf("expected overwritten state 'absent', got '%s'", got.Execute(testCtx()).State)
	}
	if keys := m.Keys(); len(keys) != 1 {
		t.Fatalf("expected 1 key (deduped), got %d", len(keys))
	}
}

func TestOrderedStringEnvelopeMapLargeSet(t *testing.T) {
	m := OrderedStringEnvelopeMap{}
	for i := 0; i < 10; i++ {
		name := fmt.Sprintf("task-%d", i)
		m.Set(name, envelopeOf(name, StatePresent))
	}
	keys := m.Keys()
	if len(keys) != 10 {
		t.Fatalf("expected 10 keys, got %d", len(keys))
	}
	for i, key := range keys {
		expected := fmt.Sprintf("task-%d", i)
		if key != expected {
			t.Errorf("key[%d] = %q, want %q", i, key, expected)
		}
	}
}
