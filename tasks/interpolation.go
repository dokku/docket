package tasks

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"

	yaml "gopkg.in/yaml.v3"
)

// The quotes around an interpolation are part of what a recipe means, not
// a spelling choice. A recipe is rendered as TEXT and only then parsed, so
// the quote characters decide how the substituted value is escaped: a
// single-quoted scalar tolerates a double quote in the value, a
// double-quoted one needs `| dq`, and a block scalar escapes nothing at
// all. Rewriting one as the other therefore changes which values the
// recipe can carry, silently, long after `fmt` has run.
//
// `docket fmt` refuses rather than write such a recipe, the same stance it
// takes on an unquoted interpolation in Format. This file holds the shared
// half of that rule: what counts as an interpolation worth protecting, and
// how the refusal is worded. The two callers differ only in where they
// find the scalars - Convert walks a yaml.Node tree, the JSON5 side walks
// a token stream.

// interpolationOpen and interpolationClose are the sigil delimiters as
// written in a recipe file.
//
// They are literals rather than render.go's leftDelim / rightDelim, which
// SIGIL_DELIMS can repoint: those describe the environment a recipe will
// be RENDERED in, and `fmt` never renders. Reading the file as written is
// the whole reason fmt sees this problem at all, and containsInterpolation
// hardcodes the same pair for the same reason.
const (
	interpolationOpen  = "{{"
	interpolationClose = "}}"
)

// maxQuotingSitesNamed bounds how many sites one error spells out, so a
// recipe with dozens does not produce an unreadable line. The count is
// always exact; only the enumeration is capped.
const maxQuotingSitesNamed = 5

// controlActions are the template keywords that open, continue or close a
// block rather than emit a value.
//
// An action built on one of these substitutes only literal recipe text -
// `'web{{ if .debug }}-verbose{{ end }}'` renders to `web-verbose` or
// `web`, neither of which can carry a quote the value did not already
// have - so the quoting around it is genuinely cosmetic and re-quoting it
// is safe. Warning about those would be noise the user cannot silence.
var controlActions = map[string]bool{
	"if":       true,
	"else":     true,
	"end":      true,
	"range":    true,
	"with":     true,
	"template": true,
	"block":    true,
	"define":   true,
	"break":    true,
	"continue": true,
}

// quotingSite is one scalar whose quoting a rewrite would change.
type quotingSite struct {
	// Line is the 1-based source line of the scalar.
	Line int

	// Action is the verbatim `{{ ... }}` text of the first interpolation
	// in the scalar that is at risk. Only the action is kept, never the
	// whole scalar: a scalar may hold a sensitive input's default, and
	// the action alone is what the user has to edit.
	Action string

	// Style names how the scalar is written, worded to follow "is".
	Style string
}

// riskyInterpolations returns the actions in s that substitute a value and
// are not already escaped for a double-quoted scalar, in source order.
//
// An action already piped through `dq` is left out because re-quoting it
// is a fix rather than a break: `dq` escapes JSON-style, which is what a
// double-quoted scalar reads, so `'{{ .app | dq }}'` is a recipe that does
// not work today and `"{{ .app | dq }}"` is one that does.
func riskyInterpolations(s string) []string {
	var risky []string
	for i := 0; i < len(s); {
		open := strings.Index(s[i:], interpolationOpen)
		if open < 0 {
			break
		}
		open += i
		body := open + len(interpolationOpen)
		end := strings.Index(s[body:], interpolationClose)
		if end < 0 {
			// An unterminated `{{` is not an action at all; the render
			// would fail on it long before the quoting mattered.
			break
		}
		end += body
		if actionSubstitutesValue(s[body:end]) {
			risky = append(risky, s[open:end+len(interpolationClose)])
		}
		i = end + len(interpolationClose)
	}
	return risky
}

// actionSubstitutesValue reports whether an action body emits text that
// came from outside the recipe, and does so unescaped.
func actionSubstitutesValue(body string) bool {
	body = trimActionMarkers(body)
	if body == "" {
		return false
	}
	// A template comment emits nothing.
	if strings.HasPrefix(body, "/*") {
		return false
	}
	if controlActions[firstWord(body)] {
		return false
	}
	return firstWord(lastPipelineStage(body)) != "dq"
}

// trimActionMarkers strips the surrounding whitespace and the `{{-` / `-}}`
// whitespace-trim markers from an action body.
func trimActionMarkers(body string) string {
	body = strings.TrimSpace(body)
	body = strings.TrimPrefix(body, "-")
	body = strings.TrimSuffix(body, "-")
	return strings.TrimSpace(body)
}

// firstWord returns the leading run of non-space characters in s.
func firstWord(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexFunc(s, unicode.IsSpace); i >= 0 {
		return s[:i]
	}
	return s
}

// lastPipelineStage returns the text after the last top-level `|` in an
// action body, or the whole body when it has no pipe.
//
// The scan skips `|` inside a quoted run so `{{ printf "a|b" .x }}` is read
// as one stage. text/template has three quoting forms and this has to know
// all of them: a "..." string, a `...` raw string, and a '...' rune
// literal, which is not a string but can still hold a pipe.
func lastPipelineStage(body string) string {
	last := -1
	var quote byte
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case quote != 0:
			// A raw string has no escapes; the other two do.
			if c == '\\' && quote != '`' {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '`' || c == '\'':
			quote = c
		case c == '|':
			last = i
		}
	}
	if last < 0 {
		return body
	}
	return body[last+1:]
}

// yamlQuotingSites collects the scalars in a decoded YAML document whose
// quoting a rewrite into a double-quoted string would change.
//
// Every style but the double-quoted one qualifies. A single-quoted scalar
// and a block scalar both tolerate a double quote in the value; a plain
// one tolerates both quote characters and breaks on other things entirely.
// None of them survive being written as `"..."` unchanged.
//
// Mapping keys are walked alongside values - Content holds both - because
// a key carrying an interpolation is substituted the same way.
//
// At most one site is reported per scalar: a scalar with three risky
// actions is still one line to edit, and one entry reads better than
// three identical line numbers.
func yamlQuotingSites(n *yaml.Node) []quotingSite {
	if n == nil {
		return nil
	}
	var sites []quotingSite
	if n.Kind == yaml.ScalarNode && n.Style&yaml.DoubleQuotedStyle == 0 {
		if risky := riskyInterpolations(n.Value); len(risky) > 0 {
			sites = append(sites, quotingSite{
				Line:   n.Line,
				Action: risky[0],
				Style:  yamlScalarStyleName(n.Style),
			})
		}
	}
	for _, child := range n.Content {
		sites = append(sites, yamlQuotingSites(child)...)
	}
	return sites
}

// yamlScalarStyleName names a scalar's style, worded to read after "is".
func yamlScalarStyleName(style yaml.Style) string {
	switch {
	case style&yaml.SingleQuotedStyle != 0:
		return "single-quoted"
	case style&yaml.LiteralStyle != 0:
		return "in a literal block scalar"
	case style&yaml.FoldedStyle != 0:
		return "in a folded block scalar"
	}
	return "unquoted"
}

// json5QuotingSites collects the single-quoted strings in JSON5 source
// whose quoting a rewrite would change.
//
// It runs over the token stream rather than the AST because json5Node
// keeps only the scalar's raw text, with no position to report, while
// every json5Tok carries the offset its line is counted from. Lexing is
// also what makes the scan trustworthy: a `'` inside a double-quoted
// string is just a character to the lexer, and never mistaken for a
// delimiter.
//
// Only the single-quoted form qualifies. JSON5's other string is already
// double-quoted, and an unquoted key is an ASCII identifier, which cannot
// hold an interpolation.
func json5QuotingSites(src []byte) ([]quotingSite, error) {
	toks, err := lexJSON5(src)
	if err != nil {
		return nil, err
	}
	var sites []quotingSite
	for _, tok := range toks {
		if tok.Kind != tokString || len(tok.Raw) == 0 || tok.Raw[0] != '\'' {
			continue
		}
		// Decoded rather than raw: an escape can spell the braces.
		decoded, ok := decodeJSON5String(tok.Raw)
		if !ok {
			continue
		}
		risky := riskyInterpolations(decoded)
		if len(risky) == 0 {
			continue
		}
		sites = append(sites, quotingSite{
			Line:   lineForOffset(src, tok.Offset),
			Action: risky[0],
			Style:  "single-quoted",
		})
	}
	return sites, nil
}

// lineForOffset returns the 1-based line a byte offset falls on.
func lineForOffset(src []byte, offset int) int {
	if offset > len(src) {
		offset = len(src)
	}
	if offset < 0 {
		offset = 0
	}
	return 1 + bytes.Count(src[:offset], []byte("\n"))
}

// unportableQuotingError words the refusal, naming every site so a recipe
// with several does not cost several edit-and-rerun cycles.
//
// The wording names what the rewrite would produce rather than the format
// it is headed for, because the same refusal covers three paths: a YAML
// recipe converting to JSON5, a JSON5 recipe being formatted in place, and
// a JSON5 recipe converting to YAML. All three end with a double-quoted
// string; only the reason differs.
func unportableQuotingError(sites []quotingSite) error {
	if len(sites) == 1 {
		s := sites[0]
		return fmt.Errorf("line %d: `%s` is %s, and rewriting it double-quoted would leave a recipe that no longer tolerates a double quote in the value; %s", s.Line, s.Action, s.Style, dqAdvice(sites))
	}

	named := sites
	extra := 0
	if len(named) > maxQuotingSitesNamed {
		extra = len(named) - maxQuotingSitesNamed
		named = named[:maxQuotingSitesNamed]
	}
	parts := make([]string, 0, len(named))
	for _, s := range named {
		parts = append(parts, fmt.Sprintf("line %d `%s` (%s)", s.Line, s.Action, s.Style))
	}
	list := strings.Join(parts, ", ")
	if extra > 0 {
		list += fmt.Sprintf(", and %d more", extra)
	}
	return fmt.Errorf("%d interpolations would lose the quoting that keeps them safe: %s; %s", len(sites), list, dqAdvice(sites))
}

// dqAdvice words the remediation, carrying a worked spelling wherever one
// exists so the error holds the answer rather than a rule to apply.
//
// An action containing its own double quote has no such spelling: the
// scalar would have to escape it, and the template engine reads the recipe
// as text before any parser unescapes anything, so `{{ .app | default \"\"
// }}` reaches it with the backslashes still in place and fails to parse.
// Such an action cannot live in a double-quoted scalar in either format,
// which makes it the one case where the fix is to rewrite the template
// rather than the quotes.
func dqAdvice(sites []quotingSite) string {
	for _, s := range sites {
		suggestion, ok := suggestDq(s.Action)
		if !ok {
			continue
		}
		if len(sites) == 1 {
			return "write it as " + suggestion
		}
		return "write each one inside a double-quoted scalar with | dq, as in " + suggestion
	}
	subject, verb := "it", "stands"
	if len(sites) > 1 {
		subject, verb = "they", "stand"
	}
	return fmt.Sprintf(`%s cannot be written in a double-quoted scalar as %s %s either, because the recipe is rendered before it is parsed, so an escaped \" reaches the template engine with the backslash still on it - rewrite the action without the quoted literal, for example with a backquoted raw string`, subject, subject, verb)
}

// suggestDq rewrites an action as the double-quoted, dq-escaped spelling
// the user should replace it with. ok is false when the action carries its
// own double quote, which no double-quoted scalar can hold - see dqAdvice.
func suggestDq(action string) (string, bool) {
	if strings.Contains(action, `"`) {
		return "", false
	}
	body := strings.TrimSuffix(strings.TrimPrefix(action, interpolationOpen), interpolationClose)
	// A trailing whitespace-trim marker belongs against the closing
	// delimiter, so it has to move to the far side of the inserted filter.
	trim := ""
	if trimmed := strings.TrimRight(body, " \t"); strings.HasSuffix(trimmed, "-") {
		trim = "-"
		body = trimmed[:len(trimmed)-1]
	}
	return fmt.Sprintf(`"%s%s | dq %s%s"`, interpolationOpen, strings.TrimRight(body, " \t"), trim, interpolationClose), true
}
