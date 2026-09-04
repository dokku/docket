package tasks

import (
	"fmt"
	"strconv"

	yaml "gopkg.in/yaml.v3"
)

// mergeTag is the tag yaml.v3 resolves the `<<` merge key to.
const mergeTag = "!!merge"

// maxAliasDepth and maxAliasNodes bound alias expansion. A YAML alias can
// only refer to an anchor already parsed, so the reference graph is a DAG
// and cannot cycle - but a DAG expands exponentially when each level
// aliases the one above it, so a small file can still ask for an enormous
// tree. Refuse rather than exhaust memory.
const (
	maxAliasDepth = 100
	maxAliasNodes = 100000
)

// Convert renders a recipe written in the from codec as the same recipe in
// the to codec, canonically formatted.
//
// Comments survive, which is the whole point: `docket fmt` preserves them
// within a format, so a conversion that dropped them would be no better
// than the trip through an external tool that #418 exists to replace.
func Convert(data []byte, from, to Codec) ([]byte, error) {
	if from == nil {
		from = DefaultCodec()
	}
	if to == nil {
		to = DefaultCodec()
	}

	// Same format in and out is exactly what `docket fmt` has always done,
	// and it must keep producing the same bytes: every invocation that
	// predates --format lands here.
	if from.Name() == to.Name() {
		return from.Format(data)
	}

	// A source the reading codec already considers malformed is refused
	// here, where the problem can still be named. A duplicate-keyed JSON5
	// object is the case that matters: parseJSON5 keeps both members, so
	// without this the conversion runs to completion and dies at the
	// cross-format guard reporting that the recipe changed - true, but
	// useless. Formatting such a file in place is untouched; it returned
	// above, and has always failed with the JSON5 formatter's own
	// round-trip message.
	if problems := from.Lint(data); len(problems) > 0 {
		return nil, fmt.Errorf("cannot convert this %s recipe: %s", from.Name(), problems[0].Message)
	}

	doc, err := from.DecodeDocument(data)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		// No document to convert - an empty or comment-only file. `fmt`
		// leaves those alone, and inventing an empty recipe in the target
		// format would be a worse answer than doing nothing.
		return from.Format(data)
	}

	// Anchors, aliases and merge keys are YAML-only spellings, so the
	// flattening runs here rather than in a codec: any target that cannot
	// express sharing needs the same treatment. Neither pass is reachable
	// from a non-YAML source, and same-format formatting returned above,
	// so today's `docket fmt` never runs either one.
	if err := inlineAliases(doc); err != nil {
		return nil, err
	}
	if err := expandMergeKeys(doc); err != nil {
		return nil, err
	}

	out, err := to.EncodeDocument(doc)
	if err != nil {
		return nil, err
	}

	// The cross-format guard. Each codec's own Format already re-parses its
	// output and checks it against its own AST, but that only proves the
	// emitter did not corrupt what it was handed. This is the only check
	// that the walk between the two formats was faithful.
	back, err := to.DecodeDocument(out)
	if err != nil {
		return nil, fmt.Errorf("converted %s failed to parse back: %w", to.Name(), err)
	}
	if !equivalentConvertedNodes(documentBody(doc), documentBody(back)) {
		return nil, fmt.Errorf("%s to %s conversion changed the recipe; refusing to write", from.Name(), to.Name())
	}

	return out, nil
}

// inlineAliases replaces every alias with a copy of what it points at and
// drops the anchors that named them.
//
// Inlining rather than refusing is the right trade because it is what
// docket already does with the recipe: UnmarshalRecipe resolves anchors
// and merges during the parse, so nothing downstream can tell an inlined
// document from the original. What is lost is the fact that two blocks
// were once written once - which is exactly the thing a format with no
// anchors cannot record.
func inlineAliases(root *yaml.Node) error {
	return (&aliasInliner{budget: maxAliasNodes}).walk(root, 0)
}

type aliasInliner struct{ budget int }

func (in *aliasInliner) walk(n *yaml.Node, depth int) error {
	if n == nil {
		return nil
	}
	if depth > maxAliasDepth {
		return fmt.Errorf("YAML alias nesting is deeper than %d levels; too deep to convert", maxAliasDepth)
	}
	// The anchor declaration goes away; the value it named stays put.
	n.Anchor = ""
	for i, child := range n.Content {
		if child.Kind == yaml.AliasNode {
			if child.Alias == nil {
				return fmt.Errorf("YAML alias *%s does not resolve to an anchor", child.Value)
			}
			replacement, err := in.copy(child.Alias)
			if err != nil {
				return err
			}
			// A comment written beside the alias is about the alias site,
			// not about the anchor it borrows from, so it stays here.
			replacement.HeadComment = child.HeadComment
			replacement.LineComment = child.LineComment
			replacement.FootComment = child.FootComment
			n.Content[i] = replacement
			child = replacement
		}
		if err := in.walk(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}

// copy deep-copies an anchored subtree, charging each node against the
// expansion budget.
func (in *aliasInliner) copy(n *yaml.Node) (*yaml.Node, error) {
	if n == nil {
		return nil, nil
	}
	in.budget--
	if in.budget < 0 {
		return nil, fmt.Errorf("YAML alias expansion exceeds %d nodes; too large to convert", maxAliasNodes)
	}
	out := *n
	out.Anchor = ""
	out.Content = nil
	for _, child := range n.Content {
		copied, err := in.copy(child)
		if err != nil {
			return nil, err
		}
		out.Content = append(out.Content, copied)
	}
	return &out, nil
}

// expandMergeKeys rewrites every `<<:` into the keys it merges in.
//
// Precedence is YAML 1.1's: a key the mapping states itself beats a merged
// one, and an earlier merge source beats a later one. The walk is
// post-order so a source that merges something itself is already flat by
// the time it is read.
func expandMergeKeys(n *yaml.Node) error {
	if n == nil {
		return nil
	}
	for _, child := range n.Content {
		if err := expandMergeKeys(child); err != nil {
			return err
		}
	}
	if n.Kind != yaml.MappingNode || !hasMergeKey(n) {
		return nil
	}

	own := make(map[string]bool, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Tag != mergeTag {
			own[n.Content[i].Value] = true
		}
	}

	var out []*yaml.Node
	emitted := make(map[string]bool, len(n.Content)/2)
	// A merge key that contributes nothing still carries its comment;
	// pending holds it until there is a real key to attach it to.
	var pending string

	for i := 0; i+1 < len(n.Content); i += 2 {
		key, value := n.Content[i], n.Content[i+1]

		if key.Tag != mergeTag {
			if emitted[key.Value] {
				continue
			}
			emitted[key.Value] = true
			if pending != "" {
				key.HeadComment = joinComments(pending, key.HeadComment)
				pending = ""
			}
			out = append(out, key, value)
			continue
		}

		sources, err := mergeSources(value)
		if err != nil {
			return err
		}
		attached := false
		for _, source := range sources {
			for j := 0; j+1 < len(source.Content); j += 2 {
				sourceKey, sourceValue := source.Content[j], source.Content[j+1]
				if own[sourceKey.Value] || emitted[sourceKey.Value] {
					continue
				}
				emitted[sourceKey.Value] = true
				// The source may be merged into more than one mapping, so
				// each insertion gets its own copy.
				copiedKey, copiedValue := deepCopyNode(sourceKey), deepCopyNode(sourceValue)
				if !attached {
					copiedKey.HeadComment = joinComments(pending, key.HeadComment, copiedKey.HeadComment)
					if key.LineComment != "" {
						copiedKey.LineComment = key.LineComment
					}
					pending = ""
					attached = true
				}
				out = append(out, copiedKey, copiedValue)
			}
		}
		if !attached {
			pending = joinComments(pending, key.HeadComment)
		}
	}

	if pending != "" {
		n.HeadComment = joinComments(pending, n.HeadComment)
	}
	n.Content = out
	return nil
}

// hasMergeKey reports whether a mapping carries a `<<` pair.
func hasMergeKey(n *yaml.Node) bool {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Tag == mergeTag {
			return true
		}
	}
	return false
}

// mergeSources returns the mappings a `<<:` value merges in, left to right.
func mergeSources(value *yaml.Node) ([]*yaml.Node, error) {
	switch value.Kind {
	case yaml.MappingNode:
		return []*yaml.Node{value}, nil
	case yaml.SequenceNode:
		out := make([]*yaml.Node, 0, len(value.Content))
		for _, item := range value.Content {
			if item.Kind != yaml.MappingNode {
				return nil, fmt.Errorf("merge key << at line %d: every entry must be a mapping", value.Line)
			}
			out = append(out, item)
		}
		return out, nil
	}
	return nil, fmt.Errorf("merge key << at line %d must be a mapping or a sequence of mappings", value.Line)
}

// deepCopyNode copies a node and everything under it.
func deepCopyNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	out := *n
	out.Content = nil
	for _, child := range n.Content {
		out.Content = append(out.Content, deepCopyNode(child))
	}
	return &out
}

// equivalentConvertedNodes compares two trees by what their scalars mean
// rather than by how they are spelled.
//
// equivalentNodes, which guards each formatter against its own emitter,
// compares Value and tag literally and so cannot be reused here: a
// conversion normalises 0x1F to 31 on purpose, and widens a !!timestamp to
// the string JSON5 can carry. Both must compare equal. A lost key, a
// dropped escape or a changed type must not.
func equivalentConvertedNodes(a, b *yaml.Node) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case yaml.ScalarNode:
		return scalarValueKey(a) == scalarValueKey(b)
	case yaml.SequenceNode:
		if len(a.Content) != len(b.Content) {
			return false
		}
		for i := range a.Content {
			if !equivalentConvertedNodes(a.Content[i], b.Content[i]) {
				return false
			}
		}
		return true
	case yaml.MappingNode:
		if len(a.Content) != len(b.Content) {
			return false
		}
		index := make(map[string]*yaml.Node, len(b.Content)/2)
		for i := 0; i+1 < len(b.Content); i += 2 {
			index[convertedKeyText(b.Content[i])] = b.Content[i+1]
		}
		for i := 0; i+1 < len(a.Content); i += 2 {
			other, ok := index[convertedKeyText(a.Content[i])]
			if !ok || !equivalentConvertedNodes(a.Content[i+1], other) {
				return false
			}
		}
		return true
	}
	return false
}

// convertedKeyText renders a mapping key for comparison, applying the same
// widening to a string that the conversion does, so a !!int key and the
// string it becomes still line up.
func convertedKeyText(n *yaml.Node) string {
	text, err := yamlKeyText(n)
	if err != nil {
		return n.Value
	}
	return text
}

// scalarValueKey renders a scalar as a comparable string carrying its type
// and decoded value, so two spellings of one number match and a number and
// the string of it do not.
func scalarValueKey(n *yaml.Node) string {
	switch n.Tag {
	case "!!null":
		return "z"
	case "!!bool":
		var b bool
		if err := n.Decode(&b); err == nil {
			return "b:" + strconv.FormatBool(b)
		}
	case "!!int":
		var i int64
		if err := n.Decode(&i); err == nil {
			return "i:" + strconv.FormatInt(i, 10)
		}
		var u uint64
		if err := n.Decode(&u); err == nil {
			return "i:" + strconv.FormatUint(u, 10)
		}
	case "!!float":
		var f float64
		if err := n.Decode(&f); err == nil {
			return "f:" + formatFloatForJSON5(f)
		}
	}
	// !!str, !!timestamp, !!binary and anything unrecognised compare as
	// text, which is what makes the timestamp widening equal to itself.
	return "s:" + n.Value
}
