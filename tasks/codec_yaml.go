package tasks

import (
	"fmt"

	yaml "gopkg.in/yaml.v3"
)

// FormatYAML is the canonical name of the YAML codec.
const FormatYAML = "yaml"

// yamlCodec reads and writes recipes as YAML, docket's original and
// default surface syntax. It is codecs[0], which makes it the format an
// empty or unrecognised format string resolves to.
type yamlCodec struct{}

func (yamlCodec) Name() string { return FormatYAML }

func (yamlCodec) Aliases() []string { return []string{"yml"} }

func (yamlCodec) Extensions() []string { return []string{"yml", "yaml"} }

// Sniff never claims bytes. YAML is the default codec, so it is what
// SniffCodec falls back to; claiming here as well would mean no other
// codec is ever reached.
func (yamlCodec) Sniff([]byte) bool { return false }

// ToYAML is the identity: the rest of the pipeline already reads YAML.
func (yamlCodec) ToYAML(data []byte) ([]byte, *Problem) { return data, nil }

// Lint has nothing to add. A duplicate YAML key is rejected by yaml.v3
// itself during the parse that follows normalisation.
func (yamlCodec) Lint([]byte) []Problem { return nil }

func (yamlCodec) Format(data []byte) ([]byte, error) { return Format(data) }

// Marshal renders v as YAML and then canonicalises it, so an exported
// recipe comes out byte-identical to one `docket fmt` has been run over.
func (yamlCodec) Marshal(v interface{}) ([]byte, error) {
	raw, err := yaml.Marshal(v)
	if err != nil {
		return nil, err
	}
	return Format(raw)
}

// MarshalVars emits the vars mapping as plain YAML. Unlike Marshal it gets
// no canonical-form pass: a vars-file is a flat mapping of input name to
// value, not a recipe, so the play/envelope key ordering has nothing to
// say about it.
func (yamlCodec) MarshalVars(v interface{}) ([]byte, error) { return yaml.Marshal(v) }

// UnmarshalVars reads a vars-file back into a string-keyed map.
//
// yaml.v3 decodes mapping keys as interface{}, so the result is converted
// to string keys and nested maps normalised, giving JSON-like consumers
// the same shape regardless of which codec produced the file. Errors come
// back unwrapped; the caller names the path.
func (yamlCodec) UnmarshalVars(data []byte) (map[string]interface{}, error) {
	out := map[string]interface{}{}
	var raw interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		return out, nil
	}
	if asMap, ok := raw.(map[string]interface{}); ok {
		return asMap, nil
	}
	// yaml.v3 returns map[string]interface{} for the common case but
	// older fixtures sometimes round-trip as map[interface{}]interface{};
	// normalise that path too.
	if generic, ok := raw.(map[interface{}]interface{}); ok {
		return normaliseYAMLMap(generic)
	}
	return nil, fmt.Errorf("top-level document must be a mapping of input names to values")
}

// normaliseYAMLMap converts an interface-keyed YAML mapping to a
// string-keyed one, rejecting a key that is not a string.
func normaliseYAMLMap(in map[interface{}]interface{}) (map[string]interface{}, error) {
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		ks, ok := k.(string)
		if !ok {
			return nil, fmt.Errorf("non-string key %v", k)
		}
		out[ks] = v
	}
	return out, nil
}
