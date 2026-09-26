package tasks

import (
	"fmt"
	"io"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// loopItemNameLimit caps the rendered `(item=<value>)` suffix so a long
// or complex item value does not produce an unwieldy task name.
const loopItemNameLimit = 40

// expandLoop produces one TaskEnvelope per iteration the envelope's loop
// resolves to. The base envelope already carries the resolved Loop value
// (literal list or expr source); bodyBytes is the task body re-marshalled
// from the parsed entry's body node. context is the file-level sigil
// context used to populate `.item` and `.index` during the second-pass
// render.
//
// The Loop value is resolved as follows:
//
//   - []interface{} or any reflect-able slice/array: used as-is.
//   - string: compiled and evaluated as an expr program against the
//     given expr context (file-level inputs); the result must be a list.
//   - anything else: returns an error.
//
// For each item, the body is sigil-rendered with `.item`/`.index` set,
// then decoded into a fresh registered task struct via decodeTaskBytes.
// The expanded envelope inherits Tags / When / Register from the base;
// LoopItem / LoopIndex carry the iteration value so the per-task `when:`
// evaluation can see them.
func expandLoop(base *TaskEnvelope, bodyBytes []byte, typeKey string, sigilContext map[string]interface{}, exprContext map[string]interface{}) ([]*TaskEnvelope, error) {
	items, err := resolveLoopList(base.Loop, exprContext)
	if err != nil {
		return nil, err
	}

	// Bodies decode first so an unnamed loop can name each iteration after the
	// resource that iteration addresses; the `.item` substitution has already
	// happened by then, which is what a name assigned before expansion could
	// not see.
	decoded := make([]Task, len(items))
	for i, item := range items {
		iterCtx := make(map[string]interface{}, safeCap(len(sigilContext), 2))
		for k, v := range sigilContext {
			iterCtx[k] = v
		}
		iterCtx["item"] = item
		iterCtx["index"] = i

		rendered, err := RenderTemplate(bodyBytes, iterCtx, "loop")
		if err != nil {
			return nil, fmt.Errorf("loop iteration %d: render error: %w", i, err)
		}
		renderedBytes, err := io.ReadAll(&rendered)
		if err != nil {
			return nil, fmt.Errorf("loop iteration %d: read error: %w", i, err)
		}

		task, err := decodeTaskBytes(typeKey, renderedBytes)
		if err != nil {
			return nil, fmt.Errorf("loop iteration %d: decode error: %w", i, err)
		}
		decoded[i] = task
	}

	names, generated := loopIterationNames(base.Name, typeKey, decoded, items)
	out := make([]*TaskEnvelope, 0, len(items))
	for i, item := range items {
		expanded := *base
		expanded.Task = decoded[i]
		expanded.Loop = nil
		expanded.LoopItem = item
		expanded.LoopIndex = i
		expanded.IsLoopExpansion = true
		expanded.Name = names[i]
		expanded.NameGenerated = generated

		out = append(out, &expanded)
	}
	return out, nil
}

// loopIterationNames derives one name per loop iteration and reports whether
// the names were generated rather than taken from a `name:` in the recipe.
//
// A named loop keeps the `<name> (item=<value>)` scheme unchanged. An unnamed
// one names each iteration after the resource it addresses, which is both
// more useful than `(item=…)` when the item feeds an identity field and the
// only form `--start-at-task` and `export --resource` can resolve.
//
// The decision is made once for the whole loop, never per iteration: when any
// two iterations address the same resource - a loop over config values for one
// app, say - every iteration falls back to the item-suffixed form. A loop that
// rendered some names as addresses and others as `(item=…)` would be harder to
// read and to predict than either form alone.
func loopIterationNames(baseName, typeKey string, decoded []Task, items []interface{}) ([]string, bool) {
	if baseName != "" {
		return loopExpansionNames(baseName, items), false
	}

	addresses := make([]string, len(decoded))
	seen := make(map[string]bool, len(decoded))
	for i, task := range decoded {
		addresses[i] = IdentityAddress(typeKey, task)
		if seen[addresses[i]] {
			return loopExpansionNames(typeKey, items), true
		}
		seen[addresses[i]] = true
	}
	return addresses, true
}

// expandLoopGroup produces one group TaskEnvelope per iteration the
// envelope's loop resolves to. The base envelope already carries the
// resolved Loop value; blockNode / rescueNode / alwaysNode are the raw
// YAML sequence nodes of nested task entries from the parsed entry.
//
// For each iteration, the three clause nodes are YAML-marshalled,
// sigil-rendered with `.item` / `.index` set, then re-parsed through the
// shared structural parser and recursed through buildEnvelopesFromEntry
// so child envelopes inherit the iteration's `.item` / `.index` in
// every nested task body. The expanded group envelope inherits Tags /
// When / Register from the base; LoopItem / LoopIndex carry the
// iteration value so the per-group `when:` evaluation can see them.
func expandLoopGroup(base *TaskEnvelope, blockNode, rescueNode, alwaysNode *yaml.Node, sigilContext, exprContext map[string]interface{}) ([]*TaskEnvelope, error) {
	items, err := resolveLoopList(base.Loop, exprContext)
	if err != nil {
		return nil, err
	}

	blockBytes, err := yaml.Marshal(blockNode)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal block body: %w", err)
	}
	var rescueBytes, alwaysBytes []byte
	if rescueNode != nil {
		rescueBytes, err = yaml.Marshal(rescueNode)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal rescue body: %w", err)
		}
	}
	if alwaysNode != nil {
		alwaysBytes, err = yaml.Marshal(alwaysNode)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal always body: %w", err)
		}
	}

	names := loopExpansionNames(base.Name, items)
	out := make([]*TaskEnvelope, 0, len(items))
	for i, item := range items {
		iterCtx := make(map[string]interface{}, safeCap(len(sigilContext), 2))
		for k, v := range sigilContext {
			iterCtx[k] = v
		}
		iterCtx["item"] = item
		iterCtx["index"] = i

		blockChildren, err := renderAndDecodeGroupClause(blockBytes, "block", iterCtx, sigilContext, exprContext, names[i], i)
		if err != nil {
			return nil, err
		}
		if len(blockChildren) == 0 {
			return nil, fmt.Errorf("loop iteration %d: block: must contain at least one child task", i)
		}

		var rescueChildren []*TaskEnvelope
		if rescueBytes != nil {
			rescueChildren, err = renderAndDecodeGroupClause(rescueBytes, "rescue", iterCtx, sigilContext, exprContext, names[i], i)
			if err != nil {
				return nil, err
			}
		}

		var alwaysChildren []*TaskEnvelope
		if alwaysBytes != nil {
			alwaysChildren, err = renderAndDecodeGroupClause(alwaysBytes, "always", iterCtx, sigilContext, exprContext, names[i], i)
			if err != nil {
				return nil, err
			}
		}

		expanded := *base
		expanded.Loop = nil
		expanded.LoopItem = item
		expanded.LoopIndex = i
		expanded.IsLoopExpansion = true
		expanded.Block = blockChildren
		expanded.Rescue = rescueChildren
		expanded.Always = alwaysChildren
		expanded.Name = names[i]

		out = append(out, &expanded)
	}
	return out, nil
}

// renderAndDecodeGroupClause renders a single group clause's YAML for
// one loop iteration and decodes the result into child envelopes. The
// per-iteration sigil context carries `.item` / `.index` so every nested
// task body sees the iteration value (per #211: each group iteration's
// `.item` / `.index` is shared across all its children). The file-level
// sigilContext stays available so other inputs continue to render.
//
// parentName is this iteration's group name; an unnamed child group extends
// it, so children of two iterations of the same loop do not collide.
func renderAndDecodeGroupClause(body []byte, clause string, iterCtx, sigilContext, exprContext map[string]interface{}, parentName string, iter int) ([]*TaskEnvelope, error) {
	rendered, err := RenderTemplate(body, iterCtx, "loop")
	if err != nil {
		return nil, fmt.Errorf("loop iteration %d %s: render error: %w", iter, clause, err)
	}
	renderedBytes, err := io.ReadAll(&rendered)
	if err != nil {
		return nil, fmt.Errorf("loop iteration %d %s: read error: %w", iter, clause, err)
	}

	entries, err := parseTaskEntrySeq(renderedBytes, "")
	if err != nil {
		return nil, fmt.Errorf("loop iteration %d %s: %s", iter, clause, err)
	}

	out := make([]*TaskEnvelope, 0, len(entries))
	childPath := parentName + "." + clause
	for i, entry := range entries {
		envelopes, err := buildEnvelopesFromEntry(entry, childPath, sigilContext, exprContext)
		if err != nil {
			return nil, fmt.Errorf("loop iteration %d %s[%d]: %s", iter, clause, i, err)
		}
		out = append(out, envelopes...)
	}
	return out, nil
}

// resolveLoopList normalises a loop value into a concrete list. Strings
// are compiled and evaluated as expr programs; lists are returned
// directly. Any other type yields an error.
func resolveLoopList(loop interface{}, exprContext map[string]interface{}) ([]interface{}, error) {
	switch v := loop.(type) {
	case nil:
		return nil, fmt.Errorf("loop value is nil")
	case []interface{}:
		return v, nil
	case string:
		prog, err := CompilePredicate(v)
		if err != nil {
			return nil, fmt.Errorf("loop expression compile error: %w", err)
		}
		return EvalList(prog, exprContext)
	}
	// Typed slices / arrays - normalise via reflection.
	if list, err := reflectToList(loop); err == nil {
		return list, nil
	}
	return nil, fmt.Errorf("loop value must be a list or expr string; got %T", loop)
}

// loopExpansionName derives a unique map key for each loop expansion.
// Scalar items render as `<name> (item=<value>)`; complex items (maps,
// lists, structs) or values longer than loopItemNameLimit fall back to
// `<name> (item=#<index>)` so the resulting key stays readable.
func loopExpansionName(base string, item interface{}, index int) string {
	if base == "" {
		base = fmt.Sprintf("loop task #%d", index+1)
	}
	rendered := renderItemForName(item)
	if rendered == "" || len(rendered) > loopItemNameLimit {
		return fmt.Sprintf("%s (item=#%d)", base, index)
	}
	return fmt.Sprintf("%s (item=%s)", base, rendered)
}

// loopExpansionNames derives the per-iteration task name for every item
// in a loop, guaranteeing a unique map key per iteration. Distinct scalar
// items keep the readable `<base> (item=<value>)` form; items that would
// otherwise collide - duplicate scalars, or scalars equal only after
// TrimSpace - get an ` #<index>` suffix so every iteration survives
// instead of overwriting an earlier one or tripping the duplicate-name
// guard (#320). Complex items already carry an index-based suffix and
// never collide.
func loopExpansionNames(base string, items []interface{}) []string {
	names := make([]string, len(items))
	counts := make(map[string]int, len(items))
	for i, item := range items {
		names[i] = loopExpansionName(base, item, i)
		counts[names[i]]++
	}
	for i, name := range names {
		if counts[name] > 1 {
			names[i] = fmt.Sprintf("%s #%d", name, i)
		}
	}
	return names
}

// renderItemForName returns a stringified item value safe for use in a
// task-name suffix. Returns "" for non-scalar values so the caller can
// fall back to an index-based suffix.
//
// A string item is trimmed so `(item= api )` reads as `(item=api)`. That
// trimming is why subprocess.cleanSensitive registers a padded secret's
// trimmed spelling as well as its literal one: masking matches literals, and
// the name is masked after this rendering, not before (#473).
func renderItemForName(item interface{}) string {
	switch v := item.(type) {
	case nil:
		return "nil"
	case string:
		return strings.TrimSpace(v)
	case bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
		return fmt.Sprint(v)
	}
	return ""
}
