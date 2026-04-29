package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/dokku/docket/tasks"

	sigil "github.com/gliderlabs/sigil"
	flag "github.com/spf13/pflag"
	yaml "gopkg.in/yaml.v3"
)

type Argument struct {
	Required  bool
	Sensitive bool
	// Type is the declared input type ("string", "int", "float", "bool"). It
	// is normalised to the canonical lowercase form; an empty `type:` field
	// in the recipe stores as "string". Used by SetFromVarsFile to coerce
	// loosely-typed map values from a --vars-file into the same Go type
	// pflag would have produced from the equivalent CLI flag.
	Type        string
	boolValue   *bool
	floatValue  *float64
	intValue    *int
	stringValue *string
}

func (c Argument) GetValue() interface{} {
	if c.boolValue != nil {
		return c.boolValue
	} else if c.intValue != nil {
		return c.intValue
	} else if c.floatValue != nil {
		return c.floatValue
	} else if c.stringValue != nil && *c.stringValue != "" {
		return c.stringValue
	}
	return nil
}

func (c Argument) HasValue() bool {
	return c.GetValue() != nil
}

// StringValue returns the argument's value formatted as the same string sigil
// will substitute into the rendered YAML. Returns "" when no value is set.
// Used to register sensitive input values with the subprocess masker.
func (c Argument) StringValue() string {
	switch v := c.GetValue().(type) {
	case *string:
		if v == nil {
			return ""
		}
		return *v
	case *int:
		if v == nil {
			return ""
		}
		return strconv.Itoa(*v)
	case *float64:
		if v == nil {
			return ""
		}
		return strconv.FormatFloat(*v, 'g', -1, 64)
	case *bool:
		if v == nil {
			return ""
		}
		return strconv.FormatBool(*v)
	}
	return ""
}

func (c *Argument) SetBoolValue(ptr *bool) {
	c.boolValue = ptr
}

func (c *Argument) SetFloatValue(ptr *float64) {
	c.floatValue = ptr
}

func (c *Argument) SetIntValue(ptr *int) {
	c.intValue = ptr
}

func (c *Argument) SetStringValue(ptr *string) {
	c.stringValue = ptr
}

func isTrueString(s string) bool {
	trueStrings := map[string]bool{
		"true": true,
		"yes":  true,
		"on":   true,
		"y":    true,
		"Y":    true,
	}
	return trueStrings[s]
}

func isFalseString(s string) bool {
	falseStrings := map[string]bool{
		"false": true,
		"no":    true,
		"off":   true,
		"n":     true,
		"N":     true,
	}
	return falseStrings[s]
}

func getTaskYamlFilename(s []string) string {
	for i, arg := range s {
		if arg == "--tasks" {
			if len(s) > i+1 {
				return s[i+1]
			}
		}
		if taskFile, found := strings.CutPrefix(arg, "--tasks="); found {
			return taskFile
		}
	}
	return "tasks.yml"
}

func getInputVariables(data []byte) (map[string]*tasks.Input, error) {
	vars := make(map[string]interface{})
	render, err := sigil.Execute(data, vars, "tasks")
	if err != nil {
		return map[string]*tasks.Input{}, fmt.Errorf("sigil error: %v", err.Error())
	}

	out, err := io.ReadAll(&render)
	if err != nil {
		return map[string]*tasks.Input{}, fmt.Errorf("render error: %v", err.Error())
	}

	return parseInputYaml(out)
}

// registerInputFlags reads the task file inputs and registers a flag for each
// dynamic input on the given FlagSet. It returns the Argument map keyed by
// input name so the caller can collect values after flags.Parse.
func registerInputFlags(f *flag.FlagSet, data []byte) (map[string]*Argument, error) {
	arguments := make(map[string]*Argument)
	inputs, err := getInputVariables(data)
	if err != nil {
		return arguments, err
	}

	for _, input := range inputs {
		if input.Name == "tasks" {
			continue
		}
		arg := &Argument{Required: input.Required, Sensitive: input.Sensitive}
		switch input.Type {
		case "string", "":
			arg.Type = "string"
			arg.SetStringValue(f.String(input.Name, input.Default, input.Description))
		case "int":
			arg.Type = "int"
			i, err := strconv.Atoi(input.Default)
			if err != nil {
				return arguments, fmt.Errorf("Error parsing input '%s': %v", input.Name, err.Error())
			}
			arg.SetIntValue(f.Int(input.Name, i, input.Description))
		case "float":
			arg.Type = "float"
			ff, err := strconv.ParseFloat(input.Default, 64)
			if err != nil {
				return arguments, fmt.Errorf("Error parsing input '%s': %v", input.Name, err.Error())
			}
			arg.SetFloatValue(f.Float64(input.Name, ff, input.Description))
		case "bool":
			arg.Type = "bool"
			if isTrueString(input.Default) {
				arg.SetBoolValue(f.Bool(input.Name, true, input.Description))
			} else if isFalseString(input.Default) {
				arg.SetBoolValue(f.Bool(input.Name, false, input.Description))
			} else {
				return arguments, fmt.Errorf("Error parsing input '%s': invalid default value", input.Name)
			}
		default:
			return arguments, fmt.Errorf("Error parsing input '%s': invalid type", input.Name)
		}
		arguments[input.Name] = arg
	}

	return arguments, nil
}

func parseInputYaml(data []byte) (map[string]*tasks.Input, error) {
	inputs := make(map[string]*tasks.Input)
	t := tasks.Recipe{}
	if err := yaml.Unmarshal(data, &t); err != nil {
		return inputs, err
	}

	for _, recipe := range t {
		if len(recipe.Inputs) == 0 {
			continue
		}

		for name := range recipe.Inputs {
			input := recipe.Inputs[name]
			inputs[input.Name] = &input
		}
	}

	return inputs, nil
}

// SetFromVarsFile coerces value to the Argument's declared Type and writes
// it through the underlying typed pointer that registerInputFlags allocated.
// The resulting state is indistinguishable from a CLI flag at the same value
// having been parsed by pflag, so the existing GetValue / HasValue /
// StringValue / Sensitive plumbing keeps working without per-call branching.
//
// Loose typing (YAML decodes "42" as a string when quoted but as int64 when
// bare; JSON always gives float64 for numbers) is normalised here so vars
// files written by hand or generated by another tool both feed the same
// pflag-shaped pointer.
func (c *Argument) SetFromVarsFile(name string, value interface{}) error {
	if value == nil {
		return fmt.Errorf("input %q has nil value in vars file", name)
	}
	switch c.Type {
	case "", "string":
		if c.stringValue == nil {
			return fmt.Errorf("input %q is not a string", name)
		}
		*c.stringValue = stringifyVarsFileValue(value)
		return nil
	case "int":
		if c.intValue == nil {
			return fmt.Errorf("input %q is not an int", name)
		}
		i, err := coerceInt(value)
		if err != nil {
			return fmt.Errorf("input %q: %v", name, err)
		}
		*c.intValue = i
		return nil
	case "float":
		if c.floatValue == nil {
			return fmt.Errorf("input %q is not a float", name)
		}
		f, err := coerceFloat(value)
		if err != nil {
			return fmt.Errorf("input %q: %v", name, err)
		}
		*c.floatValue = f
		return nil
	case "bool":
		if c.boolValue == nil {
			return fmt.Errorf("input %q is not a bool", name)
		}
		b, err := coerceBool(value)
		if err != nil {
			return fmt.Errorf("input %q: %v", name, err)
		}
		*c.boolValue = b
		return nil
	}
	return fmt.Errorf("input %q has unknown type %q", name, c.Type)
}

// stringifyVarsFileValue renders any scalar from a YAML/JSON map into the
// flag-equivalent string form. yaml.v3 round-trips bools and numbers through
// their native Go types, so a `default: "1"` declared as `type: string` gets
// the same rendered value whether it came from CLI (`--key=1`) or vars file
// (`key: 1` or `key: "1"`).
func stringifyVarsFileValue(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	case bool:
		return strconv.FormatBool(v)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case float64:
		// Trim trailing zeros so 3.0 in JSON renders as "3" the same as
		// `--key=3` would; full precision is preserved for non-whole values.
		return strconv.FormatFloat(v, 'g', -1, 64)
	}
	return fmt.Sprintf("%v", value)
}

func coerceInt(value interface{}) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		if v != float64(int(v)) {
			return 0, fmt.Errorf("expected int, got non-whole number %v", v)
		}
		return int(v), nil
	case string:
		i, err := strconv.Atoi(v)
		if err != nil {
			return 0, fmt.Errorf("expected int, got %q", v)
		}
		return i, nil
	case bool:
		return 0, fmt.Errorf("expected int, got bool %v", v)
	}
	return 0, fmt.Errorf("expected int, got %T", value)
}

func coerceFloat(value interface{}) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, fmt.Errorf("expected float, got %q", v)
		}
		return f, nil
	case bool:
		return 0, fmt.Errorf("expected float, got bool %v", v)
	}
	return 0, fmt.Errorf("expected float, got %T", value)
}

func coerceBool(value interface{}) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		if isTrueString(v) {
			return true, nil
		}
		if isFalseString(v) {
			return false, nil
		}
		return false, fmt.Errorf("expected bool, got %q", v)
	}
	return false, fmt.Errorf("expected bool, got %T", value)
}

// loadVarsFiles parses each path left-to-right and returns the merged flat
// map plus a `key -> source path` index so unknown-key errors can name the
// offending file. Later files override earlier files for the same key.
//
// File format is detected by extension: `.json` parses as JSON, anything
// else parses as YAML. The top-level document must be a string-keyed
// mapping; lists, scalars, and non-string keys are rejected.
func loadVarsFiles(paths []string) (map[string]interface{}, map[string]string, error) {
	merged := map[string]interface{}{}
	sources := map[string]string{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, fmt.Errorf("--vars-file %s: %v", path, err)
		}
		one, err := parseVarsFile(path, data)
		if err != nil {
			return nil, nil, err
		}
		for k, v := range one {
			merged[k] = v
			sources[k] = path
		}
	}
	return merged, sources, nil
}

func parseVarsFile(path string, data []byte) (map[string]interface{}, error) {
	out := map[string]interface{}{}
	if strings.EqualFold(filepath.Ext(path), ".json") {
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("--vars-file %s: %v", path, err)
		}
		return out, nil
	}
	// YAML decodes mapping keys as interface{}; convert to string keys and
	// recursively normalise nested maps so JSON-like consumers see the same
	// shape regardless of source format.
	var raw interface{}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("--vars-file %s: %v", path, err)
	}
	if raw == nil {
		return out, nil
	}
	asMap, ok := raw.(map[string]interface{})
	if !ok {
		// yaml.v3 returns map[string]interface{} for the common case but
		// older fixtures sometimes round-trip as map[interface{}]interface{};
		// normalise that path too.
		if generic, ok2 := raw.(map[interface{}]interface{}); ok2 {
			converted, err := normaliseYAMLMap(generic)
			if err != nil {
				return nil, fmt.Errorf("--vars-file %s: %v", path, err)
			}
			return converted, nil
		}
		return nil, fmt.Errorf("--vars-file %s: top-level document must be a mapping of input names to values", path)
	}
	return asMap, nil
}

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

// applyVarsFiles loads each --vars-file path and merges the result into the
// registered Argument set, honouring the precedence rules from #207:
//
//  1. file-level inputs: defaults  (already in flag pointers from registerInputFlags)
//  2. play-level inputs: defaults  (layered per-play in tasks.GetPlays via #208)
//  3. --vars-file values
//  4. --name=value CLI flags       (highest)
//
// flags.Visit is the canonical pflag idiom for "which flags did the user
// type on the command line"; the visitor only fires for flags whose Changed
// bit is set. Any unknown vars-file key is a hard error with a Levenshtein
// suggestion against the registered input names.
//
// The returned map contains the input names this call wrote into the
// argument set from a vars file. Callers union it with flags.Visit to
// derive the full "user has overridden this key" set, which #208 needs so
// per-play input defaults do not shadow user overrides.
func applyVarsFiles(arguments map[string]*Argument, flags *flag.FlagSet, paths []string) (map[string]bool, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	merged, sources, err := loadVarsFiles(paths)
	if err != nil {
		return nil, err
	}

	cliSet := map[string]bool{}
	if flags != nil {
		flags.Visit(func(f *flag.Flag) {
			cliSet[f.Name] = true
		})
	}

	keys := make([]string, 0, len(merged))
	for k := range merged {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	knownNames := make([]string, 0, len(arguments))
	for name := range arguments {
		knownNames = append(knownNames, name)
	}

	applied := map[string]bool{}
	for _, key := range keys {
		arg, ok := arguments[key]
		if !ok {
			suggestion := nearestInputName(key, knownNames)
			hint := ""
			if suggestion != "" {
				hint = fmt.Sprintf("; did you mean %q?", suggestion)
			}
			return nil, fmt.Errorf("unknown input %q in --vars-file %s%s", key, sources[key], hint)
		}
		if cliSet[key] {
			continue
		}
		if err := arg.SetFromVarsFile(key, merged[key]); err != nil {
			return nil, fmt.Errorf("--vars-file %s: %v", sources[key], err)
		}
		applied[key] = true
	}
	return applied, nil
}

// userSetKeys merges the set of input names the user has overridden via
// --vars-file (varsFileKeys) with those they have typed on the CLI
// (flags.Visit). Used by #208 so per-play input defaults do not shadow a
// user override.
func userSetKeys(flags *flag.FlagSet, varsFileKeys map[string]bool, arguments map[string]*Argument) map[string]bool {
	out := make(map[string]bool, len(varsFileKeys))
	for k := range varsFileKeys {
		out[k] = true
	}
	if flags != nil {
		flags.Visit(func(f *flag.Flag) {
			if _, ok := arguments[f.Name]; ok {
				out[f.Name] = true
			}
		})
	}
	return out
}

// nearestInputName returns the registered input name with the lowest
// Levenshtein distance to candidate, but only if that distance is at most
// 2. Empty string means "no useful suggestion". Mirrors the behaviour of
// tasks.nearestTaskName so unknown-input messages stay consistent across
// the validator and the input loader.
func nearestInputName(candidate string, names []string) string {
	best := ""
	bestDist := 3
	for _, name := range names {
		d := editDistance(candidate, name)
		if d < bestDist {
			bestDist = d
			best = name
		}
	}
	if bestDist <= 2 {
		return best
	}
	return ""
}

// editDistance is a small ASCII Levenshtein implementation. Input names are
// short and the candidate set is bounded by the recipe size, so a 2D
// allocation per lookup is fine.
func editDistance(a, b string) int {
	if a == b {
		return 0
	}
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = minOf3(prev[j]+1, curr[j-1]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[lb]
}

func minOf3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
