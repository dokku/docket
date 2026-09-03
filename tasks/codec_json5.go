package tasks

import (
	"encoding/json"
	"fmt"

	json5 "github.com/titanous/json5"
	yaml "gopkg.in/yaml.v3"
)

// FormatNameJSON5 is the canonical name of the JSON5 codec. It is spelled
// with "Name" because FormatJSON5 is already the JSON5 formatter.
const FormatNameJSON5 = "json5"

// json5Codec reads and writes recipes as JSON5, a strict superset of JSON
// that adds comments, trailing commas and unquoted keys. Every existing
// JSON file is already valid JSON5, which is why "json" is an alias rather
// than a codec of its own.
type json5Codec struct{}

func (json5Codec) Name() string { return FormatNameJSON5 }

func (json5Codec) Aliases() []string { return []string{"json"} }

func (json5Codec) Extensions() []string { return []string{"json", "json5"} }

// Sniff claims bytes that open like a JSON5 document. After optional
// whitespace, JSON5 always starts with `[`, `{`, or a comment; YAML
// recipes start with `-`, `---`, or a key, none of which collide. Anything
// else is left to the default codec, so an ambiguous stream stays YAML.
func (json5Codec) Sniff(data []byte) bool {
	for i := 0; i < len(data); i++ {
		c := data[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			continue
		}
		if c == '/' && i+1 < len(data) && (data[i+1] == '/' || data[i+1] == '*') {
			// A leading line/block comment - JSON5 idiom.
			return true
		}
		return c == '[' || c == '{'
	}
	return false
}

// ToYAML parses the JSON5 document and re-emits it as YAML so the rest of
// the pipeline (sigil render, yaml.Node walk, line/column reporting) keeps
// a single implementation. Sigil templates inside string values survive
// verbatim, being just text to both parsers.
func (json5Codec) ToYAML(data []byte) ([]byte, *Problem) {
	converted, err := json5ToYAMLBytes(data)
	if err != nil {
		return nil, &Problem{Code: "json5_parse", Message: err.Error()}
	}
	return converted, nil
}

// Lint rejects a duplicated key. json5.Unmarshal silently keeps the last
// of a repeated pair, so the scan runs over the original bytes to catch
// what the conversion has already thrown away - the way yaml.v3 rejects a
// duplicate YAML key (#318).
func (json5Codec) Lint(data []byte) []Problem {
	dup := detectJSON5DuplicateKeys(data)
	if dup == nil {
		return nil
	}
	return []Problem{{
		Code:    "duplicate_key",
		Line:    dup.Line,
		Column:  dup.Column,
		Message: fmt.Sprintf("duplicate key %q", dup.Key),
	}}
}

func (json5Codec) Format(data []byte) ([]byte, error) { return FormatJSON5(data) }

// Marshal renders v as a canonically formatted JSON5 recipe.
//
// It round-trips through YAML first so the struct yaml tags drive the key
// names - the same tags Plays() and the YAML codec use - and only then
// re-encodes as JSON for the JSON5 formatter. Marshalling v straight to
// JSON would key the document off json tags the task structs do not carry.
func (json5Codec) Marshal(v interface{}) ([]byte, error) {
	raw, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	var generic interface{}
	if err := yaml.Unmarshal(raw, &generic); err != nil {
		return nil, err
	}
	jsonRaw, err := json.Marshal(generic)
	if err != nil {
		return nil, err
	}
	return FormatJSON5(jsonRaw)
}

// MarshalVars emits the vars mapping as indented plain JSON, which is
// valid JSON5 and also valid YAML - so a vars-file written here still
// loads even if it ends up named .yml.
func (json5Codec) MarshalVars(v interface{}) ([]byte, error) {
	return json.MarshalIndent(v, "", "  ")
}

// UnmarshalVars reads a vars-file back. json5.Unmarshal is a superset of
// encoding/json, so a plain .json vars-file decodes to the same Go values
// it always has, and a .json5 one carrying comments or a trailing comma
// now decodes too instead of falling through to the YAML parser.
func (json5Codec) UnmarshalVars(data []byte) (map[string]interface{}, error) {
	out := map[string]interface{}{}
	if err := json5.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}
