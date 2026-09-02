package commands

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/dokku/docket/subprocess"
	"github.com/dokku/docket/tasks"

	flag "github.com/spf13/pflag"
	yaml "gopkg.in/yaml.v3"
)

type Argument struct {
	Required  bool
	Sensitive bool
	// HasDefault records whether the recipe declared a non-empty `default:`
	// for this input. It is the half of "the value was actually supplied"
	// that the flag pointer cannot answer: a bool / int / float pointer is
	// never nil, so GetValue() cannot tell an implicit zero apart from a
	// declared or user-typed one. Paired with userSetKeys it drives both the
	// required-input check and the decision to register a sensitive value.
	HasDefault bool
	// Type is the declared input type ("string", "int", "float", "bool"). It
	// is normalised to the canonical lowercase form; an empty `type:` field
	// in the recipe stores as "string", and so does a type docket does not
	// implement, which the loader rejects as invalid_input_type before the
	// value is ever read. Used by SetFromVarsFile to coerce loosely-typed map
	// values from a --vars-file into the same Go type pflag would have
	// produced from the equivalent CLI flag.
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

// ContextValue returns the value the recipe sees for this input: the concrete
// Go value rather than the flag pointer GetValue reports.
//
// The pointer is pflag's, not something the recipe asked for, and putting it in
// the render / predicate context leaked it into two evaluators that each read a
// pointer as "true" (#497). expr dereferences the operands of an operator but
// not a program that is nothing but an identifier, so `when: debug` tested the
// pointer - never nil for a bool / int / float - instead of the value.
// text/template's own isTrue counts any non-nil pointer as true, so
// `{{ if .debug }}` in a recipe body did the same.
//
// It differs from GetValue in one place, on purpose: an empty string is a
// value, not nil, so an input left at its zero value renders as the empty
// string rather than as text/template's `<no value>`. GetValue keeps the nil
// because HasValue, IsSatisfied, and StringValue read it as "nothing was
// supplied".
func (c Argument) ContextValue() interface{} {
	switch {
	case c.boolValue != nil:
		return *c.boolValue
	case c.intValue != nil:
		return *c.intValue
	case c.floatValue != nil:
		return *c.floatValue
	case c.stringValue != nil:
		return *c.stringValue
	}
	return nil
}

// IsSatisfied reports whether this input has a non-empty value that the recipe
// or the operator actually wrote. It is the test behind `required: true`, and
// it needs both halves:
//
// HasValue() alone cannot answer it, because pflag's pointer for a bool / int /
// float is never nil - every non-string input looked satisfied, so `required:`
// was enforced for strings only (#493). "The user typed the flag" alone cannot
// answer it either, because `--app=` types the flag and supplies nothing; the
// input would resolve to an empty string and render an empty app name.
//
// userSet is whether the operator supplied a value for this input, on the
// command line or through a --vars-file; see userSetKeys.
func (c Argument) IsSatisfied(userSet bool) bool {
	if !c.HasDefault && !userSet {
		return false
	}
	return c.StringValue() != ""
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

func getTaskYamlFilename(baseDir string, s []string) string {
	path, _ := resolveTaskFileFromArgs(baseDir, s)
	return path
}

// resolveTaskFileFromArgs walks os.Args-style argv, finds a --tasks
// value, and returns it together with its detected format. When --tasks
// is not present the function probes defaultTaskFileCandidates in order
// and returns the first one that exists; if none exist the legacy
// default ("tasks.yml") is returned so the downstream os.ReadFile error
// path still fires with the familiar message. Format is keyed by file
// extension; see detectTaskFileFormat. The format is empty for stdin,
// which has no name - taskFileFormatFor sniffs the bytes instead.
func resolveTaskFileFromArgs(baseDir string, s []string) (string, string) {
	positional := ""
	skipNext := false
	for i, arg := range s {
		if arg == "--tasks" {
			if len(s) > i+1 {
				path := s[i+1]
				if path == taskFileStdin {
					return taskFileStdin, ""
				}
				return path, detectTaskFileFormat(path)
			}
		}
		if taskFile, found := strings.CutPrefix(arg, "--tasks="); found {
			if taskFile == taskFileStdin {
				return taskFileStdin, ""
			}
			return taskFile, detectTaskFileFormat(taskFile)
		}
		// Best-effort positional detection so recipe input flags still
		// preregister when the file is given as `docket validate x.yml`
		// (the authoritative selection is flags.Args() in Run). A token
		// counts only if it carries a task-file extension and is not the
		// value of a preceding value-taking flag, so `--vars-file prod.yml`
		// never wins.
		if skipNext {
			skipNext = false
			continue
		}
		// A bare "-" is the stdin recipe. It has to be tested after the
		// skipNext check, so `--play -` cannot be mistaken for the
		// recipe, and before the HasPrefix check below, which would
		// otherwise swallow it as a flag. It also needs its own branch
		// because hasTaskFileExtension("-") is false.
		if arg == taskFileStdin {
			if positional == "" {
				positional = taskFileStdin
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			if valueTakingFlags[arg] {
				skipNext = true
			}
			continue
		}
		if positional == "" && hasTaskFileExtension(arg) {
			positional = arg
		}
	}
	// stdin must short-circuit before the os.Stat below: it never
	// stats, and falling through would silently resolve to ./tasks.yml
	// while Run read the recipe the user actually piped in.
	if positional == taskFileStdin {
		return taskFileStdin, ""
	}
	if positional != "" {
		if _, err := os.Stat(inDir(baseDir, positional)); err == nil {
			return positional, detectTaskFileFormat(positional)
		}
	}
	// The other candidates the probe found, and any stat error, are both
	// dropped here. This runs from preloadRecipeForFlags before pflag has
	// parsed anything - more than once per invocation on the help and
	// flag-error paths - so an ambiguity warning would print two or three
	// times before the command started. Run warns once, off recipeSource.
	// A stat error likewise belongs to Run, which re-resolves the recipe
	// and reports it properly.
	chosen, _, _ := probeDefaultTaskFile(baseDir)
	if chosen != "" {
		return chosen, detectTaskFileFormat(chosen)
	}
	return defaultTaskFileCandidates[0], detectTaskFileFormat(defaultTaskFileCandidates[0])
}

// tasksFormatFromArgs pulls a --tasks-format value out of os.Args-style
// argv. Needed because FlagSet() runs before pflag parses, so the
// preregistration pass cannot read the parsed flag. An empty result
// means "not given"; the value is not validated here, only in Run.
func tasksFormatFromArgs(s []string) string {
	for i, arg := range s {
		if arg == "--tasks-format" && len(s) > i+1 {
			return s[i+1]
		}
		if value, found := strings.CutPrefix(arg, "--tasks-format="); found {
			return value
		}
	}
	return ""
}

// valueTakingFlags are the built-in flags that consume the following argv
// token as their value, used by resolveTaskFileFromArgs so a flag value
// with a task-file extension is not mistaken for a positional recipe path.
var valueTakingFlags = map[string]bool{
	"--tasks":         true,
	"--tasks-format":  true,
	"--vars-file":     true,
	"--host":          true,
	"--tags":          true,
	"--skip-tags":     true,
	"--play":          true,
	"--start-at-task": true,
	"--output":        true,
	"--color":         true,
}

func getInputVariables(data []byte, format string) (map[string]*tasks.Input, error) {
	vars := make(map[string]interface{})
	render, err := tasks.RenderTemplate(data, vars, "tasks")
	if err != nil {
		return map[string]*tasks.Input{}, fmt.Errorf("sigil error: %v", err.Error())
	}

	out, err := io.ReadAll(&render)
	if err != nil {
		return map[string]*tasks.Input{}, fmt.Errorf("render error: %v", err.Error())
	}

	return parseInputDocument(out, format)
}

// registerInputFlags reads the task file inputs and registers a flag for each
// dynamic input on the given FlagSet. It returns the Argument map keyed by
// input name so the caller can collect values after flags.Parse. format is
// "yaml" or "json5"; the empty string is treated as YAML.
//
// Every declared input gets a flag, whatever it declares. A malformed input is
// never allowed to cost its neighbours their flags, because the caller cannot
// report the failure - FlagSet() has no error return, and the whole input
// surface silently vanishing is far worse than one input resolving to a zero
// value it is about to be rejected for anyway (#493). The declaration itself is
// judged by checkInputDeclarations, which runs in the loader and the validator.
//
// The one error still returned is a recipe that could not be rendered or
// parsed at all, in which case there are no inputs to register.
func registerInputFlags(f *flag.FlagSet, data []byte, format string) (map[string]*Argument, error) {
	arguments := make(map[string]*Argument)
	inputs, err := getInputVariables(data, format)
	if err != nil {
		return arguments, err
	}

	for _, input := range inputs {
		// Skip any input whose name collides with a built-in flag. pflag
		// panics with "flag redefined" if we register it, so the loader /
		// validator reject it as reserved_input_name instead (#302); here
		// we just avoid the panic and let the input fall back to its
		// default. validate is the gate that surfaces the error.
		if tasks.ReservedInputNames[input.Name] {
			continue
		}
		arg := &Argument{Required: input.Required, Sensitive: input.Sensitive, HasDefault: input.Default != ""}
		// An omitted `default:` is the zero value for the type, the way the
		// inputs table has always documented it. A malformed one, or a type
		// docket does not implement, still registers a flag - at the zero
		// value, or as a string - because the recipe is rejected by the
		// invalid_input_default / invalid_input_type diagnostic before any of
		// it runs. Registering anyway is what keeps `--name=value` parseable,
		// so the operator reads that diagnostic instead of "unknown flag" (and
		// keeps one bad input from costing every other input its flag, #493).
		typ, _ := tasks.CanonicalInputType(input.Type)
		switch typ {
		case "int":
			arg.Type = "int"
			i, _ := tasks.ParseInputInt(input.Default)
			arg.SetIntValue(f.Int(input.Name, i, input.Description))
		case "float":
			arg.Type = "float"
			ff, _ := tasks.ParseInputFloat(input.Default)
			arg.SetFloatValue(f.Float64(input.Name, ff, input.Description))
		case "bool":
			arg.Type = "bool"
			b, _ := tasks.ParseInputBool(input.Default)
			arg.SetBoolValue(f.Bool(input.Name, b, input.Description))
		default:
			arg.Type = "string"
			arg.SetStringValue(f.String(input.Name, input.Default, input.Description))
		}
		if input.Sensitive {
			maskFlagDefault(f, input.Name, input.Default)
		}
		arguments[input.Name] = arg
	}

	return arguments, nil
}

// maskFlagDefault replaces the usage-string default of a `sensitive: true`
// input's flag with the mask placeholder. pflag renders `(default <DefValue>)`
// straight from this field, and `--help` is rendered before the recipe is
// parsed - the mask registry is still empty at that point, so no MaskString at
// the print site could have caught it (#490).
//
// Only the display copy changes. The flag's Value still holds the real default,
// so the input still resolves to it, and the run that follows still registers
// and masks it like any other sensitive value. An input that declared no
// default has nothing to hide, and is the shape `docket export` generates.
func maskFlagDefault(f *flag.FlagSet, name, def string) {
	if def == "" {
		return
	}
	if fl := f.Lookup(name); fl != nil {
		fl.DefValue = subprocess.MaskPlaceholder
	}
}

// parseInputDocument decodes data as a Recipe in the given on-disk
// format and returns the merged input map keyed by input name. format
// is "yaml" or "json5"; empty / unknown values default to YAML.
func parseInputDocument(data []byte, format string) (map[string]*tasks.Input, error) {
	inputs := make(map[string]*tasks.Input)
	t, err := tasks.UnmarshalRecipe(data, format)
	if err != nil {
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

// parseInputYaml is the YAML-only back-compat wrapper kept so existing
// callers (and the unit tests under apply_args_test.go) do not need to
// learn the format dispatch.
func parseInputYaml(data []byte) (map[string]*tasks.Input, error) {
	return parseInputDocument(data, tasks.FormatYAML)
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

// coerceBool reads a --vars-file value for a `type: bool` input. Strings go
// through tasks.ParseInputBool so the vars file, a recipe `default:`, and the
// invalid_input_default diagnostic all agree on which spellings are a bool.
//
// A native number is a bool only when it is exactly 1 or 0 - the spelling pflag
// takes for `--debug=1`, which a hand-written or generated vars file has no
// reason to spell differently (#495). Any other number is an error rather than
// C-style truthiness. The reverse does not hold: coerceInt and coerceFloat
// reject a native bool, the same way pflag refuses `--replicas=true`.
func coerceBool(value interface{}) (bool, error) {
	switch v := value.(type) {
	case bool:
		return v, nil
	case string:
		if b, ok := tasks.ParseInputBool(v); ok {
			return b, nil
		}
		return false, fmt.Errorf("expected bool, got %q", v)
	case int:
		return coerceBoolNumber(float64(v), v)
	case int64:
		return coerceBoolNumber(float64(v), v)
	case float64:
		return coerceBoolNumber(v, v)
	}
	return false, fmt.Errorf("expected bool, got %T", value)
}

// coerceBoolNumber maps the numbers pflag spells a bool with onto one, naming
// the value rather than its Go type in the error a number outside that pair
// raises: "expected bool, got 2" is actionable where "got int" is not.
func coerceBoolNumber(n float64, original interface{}) (bool, error) {
	switch n {
	case 1:
		return true, nil
	case 0:
		return false, nil
	}
	return false, fmt.Errorf("expected bool, got %v", original)
}

// loadVarsFiles parses each path left-to-right and returns three views of the
// result: the merged flat map, a `key -> source path` index so unknown-key
// errors can name the offending file, and a `path -> declared keys` index.
// Later files override earlier files for the same key, so the source index is
// last-writer-wins - right for naming one file in an error, wrong for asking
// which files a key lives in. varsFileWarnings needs the latter: a base file
// whose sensitive key a later file overrides still holds that secret on disk
// (#489).
//
// File format is detected by extension: `.json` parses as JSON, anything
// else parses as YAML. The top-level document must be a string-keyed
// mapping; lists, scalars, and non-string keys are rejected.
func loadVarsFiles(paths []string) (map[string]interface{}, map[string]string, map[string][]string, error) {
	merged := map[string]interface{}{}
	sources := map[string]string{}
	perFile := map[string][]string{}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("--vars-file %s: %v", path, err)
		}
		one, err := parseVarsFile(path, data)
		if err != nil {
			return nil, nil, nil, err
		}
		for k, v := range one {
			merged[k] = v
			sources[k] = path
			perFile[path] = append(perFile[path], k)
		}
	}
	return merged, sources, perFile, nil
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
// The first returned map contains the input names this call wrote into the
// argument set from a vars file. Callers union it with flags.Visit to
// derive the full "user has overridden this key" set, which #208 needs so
// per-play input defaults do not shadow user overrides. The second return is
// the warnings from varsFileWarnings, which the caller prints; they are
// warnings rather than a second error because the run is fine, the file's
// mode is not.
func applyVarsFiles(arguments map[string]*Argument, flags *flag.FlagSet, paths []string) (map[string]bool, []string, error) {
	if len(paths) == 0 {
		return nil, nil, nil
	}
	merged, sources, perFile, err := loadVarsFiles(paths)
	if err != nil {
		return nil, nil, err
	}
	warnings := varsFileWarnings(paths, perFile, arguments)

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
			return nil, nil, fmt.Errorf("unknown input %q in --vars-file %s%s", key, sources[key], hint)
		}
		if cliSet[key] {
			continue
		}
		if err := arg.SetFromVarsFile(key, merged[key]); err != nil {
			return nil, nil, fmt.Errorf("--vars-file %s: %v", sources[key], err)
		}
		applied[key] = true
	}
	return applied, warnings, nil
}

// varsFileWarnings reports each --vars-file that carries a value for an input
// the recipe declares sensitive and is readable by users other than its owner.
//
// The trigger is content, not mode alone. A vars file of ordinary settings -
// an app name, a replica count - says nothing about secrets, and warning
// about its mode would be noise on the ordinary per-environment file
// docs/inputs.md recommends. A `sensitive: true` input is what makes a vars
// file a secrets file, and an exported one is made of nothing else. This is
// the reading end of #489: docket export writes its own half of the pair at
// varsFileMode, but a file that arrived from somewhere docket did not control
// can be anything.
//
// It stays a warning. The file is the user's, docket only reads it, and a
// recipe that applies cleanly should not fail over the mode of an input file.
func varsFileWarnings(paths []string, perFile map[string][]string, arguments map[string]*Argument) []string {
	// Windows has no mode bits to read: os.FileMode.Perm synthesises 0666 or
	// 0444 from the readonly attribute, so every file there would warn.
	if runtime.GOOS == "windows" {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	for _, path := range paths {
		if seen[path] {
			continue
		}
		seen[path] = true
		if !holdsSensitiveInput(perFile[path], arguments) {
			continue
		}
		info, err := os.Stat(path)
		// A read error is not this function's to report - loadVarsFiles has
		// already read the file successfully - and a fifo, a device, or a
		// process substitution has no mode worth complaining about.
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		perm := info.Mode().Perm()
		if perm&0o077 == 0 {
			continue
		}
		out = append(out, fmt.Sprintf(
			"warning: --vars-file %s holds sensitive input values and is readable by other users (mode %04o); chmod 600 %s",
			path, perm, shellQuotePath(path)))
	}
	return out
}

// holdsSensitiveInput reports whether any of keys names an input the recipe
// declared sensitive. A key with no registered Argument is on its way to the
// unknown-input error and is not this check's business.
func holdsSensitiveInput(keys []string, arguments map[string]*Argument) bool {
	for _, key := range keys {
		if arg, ok := arguments[key]; ok && arg.Sensitive {
			return true
		}
	}
	return false
}

// buildInputContext turns the registered arguments into the sigil render
// context and collects the sensitive values worth handing to the masker.
//
// An input declared `required: true` has to be satisfied - see IsSatisfied for
// what that means and why neither half of the test is sufficient alone.
// validateStrictInputs applies the same rule offline for `validate --strict`.
//
// Names are walked in sorted order so a recipe missing more than one required
// input names the same one on every run.
//
// The context holds ContextValue, not GetValue: the flag pointer must not reach
// the render or the predicates, which both read one as true whatever it points
// at (#497).
//
// A sensitive value is registered only when the recipe declared it or the user
// supplied it. Registering an implicit zero would hand the masker "0" or
// "false" and blank out every unrelated occurrence of that substring. A
// declared one still registers whatever the input resolved to, so a
// `sensitive: true, type: bool, default: 1` hands it the literal "true" - the
// documented reason to keep `sensitive:` for string inputs holding real
// secrets.
func buildInputContext(arguments map[string]*Argument, userSet map[string]bool) (map[string]interface{}, []string, error) {
	names := make([]string, 0, len(arguments))
	for name := range arguments {
		names = append(names, name)
	}
	sort.Strings(names)

	context := make(map[string]interface{}, len(arguments))
	var sensitiveValues []string
	for _, name := range names {
		argument := arguments[name]
		if argument.Required && !argument.IsSatisfied(userSet[name]) {
			return nil, nil, fmt.Errorf("Missing flag '--%s'", name)
		}
		context[name] = argument.ContextValue()
		if argument.Sensitive && (argument.HasDefault || userSet[name]) {
			if v := argument.StringValue(); v != "" {
				sensitiveValues = append(sensitiveValues, v)
			}
		}
	}
	return context, sensitiveValues, nil
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

// nearestInputName returns the name from names with the lowest Levenshtein
// distance to candidate, but only if that distance is at most 2. Empty string
// means "no useful suggestion". Mirrors the behaviour of
// tasks.nearestEnvelopeOrTaskKey so did-you-mean messages stay consistent
// across the validator, the input loader, and `schema --task`.
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
