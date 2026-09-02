package tasks

import (
	"fmt"
	"sort"
	"strings"
)

// ResourceSelector is one parsed `docket export --resource` address: which
// task type to export, and which of that type's identity keys the address
// pinned. An empty Keys map means "every resource of this type".
type ResourceSelector struct {
	// Address is the address as the user wrote it, for diagnostics.
	Address string

	// TypeKey is the registered task name, e.g. "dokku_config".
	TypeKey string

	// Keys are the identity key values the address pinned, by yaml name.
	Keys map[string]string
}

// ParseResourceSelectors parses and validates every --resource value.
//
// Validation happens here, before the export contacts the server, so a typo in
// an address fails immediately rather than after an SSH round trip. Three
// things are checked: the type resolves to a registered task, that task is
// actually reachable from ExportRecipe, and every key the address names is one
// the task declares.
func ParseResourceSelectors(addresses []string) ([]ResourceSelector, error) {
	inApp, inGlobal := exportOrderSets()

	out := make([]ResourceSelector, 0, len(addresses))
	for _, address := range addresses {
		typeKey, keys, err := ParseIdentityAddress(address)
		if err != nil {
			return nil, err
		}

		task, ok := RegisteredTasks[typeKey]
		if !ok {
			msg := fmt.Sprintf("unknown task type %q in resource address %q", typeKey, address)
			if near := nearestEnvelopeOrTaskKey(typeKey); near != "" {
				msg += fmt.Sprintf(" (did you mean %q?)", near)
			}
			return nil, fmt.Errorf("%s", msg)
		}

		if !inApp[typeKey] && !inGlobal[typeKey] {
			reason := ""
			if support, declared := TaskExportSupport(task); declared && support.Caveat != "" {
				reason = ": " + support.Caveat
			}
			return nil, fmt.Errorf("task type %q cannot be exported%s", typeKey, reason)
		}

		declared := map[string]bool{}
		for _, key := range TaskIdentity(task) {
			declared[key.YAMLName] = true
		}
		for name := range keys {
			if !declared[name] {
				return nil, fmt.Errorf("resource address %q: %q is not an identity key of %s (keys: %s)",
					address, name, typeKey, strings.Join(sortedIdentityKeyNames(task), ", "))
			}
		}

		out = append(out, ResourceSelector{Address: address, TypeKey: typeKey, Keys: keys})
	}
	return out, nil
}

// exportOrderSets returns the app and global export order lists as lookup
// sets.
func exportOrderSets() (app, global map[string]bool) {
	app = make(map[string]bool, len(appExportOrder))
	global = make(map[string]bool, len(globalExportOrder))
	for _, key := range appExportOrder {
		app[key] = true
	}
	for _, key := range globalExportOrder {
		global[key] = true
	}
	return app, global
}

// resourceFilter narrows an export run to the selected addresses. A nil
// filter selects everything, which is what an unrestricted export uses.
type resourceFilter struct {
	selectors []ResourceSelector
	byType    map[string][]int
	matched   []bool
}

// newResourceFilter builds a filter from the parsed selectors, or nil when
// there are none.
func newResourceFilter(selectors []ResourceSelector) *resourceFilter {
	if len(selectors) == 0 {
		return nil
	}
	f := &resourceFilter{
		selectors: selectors,
		byType:    make(map[string][]int, len(selectors)),
		matched:   make([]bool, len(selectors)),
	}
	for i, sel := range selectors {
		f.byType[sel.TypeKey] = append(f.byType[sel.TypeKey], i)
	}
	return f
}

// wantsType reports whether any selector asks for this task type, so the
// export can skip running an exporter nothing selected.
func (f *resourceFilter) wantsType(typeKey string) bool {
	if f == nil {
		return true
	}
	return len(f.byType[typeKey]) > 0
}

// keepBody reports whether an exported task body matches a selector, and
// records the match so an address that resolved to nothing can be reported.
//
// The body is matched, not the request: an exporter returns one body per
// resource it found, each already carrying the identity field values the
// server reported, so `IdentityMatches` answers "is this the resource the
// address named" directly.
//
// Every exporter emits task struct values today. The error return covers a
// future one that does not: dropping such a body silently would look exactly
// like "the server does not have this resource", so the caller turns it into a
// warning instead.
func (f *resourceFilter) keepBody(typeKey string, body interface{}) (bool, error) {
	if f == nil {
		return true, nil
	}
	task, ok := body.(Task)
	if !ok {
		return false, fmt.Errorf("exported body of type %T does not implement Task, so --resource cannot match it", body)
	}
	keep := false
	for _, i := range f.byType[typeKey] {
		if IdentityMatches(task, f.selectors[i].Keys) {
			f.matched[i] = true
			keep = true
		}
	}
	return keep, nil
}

// wantsGlobalScope reports whether the leading global play should run at all.
// A selector pinned to an app never wants it, even when its task type also
// has a global form.
func (f *resourceFilter) wantsGlobalScope(inGlobal map[string]bool) bool {
	if f == nil {
		return true
	}
	for _, sel := range f.selectors {
		if !inGlobal[sel.TypeKey] {
			continue
		}
		if _, pinned := sel.Keys["app"]; pinned {
			continue
		}
		return true
	}
	return false
}

// hasGlobalAddress reports whether any selector names a not-app-scoped type
// without pinning an app. It is the narrower question wantsGlobalScope answers
// for an unrestricted run: not "should globals be emitted" but "did the caller
// ask for one by name", which is what lets an explicit address keep the global
// play that --app would otherwise skip (#518).
func (f *resourceFilter) hasGlobalAddress(inGlobal map[string]bool) bool {
	if f == nil {
		return false
	}
	for _, sel := range f.selectors {
		if !inGlobal[sel.TypeKey] {
			continue
		}
		if _, pinned := sel.Keys["app"]; pinned {
			continue
		}
		return true
	}
	return false
}

// appNames returns the app names the selectors pin, and whether that set is a
// restriction at all. An app-scoped selector that names no app - `dokku_config`
// on its own - means every app, so no restriction applies.
func (f *resourceFilter) appNames(inApp map[string]bool) ([]string, bool) {
	if f == nil {
		return nil, false
	}
	seen := map[string]bool{}
	for _, sel := range f.selectors {
		if !inApp[sel.TypeKey] {
			continue
		}
		app, pinned := sel.Keys["app"]
		if !pinned || app == "" {
			// One unpinned app-scoped selector widens the run to every app.
			if _, global := sel.Keys["global"]; global {
				continue
			}
			return nil, false
		}
		seen[app] = true
	}
	if len(seen) == 0 {
		return nil, true
	}
	out := make([]string, 0, len(seen))
	for app := range seen {
		out = append(out, app)
	}
	sort.Strings(out)
	return out, true
}

// unmatchedAddresses returns the addresses that selected nothing, in the
// order they were given.
func (f *resourceFilter) unmatchedAddresses() []string {
	if f == nil {
		return nil
	}
	var out []string
	for i, sel := range f.selectors {
		if !f.matched[i] {
			out = append(out, sel.Address)
		}
	}
	return out
}
