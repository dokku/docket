package subprocess

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// MaskPlaceholder is what every sensitive value is replaced with in user-facing
// output. Length is intentionally fixed at three asterisks so masked output
// reveals nothing about the original value (no length, prefix, or suffix).
//
// Exported because one caller has to render the placeholder without running
// text through MaskString: commands/apply_args.go rewrites the usage-string
// default of a `sensitive: true` input's flag, which `--help` prints before any
// value has been registered here.
const MaskPlaceholder = "***"

// Masker holds the set of literal string values that must not appear in
// user-facing output, and replaces them with MaskPlaceholder wherever they
// would.
//
// One Masker belongs to one run. That is the whole point of the type: the
// registry it replaces was process-wide, and the API that populated it
// *replaced* the set rather than adding to it, so two runs sharing a process
// had the first one's teardown blank the second one's secrets while it was
// still writing output. A Masker is never cleared - it goes out of scope with
// the run that owns it - so there is no teardown to get wrong.
//
// A nil *Masker masks nothing and is safe to use. That is deliberate: a caller
// that registered no secrets has nothing to hide, so code paths reached with
// or without a masker need no branch of their own.
type Masker struct {
	mu     sync.RWMutex
	values []string
}

// NewMasker returns a Masker holding values, in the spellings cleanSensitive
// derives. A Masker built with no values is still usable; more can be added.
func NewMasker(values ...string) *Masker {
	m := &Masker{}
	m.Add(values...)
	return m
}

// Add registers more values to mask, keeping the ones already there.
//
// There is no Set. Replacing the set is what made the old registry fail open,
// and nothing needs it: a run's secrets only ever accumulate, as tasks read
// values back off the server that the pre-run collection could not see - a
// drifted property's old value, scheduler-k3s trigger metadata.
func (m *Masker) Add(values ...string) {
	if m == nil || len(values) == 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.values = cleanSensitive(append(m.values, values...))
}

// Values returns a snapshot of the registered spellings, longest first.
func (m *Masker) Values() []string {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if len(m.values) == 0 {
		return nil
	}
	out := make([]string, len(m.values))
	copy(out, m.values)
	return out
}

// String replaces every occurrence of a registered value in s with `***`.
func (m *Masker) String(s string) string {
	if m == nil || s == "" {
		return s
	}
	m.mu.RLock()
	values := m.values
	m.mu.RUnlock()
	for _, v := range values {
		if v == "" {
			continue
		}
		s = strings.ReplaceAll(s, v, MaskPlaceholder)
	}
	return s
}

// Value returns v with every registered value replaced by `***` in every
// string it contains, walking slices and maps recursively. Non-string scalars
// - numbers, booleans, nil - are returned unchanged: masking is substring
// replacement over text, and a value that carried a secret would have been
// rendered as a string before it got here.
//
// Map keys are masked as well as map values. A recipe is rendered as one
// template before it is parsed, so a key is interpolated user data exactly as
// a value is. Two keys that differ only inside a secret therefore collapse
// into a single `***` entry - the same "two distinct values become
// indistinguishable" trade-off masking already makes for task names.
//
// It exists for the values that reach a stream as interface{} - today the
// `loop_item` field of `--list-tasks --json`, which carries whatever the
// recipe's `loop:` resolved to: a scalar, a list, or a mapping. Masking the
// value rather than the marshalled line is deliberate: a secret containing a
// quote or a backslash is escaped in the serialised form, and String cannot be
// relied on to match it there.
func (m *Masker) Value(v interface{}) interface{} {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return m.String(t)
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, item := range t {
			out[i] = m.Value(item)
		}
		return out
	case []string:
		out := make([]string, len(t))
		for i, item := range t {
			out[i] = m.String(item)
		}
		return out
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, item := range t {
			out[m.String(k)] = m.Value(item)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(t))
		for k, item := range t {
			out[m.String(k)] = m.String(item)
		}
		return out
	}
	return m.reflectValue(v)
}

// reflectValue is the fallback for the container types the switch in Value
// does not name - the typed slices and maps an expr-evaluated `loop:` can
// produce, and the map[interface{}]interface{} shape a hand-built value can
// carry. It mirrors the reflect normalisation tasks.resolveLoopList already
// applies on the way in. The copy is interface{}-shaped because the only
// consumer serialises it to JSON, where an object key is a string regardless.
// Anything that is not a slice, array, or map is returned unchanged: a
// non-string scalar cannot carry a secret.
func (m *Masker) reflectValue(v interface{}) interface{} {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		return m.String(rv.String())
	case reflect.Slice, reflect.Array:
		out := make([]interface{}, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = m.Value(rv.Index(i).Interface())
		}
		return out
	case reflect.Map:
		out := make(map[string]interface{}, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[m.String(fmt.Sprint(iter.Key().Interface()))] = m.Value(iter.Value().Interface())
		}
		return out
	case reflect.Ptr, reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return m.Value(rv.Elem().Interface())
	}
	return v
}

// maskerKey is the context key a run's Masker travels under.
type maskerKey struct{}

// ContextWithMasker returns a copy of ctx carrying m, so the layers that mask
// text but have no other reason to know about a run - the subprocess
// transports, a task registering a secret it just read back - can reach it.
func ContextWithMasker(ctx context.Context, m *Masker) context.Context {
	return context.WithValue(ctx, maskerKey{}, m)
}

// MaskerFromContext returns the Masker ctx carries, or nil when it carries
// none. A nil Masker masks nothing, so callers need not check.
func MaskerFromContext(ctx context.Context) *Masker {
	if ctx == nil {
		return nil
	}
	m, _ := ctx.Value(maskerKey{}).(*Masker)
	return m
}

// cleanSensitive drops empty and duplicate entries and returns the values
// sorted by length descending so a longer secret is masked before any shorter
// secret that is a substring of it would be.
//
// Each value registers every spelling docket can print it in, not just the
// literal it was given. Masking is literal substring replacement, and some of
// the text docket builds transforms a value on the way in, so a transformed
// value would otherwise reach the output in the clear. Two transforms apply:
//
//   - strings.TrimSpace, which renders a loop item in the `(item=<value>)`
//     task-name suffix, so a value carrying leading or trailing whitespace
//     registers its trimmed spelling as well (#473).
//   - Go quoting, which tasks.quoteIdentityValue applies to an identity key
//     value in a generated resource address - `dokku_stub[key="quo\"ted"]` -
//     so a value whose escaped form differs from its literal registers that
//     escaped form as well (#475).
//
// Deriving the spellings here rather than at each site that builds text keeps
// them in step no matter when a value joins the registry: a task-declared
// secret is added after the recipe has already parsed and named its tasks.
func cleanSensitive(values []string) []string {
	cleaned := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	add := func(v string) {
		if v == "" {
			return
		}
		if _, ok := seen[v]; ok {
			return
		}
		seen[v] = struct{}{}
		cleaned = append(cleaned, v)
	}
	addSpellings := func(v string) {
		add(v)
		add(escapedSpelling(v))
	}
	for _, v := range values {
		addSpellings(v)
		addSpellings(strings.TrimSpace(v))
	}
	sort.SliceStable(cleaned, func(i, j int) bool {
		return len(cleaned[i]) > len(cleaned[j])
	})
	return cleaned
}

// escapedSpelling returns v escaped the way Go quoting escapes it, without the
// surrounding quotes. That escaped body is the spelling a resource address
// carries for a key value holding a comma, a bracket, or a double quote, and
// the spelling any `%q` rendering of the value carries. Returns v unchanged
// when quoting escapes nothing, which is the common case; the caller
// deduplicates the repeat.
//
// The escaping is reimplemented here rather than shared with
// tasks.quoteIdentityValue because tasks imports subprocess, not the other way
// round. TestIdentityAddressMasksQuotedSensitiveValue in package tasks - which
// can see both - is what fails if the two ever drift apart.
func escapedSpelling(v string) string {
	quoted := strconv.Quote(v)
	return quoted[1 : len(quoted)-1]
}
