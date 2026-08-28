package subprocess

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
)

// maskPlaceholder is what every sensitive value is replaced with in user-facing
// output. Length is intentionally fixed at three asterisks so masked output
// reveals nothing about the original value (no length, prefix, or suffix).
const maskPlaceholder = "***"

var (
	sensitiveMu     sync.RWMutex
	sensitiveValues []string
)

// SetGlobalSensitive registers the set of literal string values that must be
// masked anywhere they appear in user-facing output. Pass nil or an empty
// slice to clear the registry. Empty entries in values are dropped (matching
// every empty substring would otherwise mask everything).
//
// Callers (typically commands/apply.go and commands/plan.go) collect this set
// from input values declared `sensitive: true` and from task struct fields
// tagged `sensitive:"true"` before any subprocess runs, then defer a clear.
func SetGlobalSensitive(values []string) {
	cleaned := cleanSensitive(values)

	sensitiveMu.Lock()
	defer sensitiveMu.Unlock()
	if len(cleaned) == 0 {
		sensitiveValues = nil
		return
	}
	sensitiveValues = cleaned
}

// AddGlobalSensitive appends values to the sensitive registry, keeping the
// entries already registered. Use it when a value that must be masked only
// becomes known after the registry was first populated - typically a secret
// read back from the server during Plan() (a drifted property's old value, or
// scheduler-k3s trigger metadata), which the pre-run collection in
// commands/apply.go and commands/plan.go cannot see. Values are de-duplicated
// against the existing set, empties are dropped, and the whole registry is
// re-sorted length-descending. A no-op when values contribute nothing new.
func AddGlobalSensitive(values ...string) {
	if len(values) == 0 {
		return
	}
	sensitiveMu.Lock()
	defer sensitiveMu.Unlock()
	merged := cleanSensitive(append(append([]string{}, sensitiveValues...), values...))
	if len(merged) == 0 {
		sensitiveValues = nil
		return
	}
	sensitiveValues = merged
}

// cleanSensitive drops empty and duplicate entries and returns the values
// sorted by length descending so a longer secret is masked before any shorter
// secret that is a substring of it would be.
//
// A value carrying leading or trailing whitespace registers its trimmed
// spelling alongside the literal. Masking is literal substring replacement,
// but docket renders some user-facing text through strings.TrimSpace - a loop
// item in the `(item=<value>)` task-name suffix, most visibly - so without the
// trimmed spelling a padded secret prints in the clear wherever it was
// trimmed (#473). Registering both here rather than at each collection site
// keeps the two spellings in step no matter when a value joins the registry:
// a task-declared secret is added after the recipe has already parsed and
// named its loop expansions.
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
	for _, v := range values {
		add(v)
		add(strings.TrimSpace(v))
	}
	sort.SliceStable(cleaned, func(i, j int) bool {
		return len(cleaned[i]) > len(cleaned[j])
	})
	return cleaned
}

// GlobalSensitive returns a snapshot of the current sensitive value set.
func GlobalSensitive() []string {
	sensitiveMu.RLock()
	defer sensitiveMu.RUnlock()
	if len(sensitiveValues) == 0 {
		return nil
	}
	out := make([]string, len(sensitiveValues))
	copy(out, sensitiveValues)
	return out
}

// MaskString replaces every occurrence of any registered sensitive value in s
// with `***`. Returns s unchanged when the registry is empty.
func MaskString(s string) string {
	if s == "" {
		return s
	}
	sensitiveMu.RLock()
	values := sensitiveValues
	sensitiveMu.RUnlock()
	if len(values) == 0 {
		return s
	}
	for _, v := range values {
		if v == "" {
			continue
		}
		s = strings.ReplaceAll(s, v, maskPlaceholder)
	}
	return s
}

// MaskValue returns v with every registered sensitive value replaced by `***`
// in every string it contains, walking slices and maps recursively. Non-string
// scalars - numbers, booleans, nil - are returned unchanged: masking is
// substring replacement over text, and a value that carried a secret would
// have been rendered as a string before it got here.
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
// quote or a backslash is escaped in the serialised form, so MaskString would
// no longer match it there.
func MaskValue(v interface{}) interface{} {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return MaskString(t)
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, item := range t {
			out[i] = MaskValue(item)
		}
		return out
	case []string:
		out := make([]string, len(t))
		for i, item := range t {
			out[i] = MaskString(item)
		}
		return out
	case map[string]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, item := range t {
			out[MaskString(k)] = MaskValue(item)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(t))
		for k, item := range t {
			out[MaskString(k)] = MaskString(item)
		}
		return out
	}
	return maskReflectValue(v)
}

// maskReflectValue is the fallback for the container types the switch in
// MaskValue does not name - the typed slices and maps an expr-evaluated
// `loop:` can produce, and the map[interface{}]interface{} shape a
// hand-built value can carry. It mirrors the reflect normalisation
// tasks.resolveLoopList already applies on the way in. The copy is
// interface{}-shaped because the only consumer serialises it to JSON, where
// an object key is a string regardless. Anything that is not a slice, array,
// or map is returned unchanged: a non-string scalar cannot carry a secret.
func maskReflectValue(v interface{}) interface{} {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		return MaskString(rv.String())
	case reflect.Slice, reflect.Array:
		out := make([]interface{}, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = MaskValue(rv.Index(i).Interface())
		}
		return out
	case reflect.Map:
		out := make(map[string]interface{}, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[MaskString(fmt.Sprint(iter.Key().Interface()))] = MaskValue(iter.Value().Interface())
		}
		return out
	case reflect.Ptr, reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return MaskValue(rv.Elem().Interface())
	}
	return v
}
