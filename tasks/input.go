package tasks

import "strconv"

// InputTypeNames lists the `type:` values an input may declare, in the
// canonical order diagnostics spell them. An input that declares no `type:` is
// a string.
var InputTypeNames = []string{"bool", "float", "int", "string"}

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
// `default:` or a --vars-file scalar - into a bool. ok is false for any other
// spelling, the empty string included: a caller that treats an omitted default
// as the type's zero value checks for that before asking.
//
// These are docket's own spellings, not pflag's. A `--name=value` flag typed on
// the command line is parsed by pflag, which takes true/false/1/0 but not
// yes/on; see docs/inputs.md.
func ParseInputBool(s string) (bool, bool) {
	switch s {
	case "true", "yes", "on", "y", "Y":
		return true, true
	case "false", "no", "off", "n", "N":
		return false, true
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
