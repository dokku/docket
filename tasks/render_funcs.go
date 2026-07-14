package tasks

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"text/template"

	sigil "github.com/gliderlabs/sigil"
	yaml "gopkg.in/yaml.v3"
)

// render_funcs.go registers the `dq` template filter with sigil so a recipe can
// interpolate an input value that contains YAML-special characters without
// breaking the surrounding scalar. Because docket renders the whole file as
// text before parsing it (see renderRecipeBytes), an input value like `ab"cd`
// substituted into `app: "{{ .app }}"` produces the invalid `app: "ab"cd"`.
//
// `dq` escapes the value for a double-quoted scalar and is used INSIDE the
// existing double quotes, so the value stays safe even alongside other text:
//
//	app:     "{{ .app | dq }}"                 // -> app: "ab\"cd"
//	domains: ["{{ .app | dq }}.example.com"]   // -> ["ab\"cd.example.com"]
//
// Keeping the quotes means the raw recipe is still valid YAML/JSON5, so
// `docket validate` and `docket fmt` (which parse the file before it is
// rendered) keep working - unlike a self-quoting unquoted filter would.
//
// The name differs from sigil's `tojson` built-in, which emits a value WITH its
// surrounding quotes and so cannot be used inside an existing quoted scalar.
func init() {
	sigil.Register(template.FuncMap{
		"dq": DoubleQuoteEscape,
	})
}

// DoubleQuoteEscape escapes v for interpolation inside a double-quoted YAML (or
// JSON5) scalar, returning the escaped body WITHOUT the surrounding quotes so
// the caller keeps its own. JSON string escaping is a subset of YAML's
// double-quoted escaping, so json.Marshal produces a body that both parsers
// accept; HTML escaping is disabled so `<`, `>`, and `&` stay readable.
func DoubleQuoteEscape(v interface{}) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(scalarArg(v)); err != nil {
		return "", err
	}
	s := strings.TrimRight(buf.String(), "\n")
	return strings.TrimSuffix(strings.TrimPrefix(s, `"`), `"`), nil
}

// YAMLScalar renders s as a complete YAML scalar, quoting and escaping only
// when necessary so ordinary values stay unquoted. Used by commands/init.go to
// emit an input's `default:` in the scaffold; it is not a recipe-render filter
// because its output carries its own quoting and so cannot sit inside an
// existing quoted scalar.
func YAMLScalar(v interface{}) (string, error) {
	b, err := yaml.Marshal(scalarArg(v))
	if err != nil {
		return "", err
	}
	return strings.TrimRight(string(b), "\n"), nil
}

// JSONScalar renders s as a JSON string literal (also valid JSON5), used by the
// JSON5 init scaffold so an embedded quote or backslash is escaped.
func JSONScalar(v interface{}) (string, error) {
	b, err := json.Marshal(scalarArg(v))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// scalarArg coerces a template argument to a string.
//
// A missing template key arrives as nil (text/template's zero value), which
// must render as an empty string rather than error so a filter survives the
// empty-context render used for input discovery and the validate syntax check.
//
// The apply/plan context stores input values as typed pointers (*string, *int,
// ...) because pflag hands back pointers. text/template auto-dereferences a
// pointer for a bare `{{ .x }}`, but passes it verbatim to a filter argument
// (`{{ .x | dq }}`), so the pointer must be dereferenced here or the address,
// not the value, would be escaped.
func scalarArg(v interface{}) string {
	if v == nil {
		return ""
	}
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return ""
		}
		rv = rv.Elem()
	}
	v = rv.Interface()
	switch t := v.(type) {
	case string:
		return t
	case fmt.Stringer:
		return t.String()
	default:
		return fmt.Sprintf("%v", t)
	}
}
