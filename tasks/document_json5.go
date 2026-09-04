package tasks

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// The bridge between the JSON5 AST in format_json5.go and the yaml.Node
// interchange every cross-format conversion passes through.
//
// Both formatters already keep comments; these walkers are what let a
// conversion keep them too, instead of round-tripping through plain Go
// values and coming out stripped.

// json5DocumentToYAML converts a parsed JSON5 document into the yaml.Node
// document the interchange is made of.
func json5DocumentToYAML(root *json5Node) (*yaml.Node, error) {
	if root == nil {
		return nil, fmt.Errorf("empty json5 document")
	}
	body, err := json5NodeToYAMLNode(root)
	if err != nil {
		return nil, err
	}
	// Comments sitting above the whole document attach to the body node
	// rather than to the DocumentNode, because that is where yaml.v3 puts
	// them when it parses a document with no explicit `---`; matching it
	// keeps a converted recipe stable under a second `docket fmt`.
	body.HeadComment = joinComments(json5CommentsToYAML(root.HeadComments), body.HeadComment)
	body.FootComment = joinComments(body.FootComment, json5CommentsToYAML(root.AfterComments))

	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{body}}, nil
}

// json5NodeToYAMLNode converts one JSON5 AST node and everything under it.
func json5NodeToYAMLNode(n *json5Node) (*yaml.Node, error) {
	switch n.Kind {
	case json5Object:
		out := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		seen := make(map[string]bool, len(n.Members))
		for _, m := range n.Members {
			if seen[m.Key] {
				return nil, fmt.Errorf("duplicate key %q", m.Key)
			}
			seen[m.Key] = true

			key := yamlStringNode(m.Key)
			key.HeadComment = json5CommentsToYAML(m.HeadComments)
			// The trailing comment goes on the KEY, not the value. yaml.v3
			// stashes a key's line comment and replays it in the right
			// place for either shape: beside the value for `name: web #
			// note`, and straight after the colon for a block value, as
			// `tasks: # note`. Putting it on the value instead would push
			// a container's comment down past the entire block.
			key.LineComment = flattenComment(json5CommentsToYAML([]string{m.LineComment}))

			value, err := json5NodeToYAMLNode(m.Value)
			if err != nil {
				return nil, fmt.Errorf("key %q: %w", m.Key, err)
			}
			out.Content = append(out.Content, key, value)
		}
		out.FootComment = json5CommentsToYAML(n.FootComments)
		return out, nil

	case json5Array:
		out := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for i, e := range n.Elements {
			value, err := json5NodeToYAMLNode(e.Value)
			if err != nil {
				return nil, fmt.Errorf("element %d: %w", i, err)
			}
			value.HeadComment = joinComments(json5CommentsToYAML(e.HeadComments), value.HeadComment)
			if lc := flattenComment(json5CommentsToYAML([]string{e.LineComment})); lc != "" {
				value.LineComment = lc
			}
			out.Content = append(out.Content, value)
		}
		out.FootComment = json5CommentsToYAML(n.FootComments)
		return out, nil

	case json5Scalar:
		return json5ScalarToYAMLNode(n.Raw)
	}
	return nil, fmt.Errorf("unknown json5 node kind %d", n.Kind)
}

// json5ScalarToYAMLNode turns a raw JSON5 scalar token into a typed YAML
// scalar node.
func json5ScalarToYAMLNode(raw string) (*yaml.Node, error) {
	if raw == "" {
		return nil, fmt.Errorf("empty json5 scalar")
	}
	if raw[0] == '"' || raw[0] == '\'' {
		decoded, ok := decodeJSON5String(raw)
		if !ok {
			return nil, fmt.Errorf("invalid json5 string literal %s", raw)
		}
		return yamlStringNode(decoded), nil
	}

	switch raw {
	case "true", "false":
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: raw}, nil
	case "null":
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	case "Infinity", "+Infinity":
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: ".inf"}, nil
	case "-Infinity":
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: "-.inf"}, nil
	case "NaN", "+NaN", "-NaN":
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: ".nan"}, nil
	}

	if c := raw[0]; c == '+' || c == '-' || c == '.' || (c >= '0' && c <= '9') {
		return json5NumberToYAMLNode(raw)
	}

	// A bare word that is not one of the JSON5 keywords. parseValue accepts
	// any identifier as a scalar, so the AST will happily carry `web` where
	// `"web"` was meant, but titanous/json5 rejects it and so does every
	// other JSON5 reader - the file is already broken. Passing it through as
	// a string would invent a value rather than report one.
	return nil, fmt.Errorf("unquoted value %q is not valid json5", raw)
}

// json5NumberToYAMLNode types a JSON5 numeric literal.
//
// Hex is normalised to decimal rather than passed through, even though
// YAML would accept `0x1F`: json5ToYAMLBytes, which is what `validate`
// reads a JSON5 recipe through, already decodes to a number and re-emits
// it in decimal, and `fmt` must not disagree with `validate` about what a
// recipe says.
func json5NumberToYAMLNode(raw string) (*yaml.Node, error) {
	negative := strings.HasPrefix(raw, "-")
	digits := strings.TrimLeft(raw, "+-")

	if len(digits) > 2 && digits[0] == '0' && (digits[1] == 'x' || digits[1] == 'X') {
		magnitude, err := strconv.ParseUint(digits[2:], 16, 64)
		if err != nil {
			return nil, fmt.Errorf("json5 number %s is out of range", raw)
		}
		text := strconv.FormatUint(magnitude, 10)
		if negative {
			text = "-" + text
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: text}, nil
	}

	if !strings.ContainsAny(digits, ".eE") {
		if i, err := strconv.ParseInt(raw, 10, 64); err == nil {
			return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(i, 10)}, nil
		}
		if !negative {
			if u, err := strconv.ParseUint(digits, 10, 64); err == nil {
				return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatUint(u, 10)}, nil
			}
		}
		return nil, fmt.Errorf("json5 number %s is out of range", raw)
	}

	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid json5 number %s", raw)
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: formatFloatForYAML(f)}, nil
}

// yamlStringNode builds the string scalar every piece of text entering the
// interchange becomes.
//
// The explicit !!str tag does most of the work: yaml.v3 compares it
// against what the bare value would resolve to and force-quotes on a
// mismatch, so "true", "5", "null", "~" and "" come out quoted and read
// back as strings. Style is left at zero so the emitter keeps its own good
// judgement - a value holding a newline becomes a literal block scalar,
// and degrades to a quoted one when the block form cannot carry it.
//
// The tag alone is not quite enough, though. A value of "<<" is written
// plain, because the encoder's quoting check does not consider the merge
// tag, and `g: <<` reads back as !!merge rather than as the string it was.
// Rather than enumerate the escapees, stringScalarSurvivesEncoding asks
// yaml.v3 directly and only forces quotes where the answer comes back
// wrong, which keeps a sigil template as the single-quoted '{{ .app }}'
// the emitter would have chosen on its own.
func yamlStringNode(s string) *yaml.Node {
	node := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
	if containsInterpolation(s) || !stringScalarSurvivesEncoding(s) {
		node.Style = yaml.DoubleQuotedStyle
	}
	return node
}

// containsInterpolation reports whether a value holds a sigil template.
//
// The quote characters around one are load-bearing, not cosmetic, because
// a recipe is rendered as TEXT and only then parsed: the `dq` filter
// escapes a value for a double-quoted scalar, JSON-style, so `"{{ .app |
// dq }}"` renders to `"we\"b"` and reads back as `we"b`. Left to itself
// the emitter picks single quotes for a value starting with `{`, and
// `'we\"b'` reads back as `we\"b` - a literal backslash, silently.
//
// JSON5 strings are double-quoted, so a double-quoted YAML scalar is the
// faithful rendering of one, and forcing the style here is what keeps a
// converted recipe meaning what it meant. The reverse direction has a
// matching gap that cannot be closed the same way: canonical JSON5 has no
// single-quoted string, so a YAML `'{{ .app }}'` - safe precisely because
// single quotes tolerate a double quote in the value - becomes the
// double-quoted form, which `docket validate` then reports as
// unsafe_input_value. That is #538.
func containsInterpolation(s string) bool {
	return strings.Contains(s, "{{")
}

// stringScalarSurvivesEncoding reports whether yaml.v3 writes s in a form
// that reads back as the same string, left to its own devices.
//
// Asking by doing is deliberate. The alternative is a list of values the
// encoder mishandles, which would be right until the day yaml.v3 changes
// or a format adds a new special token, and wrong silently thereafter.
func stringScalarSurvivesEncoding(s string) bool {
	out, err := yaml.Marshal(&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s})
	if err != nil {
		return false
	}
	var back yaml.Node
	if err := yaml.Unmarshal(out, &back); err != nil {
		return false
	}
	body := documentBody(&back)
	return body != nil && body.Kind == yaml.ScalarNode && body.Tag == "!!str" && body.Value == s
}

// formatFloatForYAML renders a float in a spelling that resolves back to
// !!float rather than !!int.
func formatFloatForYAML(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return ".inf"
	case math.IsInf(f, -1):
		return "-.inf"
	case math.IsNaN(f):
		return ".nan"
	}
	text := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(text, ".eE") {
		text += ".0"
	}
	return text
}

// yamlDocumentToJSON5 converts an interchange document into the JSON5 AST
// the emitter in format_json5.go writes.
func yamlDocumentToJSON5(doc *yaml.Node) (*json5Node, error) {
	body := documentBody(doc)
	if body == nil {
		return nil, fmt.Errorf("cannot convert an empty document")
	}
	out, err := yamlNodeToJSON5Node(body)
	if err != nil {
		return nil, err
	}

	head := yamlCommentToJSON5(body.HeadComment)
	if doc != nil && doc.Kind == yaml.DocumentNode {
		head = append(yamlCommentToJSON5(doc.HeadComment), head...)
	}
	out.HeadComments = append(head, out.HeadComments...)

	// Comments after the value entirely live in AfterComments, kept apart
	// from a container's own FootComments so emitJSON5 writes each exactly
	// once - one inside the closing bracket, one after it.
	after := yamlCommentToJSON5(body.FootComment)
	if doc != nil && doc.Kind == yaml.DocumentNode {
		after = append(after, yamlCommentToJSON5(doc.FootComment)...)
	}
	out.AfterComments = append(out.AfterComments, after...)

	return out, nil
}

// yamlNodeToJSON5Node converts one interchange node and everything below.
func yamlNodeToJSON5Node(n *yaml.Node) (*json5Node, error) {
	switch n.Kind {
	case yaml.DocumentNode:
		return yamlDocumentToJSON5(n)

	case yaml.AliasNode:
		// Convert inlines every alias before it gets here, so reaching this
		// means the flattening pass was skipped or missed a node.
		return nil, fmt.Errorf("internal: unresolved YAML alias *%s reached the json5 writer", n.Value)

	case yaml.MappingNode:
		out := &json5Node{Kind: json5Object}
		seen := make(map[string]bool, len(n.Content)/2)
		var pending []string
		for i := 0; i+1 < len(n.Content); i += 2 {
			keyNode, valueNode := n.Content[i], n.Content[i+1]
			key, err := yamlKeyText(keyNode)
			if err != nil {
				return nil, err
			}
			if seen[key] {
				return nil, fmt.Errorf("duplicate key %q after conversion", key)
			}
			seen[key] = true

			value, err := yamlNodeToJSON5Node(valueNode)
			if err != nil {
				return nil, fmt.Errorf("key %q: %w", key, err)
			}
			// A container value carries its own head comments in the JSON5
			// AST only for the root; for a member they belong to the member,
			// so they are hoisted here and cleared to avoid a double print.
			head := append(pending, yamlCommentToJSON5(keyNode.HeadComment)...)
			head = append(head, yamlCommentToJSON5(valueNode.HeadComment)...)
			head = append(head, value.HeadComments...)
			value.HeadComments = nil
			pending = nil

			member := &json5Member{Key: key, Value: value, HeadComments: head}
			member.LineComment = firstComment(valueNode.LineComment, keyNode.LineComment)
			out.Members = append(out.Members, member)

			// yaml.v3 migrates a value's foot comment onto its key, and a
			// foot comment has no anchor of its own in JSON5, so it becomes
			// the head of whatever pair follows - or the container's foot
			// comment when nothing does.
			pending = append(pending, yamlCommentToJSON5(keyNode.FootComment)...)
		}
		out.FootComments = append(pending, yamlCommentToJSON5(n.FootComment)...)
		return out, nil

	case yaml.SequenceNode:
		out := &json5Node{Kind: json5Array}
		var pending []string
		for i, item := range n.Content {
			value, err := yamlNodeToJSON5Node(item)
			if err != nil {
				return nil, fmt.Errorf("element %d: %w", i, err)
			}
			head := append(pending, yamlCommentToJSON5(item.HeadComment)...)
			head = append(head, value.HeadComments...)
			value.HeadComments = nil
			pending = nil

			element := &json5Element{Value: value, HeadComments: head}
			element.LineComment = firstComment(item.LineComment)
			out.Elements = append(out.Elements, element)

			pending = append(pending, yamlCommentToJSON5(item.FootComment)...)
		}
		out.FootComments = append(pending, yamlCommentToJSON5(n.FootComment)...)
		return out, nil

	case yaml.ScalarNode:
		raw, err := yamlScalarToJSON5Raw(n)
		if err != nil {
			return nil, err
		}
		return &json5Node{Kind: json5Scalar, Raw: raw}, nil
	}
	return nil, fmt.Errorf("unsupported YAML node kind %d", n.Kind)
}

// yamlScalarToJSON5Raw renders a YAML scalar as a JSON5 token.
//
// Dispatch is on the tag and never on the style: yaml.v3 resolves any
// quoted or block scalar to !!str, so the tag already answers the question
// a style inspection would be asking, including for `|` and `>`.
//
// !!timestamp and !!binary widen to strings. JSON5 has no date or binary
// literal, and docket's own recipe structs hold both as strings, so the
// widening is invisible to everything downstream - but it is a widening,
// and equivalentConvertedNodes knows to accept it.
func yamlScalarToJSON5Raw(n *yaml.Node) (string, error) {
	switch n.Tag {
	case "", "!!str", "!!timestamp", "!!binary":
		return quoteJSON5String(n.Value), nil
	case "!!null":
		return "null", nil
	case "!!bool":
		var b bool
		if err := n.Decode(&b); err != nil {
			return "", fmt.Errorf("invalid boolean %q: %w", n.Value, err)
		}
		return strconv.FormatBool(b), nil
	case "!!int":
		// Decoding through yaml.v3 rather than re-lexing the spelling here
		// is what keeps `fmt` and the loader agreeing on a value: YAML
		// accepts 0o17, 0b1010, 1_000 and +5 as ints and JSON5 accepts none
		// of them, and the only authority on which integer each means is
		// the resolver the loader itself uses.
		var i int64
		if err := n.Decode(&i); err == nil {
			return strconv.FormatInt(i, 10), nil
		}
		var u uint64
		if err := n.Decode(&u); err == nil {
			return strconv.FormatUint(u, 10), nil
		}
		return "", fmt.Errorf("integer %q is out of range for json5", n.Value)
	case "!!float":
		var f float64
		if err := n.Decode(&f); err != nil {
			return "", fmt.Errorf("invalid float %q: %w", n.Value, err)
		}
		return formatFloatForJSON5(f), nil
	}
	return "", fmt.Errorf("YAML tag %s has no json5 representation", n.Tag)
}

// formatFloatForJSON5 renders a float using JSON5's spellings for the
// non-finite values.
func formatFloatForJSON5(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "Infinity"
	case math.IsInf(f, -1):
		return "-Infinity"
	case math.IsNaN(f):
		return "NaN"
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

// yamlKeyText renders a mapping key as the string a JSON5 object key is.
//
// A non-scalar key - YAML's `? [a, b] : v` - is refused rather than
// stringified: there is no spelling of it JSON5 could read back, and
// inventing one would quietly change what the recipe says.
func yamlKeyText(n *yaml.Node) (string, error) {
	if n.Kind != yaml.ScalarNode {
		return "", fmt.Errorf("mapping key at line %d is not a scalar; json5 object keys must be strings", n.Line)
	}
	switch n.Tag {
	case "", "!!str", "!!timestamp", "!!binary":
		return n.Value, nil
	}
	return yamlScalarToJSON5Raw(n)
}

// firstComment returns the first non-empty comment, rendered as a single
// JSON5 line comment.
func firstComment(comments ...string) string {
	for _, c := range comments {
		if strings.TrimSpace(c) == "" {
			continue
		}
		return json5CommentLine(flattenComment(c))
	}
	return ""
}

// joinComments concatenates comment blocks, skipping empty ones.
func joinComments(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, "\n")
}
