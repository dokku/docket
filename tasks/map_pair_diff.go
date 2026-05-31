package tasks

import (
	"errors"
	"fmt"
	"sort"
)

// validateAppGlobalExclusive enforces the app/global mutual-exclusion that
// every dokku task taking either an app name or the --global flag shares:
// exactly one of the two must be set.
func validateAppGlobalExclusive(app string, global bool) error {
	if !global && app == "" {
		return errors.New("app is required when global is false")
	}
	if global && app != "" {
		return fmt.Errorf("'app' must not be set when 'global' is set to true")
	}
	return nil
}

// driftedKeys returns the sorted list of keys in desired whose value differs
// from current (or which are missing from current). allNew is true when every
// drifted key is brand new (not present in current at all), so a caller can
// pick PlanStatusCreate over PlanStatusModify.
func driftedKeys(desired, current map[string]string) (drifted []string, allNew bool) {
	allNew = true
	for k, v := range desired {
		cur, ok := current[k]
		if ok && cur == v {
			continue
		}
		drifted = append(drifted, k)
		if ok {
			allNew = false
		}
	}
	sort.Strings(drifted)
	return drifted, allNew
}

// intersectingKeys returns the sorted list of keys in target that also exist
// in current. Used by absent-state planners to skip work for keys dokku
// already lacks.
func intersectingKeys(target map[string]string, current map[string]string) []string {
	out := []string{}
	for k := range target {
		if _, ok := current[k]; ok {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// formatSetMutations renders the user-facing "set k=v (new|was \"old\")"
// mutation lines a planner emits when keys drift. The drifted slice is
// expected pre-sorted (driftedKeys does this).
func formatSetMutations(drifted []string, desired, current map[string]string) []string {
	out := make([]string, 0, len(drifted))
	for _, k := range drifted {
		if cur, ok := current[k]; ok {
			out = append(out, fmt.Sprintf("set %s=%s (was %q)", k, desired[k], cur))
		} else {
			out = append(out, fmt.Sprintf("set %s=%s (new)", k, desired[k]))
		}
	}
	return out
}

// formatClearMutations renders the user-facing "unset k (was \"old\")"
// mutation lines an absent-state planner emits. The toClear slice is expected
// pre-sorted (intersectingKeys does this).
func formatClearMutations(toClear []string, current map[string]string) []string {
	out := make([]string, 0, len(toClear))
	for _, k := range toClear {
		out = append(out, fmt.Sprintf("unset %s (was %q)", k, current[k]))
	}
	return out
}
