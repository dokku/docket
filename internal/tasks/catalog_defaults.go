package tasks

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// defaultListPattern is go-defaults' own list syntax: the elements between
// one pair of brackets, split on commas.
var defaultListPattern = regexp.MustCompile(`^\[(.*)\]$`)

// ParseFieldDefault turns a field's raw `default:` tag text into the typed
// value the catalog publishes as FieldSchema.Default: a bool, an int64, a
// float64, a string, or a []any of those, matching fieldType. fieldType and
// itemType are the catalog's type names - a FieldSchema's Type and its
// Item.Type - so a caller holding either a task struct or a published catalog
// can call it. itemType is read only when fieldType is TypeList. An empty raw
// tag means the field has no default, and returns nil.
//
// It follows go-defaults' parsing rules, which are what the loader applies
// through defaults.SetDefaults: strconv for scalars, the string as-is, and a
// list spelled `[a,b]` and split on commas with no trimming. Where go-defaults
// would silently store a zero value - an unparsable bool or number, a list tag
// without brackets, a default on a dict or on a list of objects, which it never
// applies - this returns an error instead, so a typo in a tag cannot publish a
// default the task does not have.
//
// A pointer field is described by the type it points to, so a *bool's tag
// parses as a bool. go-defaults leaves a pointer nil, so on a pointer field the
// value is what the task coerces nil to, not what the loader writes into the
// struct.
//
//sdk:export
func ParseFieldDefault(fieldType, itemType, raw string) (any, error) {
	if raw == "" {
		return nil, nil
	}
	if fieldType == TypeList {
		match := defaultListPattern.FindStringSubmatch(raw)
		if match == nil {
			return nil, fmt.Errorf("default %q for a list must be written [a,b]", raw)
		}
		items := []any{}
		if match[1] == "" {
			return items, nil
		}
		for _, element := range strings.Split(match[1], ",") {
			value, err := parseScalarDefault(itemType, element)
			if err != nil {
				return nil, fmt.Errorf("default %q: element %w", raw, err)
			}
			items = append(items, value)
		}
		return items, nil
	}
	return parseScalarDefault(fieldType, raw)
}

// parseScalarDefault parses one scalar default, a field's or a list element's.
func parseScalarDefault(typ, raw string) (any, error) {
	switch typ {
	case TypeString:
		return raw, nil
	case TypeBool:
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("%q is not a valid %s", raw, typ)
		}
		return value, nil
	case TypeInt:
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a valid %s", raw, typ)
		}
		return value, nil
	case TypeFloat:
		value, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("%q is not a valid %s", raw, typ)
		}
		return value, nil
	default:
		return nil, fmt.Errorf("%q cannot be a default: go-defaults applies none to type %s", raw, typ)
	}
}
