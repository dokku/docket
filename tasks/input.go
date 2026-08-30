package tasks

import (
	"strconv"
	"strings"
)

// InputTypeNames lists the `type:` values an input may declare, in the
// canonical order diagnostics spell them. An input that declares no `type:` is
// a string.
var InputTypeNames = []string{"bool", "float", "int", "string"}

// InputBoolTrueSpellings and InputBoolFalseSpellings are the spellings a bool
// input value may be written in, in the order diagnostics list them. They are
// the union of docket's own vocabulary and pflag's, so the set is a superset of
// what `strconv.ParseBool` takes: every spelling that works on the command line
// also works as a `default:` or a --vars-file value (#495).
var (
	InputBoolTrueSpellings  = []string{"true", "yes", "on", "y", "t", "1"}
	InputBoolFalseSpellings = []string{"false", "no", "off", "n", "f", "0"}
)

// CanonicalInputType maps a declared `type:` onto its canonical spelling,
// resolving the empty type to "string". ok is false for a type docket does not
// implement, which the validator reports as invalid_input_type.
func CanonicalInputType(t string) (string, bool) {
	switch t {
	case "":
		return "string", true
	case "bool", "float", "int", "string":
		return t, true
	}
	return "", false
}

// ParseInputBool parses the string spelling of a bool input value - a recipe
// `default:` or a --vars-file scalar - into a bool. Matching is
// case-insensitive, so `True` and `TRUE` read the same as `true`. ok is false
// for any other spelling, the empty string included: a caller that treats an
// omitted default as the type's zero value checks for that before asking.
//
// The table is a superset of the one pflag parses a `--name=value` flag with,
// which is what keeps the two from disagreeing about the same recipe. It is not
// a subset: `yes` and `on` are docket's alone, so they remain unusable on the
// command line. See docs/inputs.md.
func ParseInputBool(s string) (bool, bool) {
	lowered := strings.ToLower(s)
	for _, spelling := range InputBoolTrueSpellings {
		if lowered == spelling {
			return true, true
		}
	}
	for _, spelling := range InputBoolFalseSpellings {
		if lowered == spelling {
			return false, true
		}
	}
	return false, false
}

// ParseInputInt parses the string spelling of an int input value. ok is false
// when the spelling is not a whole number, the empty string included.
func ParseInputInt(s string) (int, bool) {
	v, err := strconv.Atoi(s)
	return v, err == nil
}

// ParseInputFloat parses the string spelling of a float input value. ok is
// false when the spelling is not a number, the empty string included.
func ParseInputFloat(s string) (float64, bool) {
	v, err := strconv.ParseFloat(s, 64)
	return v, err == nil
}

// inputDefaultValue resolves a declared `default:` to the value the input
// renders as, in the same Go shape commands.registerInputFlags hands the flag
// set: a typed pointer for bool / int / float, so a `default: on` renders as
// `true` rather than as the text `on`, and both halves of a recipe's input
// context hold the same thing (#495).
//
// The fallback is the raw text, not the type's zero value: a default that does
// not parse, or a `type:` docket does not implement, is reported as
// invalid_input_default / invalid_input_type, and `validate` collects its
// problems and keeps rendering. Substituting a zero there would hide the text
// an unsafe_input_value diagnostic needs to see.
//
// Each call allocates its own pointer, so two plays declaring the same input
// name never share one.
func inputDefaultValue(typ, def string) interface{} {
	canonical, ok := CanonicalInputType(typ)
	if !ok {
		return def
	}
	switch canonical {
	case "bool":
		if v, parsed := ParseInputBool(def); parsed {
			return &v
		}
	case "int":
		if v, parsed := ParseInputInt(def); parsed {
			return &v
		}
	case "float":
		if v, parsed := ParseInputFloat(def); parsed {
			return &v
		}
	}
	return def
}
