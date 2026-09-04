package tasks

import "strings"

// Comment translation between the two surface syntaxes.
//
// YAML and JSON5 both let a recipe carry commentary, and `docket fmt`
// preserves it within a format, so a conversion that dropped it would be
// no better than the round trip through an external tool that #418 exists
// to replace. The two formatters store comments differently, though:
// yaml.v3 keeps one string per anchor point with the `#` included and
// embedded newlines for a multi-line run, while the JSON5 AST keeps a
// slice of raw tokens with their `//` or `/* */` delimiters intact.

// json5CommentsToYAML renders a run of raw JSON5 comment tokens as the
// single `#`-prefixed string yaml.v3 wants, one source line per output
// line.
func json5CommentsToYAML(raws []string) string {
	var lines []string
	for _, raw := range raws {
		// An absent comment is the empty string - json5Member.LineComment
		// is not a slice, so "no trailing comment" arrives as one. Rendering
		// it would put a bare "#" beside every key that had nothing to say.
		if strings.TrimSpace(raw) == "" {
			continue
		}
		lines = append(lines, json5CommentBodyLines(raw)...)
	}
	if len(lines) == 0 {
		return ""
	}
	for i, line := range lines {
		lines[i] = yamlCommentLine(line)
	}
	return strings.Join(lines, "\n")
}

// json5CommentBodyLines strips a raw comment token's delimiters and
// returns its text, one entry per line. A block comment spanning several
// lines yields several entries; the delimiters themselves never survive,
// since the YAML side has only one comment syntax to render into.
func json5CommentBodyLines(raw string) []string {
	switch {
	case strings.HasPrefix(raw, "//"):
		return []string{strings.TrimRight(strings.TrimPrefix(raw, "//"), " \t")}
	case strings.HasPrefix(raw, "/*"):
		body := strings.TrimPrefix(raw, "/*")
		body = strings.TrimSuffix(body, "*/")
		lines := strings.Split(body, "\n")
		out := make([]string, 0, len(lines))
		for _, line := range lines {
			out = append(out, strings.TrimRight(line, " \t"))
		}
		// A `/* ... */` written across lines usually opens and closes on
		// blank fragments; dropping those keeps the YAML from gaining two
		// empty comment lines it never had.
		return trimEmptyEdges(out)
	}
	return []string{raw}
}

// yamlCommentLine turns one line of comment text into the form yaml.v3
// stores and re-emits.
//
// An empty line has to come back as a bare "#", never "". The emitter
// writes an empty comment line through as a real blank line, which on
// re-parse ends the comment and splits what was one head comment in two -
// so a converted recipe would stop being idempotent under `docket fmt`.
func yamlCommentLine(text string) string {
	trimmed := strings.TrimRight(text, " \t")
	if strings.TrimSpace(trimmed) == "" {
		return "#"
	}
	if strings.HasPrefix(strings.TrimSpace(trimmed), "#") {
		return strings.TrimSpace(trimmed)
	}
	if strings.HasPrefix(trimmed, " ") {
		return "#" + trimmed
	}
	return "# " + trimmed
}

// yamlCommentToJSON5 renders a yaml.v3 comment string as JSON5 comment
// tokens, one per line.
//
// Always `//`, never `/* */`: a line comment runs to end of line, so no
// comment text can terminate it early, while a `*/` sitting inside the
// text would close a block comment in the middle and turn the remainder
// into syntax. The cost is that a JSON5 `/* x */` comes back as `// x`
// after a round trip, which is a normalisation rather than a loss.
func yamlCommentToJSON5(comment string) []string {
	if strings.TrimSpace(comment) == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(comment, "\n") {
		out = append(out, json5CommentLine(line))
	}
	return trimEmptyEdges(out)
}

// json5CommentLine turns one line of YAML comment text into a `//` token.
func json5CommentLine(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "#") {
		return "//" + strings.TrimSuffix(strings.TrimPrefix(trimmed, "#"), " ")
	}
	return "// " + trimmed
}

// flattenComment collapses a multi-line comment onto one line, for the
// trailing-comment slots that only hold one.
//
// Flattening keeps the comment where the author put it. Demoting it to a
// head comment instead would preserve every word but move it above the
// thing it was annotating, which reads worse than a long line.
func flattenComment(comment string) string {
	fields := strings.Fields(strings.ReplaceAll(comment, "\n", " "))
	return strings.Join(fields, " ")
}

// trimEmptyEdges drops leading and trailing empty entries, leaving any
// interior ones alone - a deliberate blank line inside a comment block is
// the author's paragraph break and survives.
func trimEmptyEdges(lines []string) []string {
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	end := len(lines)
	for end > start && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	if start >= end {
		return nil
	}
	return lines[start:end]
}
