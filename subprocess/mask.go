package subprocess

import (
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
func cleanSensitive(values []string) []string {
	cleaned := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		cleaned = append(cleaned, v)
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
