package tasks

// OrderedStringEnvelopeMap is an insertion-ordered map of TaskEnvelopes
// keyed by string. Keys() returns names in the order they were Set, which
// is the order GetTasks parsed them out of the recipe. Loop expansions
// each contribute their own key (`<name> (item=<value>)`) so the map's
// uniqueness invariant holds across expanded envelopes.
//
// Get(name) returns the underlying Task so it stays ergonomic for tests
// that only care about the decoded task body. Envelope-aware callers
// (apply / plan, sensitive, validate) use GetEnvelope to access the
// cross-cutting fields (tags / when / loop / register).
type OrderedStringEnvelopeMap struct {
	data map[string]*TaskEnvelope
	keys []string
}

// Get returns the Task registered under k, or nil if absent. This is a
// convenience wrapper for callers that only need the task body.
func (o *OrderedStringEnvelopeMap) Get(k string) Task {
	env := o.GetEnvelope(k)
	if env == nil {
		return nil
	}
	return env.Task
}

// GetEnvelope returns the envelope registered under k, or nil if absent.
func (o *OrderedStringEnvelopeMap) GetEnvelope(k string) *TaskEnvelope {
	if len(o.data) == 0 {
		return nil
	}
	return o.data[k]
}

// Set registers env under k. Setting a key that already exists overwrites
// the value but does not record a duplicate key in the order list. The
// loader avoids overwrites by suffixing loop-expansion names.
func (o *OrderedStringEnvelopeMap) Set(k string, env *TaskEnvelope) {
	if o.data == nil {
		o.data = make(map[string]*TaskEnvelope)
	}
	if _, exists := o.data[k]; !exists {
		o.keys = append(o.keys, k)
	}
	o.data[k] = env
}

// Keys returns the registered keys in insertion order.
func (o *OrderedStringEnvelopeMap) Keys() []string {
	return o.keys
}
