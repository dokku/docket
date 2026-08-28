package tasks

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
)

// Identity declares what a task addresses on the server.
//
// Every task knows implicitly which resource it manages - dokku_config by
// `app`, a property task by `app`-or-`global` plus `property`,
// dokku_service_property by `service` plus `name` plus `property` - but before
// #427 that knowledge lived only in the shape of each task's `required:"true"`
// fields. It is declared here with two field tags:
//
//	identity:"key"         the field is part of what selects the resource
//	identity:"collection"  the field is the set of items the task manages
//	                       wholesale, and is *not* part of the key
//
// The tags sit on the field rather than in a method or a central table so a
// field rename cannot silently orphan the declaration; TestIdentityTagsResolve
// fails when one does.
//
// Three rules decide what is a key, and none of them is "the required fields":
//
//   - The desired state is never a key. `state:` says what you want done to the
//     resource, not which resource it is, so editing it must not rename a task
//     or change what `export --resource` selects.
//   - A required field can be a payload rather than an address.
//     dokku_git_from_image's `image` is `required:"true" sensitive:"true"`; the
//     task keys on `app`, because an app has one deploy source.
//   - A key need not be required. dokku_certs and dokku_domains have no
//     `required:"true"` field at all - they key on the mutually exclusive
//     `app` / `global` pair.
//
// For the set-valued tasks the issue singles out (dokku_domains, dokku_ports,
// dokku_acl_app), the answer this encodes is that the collection *is* the
// resource, not part of its key: dokku_domains addresses "the domain list of
// this app", so it keys on `app`-or-`global` alone. Where the items themselves
// have identity, it is declared on the element struct
// (PortMapping.Scheme + .Host, HttpAuthUser.Username) instead of being left in
// a comment.
const (
	identityTag           = "identity"
	identityTagKey        = "key"
	identityTagCollection = "collection"
)

// ItemIdentity classifies what identifies one item inside a collection field.
type ItemIdentity string

const (
	// ItemIdentityValue means the element value is the item's identity, as in
	// a `[]string` of domain names.
	ItemIdentityValue ItemIdentity = "value"

	// ItemIdentityMapKey means the map key is the item's identity, as in a
	// `map[string]string` of config pairs.
	ItemIdentityMapKey ItemIdentity = "map_key"

	// ItemIdentityFields means the element is a struct whose own
	// `identity:"key"` fields identify the item, as in PortMapping.
	ItemIdentityFields ItemIdentity = "fields"
)

// IdentityKey is one declared key field, with the value it carries on the
// task instance it was read from. Value is empty and Present is false when
// read from a registry prototype, which is what the docs generator wants.
type IdentityKey struct {
	// Field is the Go struct field name.
	Field string

	// YAMLName is the recipe key, from the field's yaml tag.
	YAMLName string

	// Value is the field's value rendered as a string.
	Value string

	// Present is false when the field holds its zero value. A key that is not
	// present is omitted from the address, which is what lets the mutually
	// exclusive app / global pair render honestly.
	Present bool
}

// IdentityCollection is one declared collection field.
type IdentityCollection struct {
	// Field is the Go struct field name.
	Field string

	// YAMLName is the recipe key, from the field's yaml tag.
	YAMLName string

	// Item says what identifies one entry in the collection.
	Item ItemIdentity

	// ItemKeys are the element struct's own identity key yaml names, in
	// declaration order. Populated only when Item is ItemIdentityFields.
	ItemKeys []string
}

// TaskIdentity returns t's declared key fields in declaration order, each
// carrying the value it holds on t. Centralised so the loader, the export
// filter, and the docs generator share one read site, mirroring
// TaskProbeSupport and TaskExportSupport.
func TaskIdentity(t Task) []IdentityKey {
	v, ok := structValue(t)
	if !ok {
		return nil
	}
	typ := v.Type()

	var out []IdentityKey
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Tag.Get(identityTag) != identityTagKey {
			continue
		}
		value, present := renderIdentityValue(v.Field(i))
		out = append(out, IdentityKey{
			Field:    field.Name,
			YAMLName: yamlFieldName(field),
			Value:    value,
			Present:  present,
		})
	}
	return out
}

// TaskIdentityCollections returns t's declared collection fields in
// declaration order. Most tasks declare none and none declares more than one
// today, but the shape does not forbid it.
func TaskIdentityCollections(t Task) []IdentityCollection {
	v, ok := structValue(t)
	if !ok {
		return nil
	}
	typ := v.Type()

	var out []IdentityCollection
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if field.Tag.Get(identityTag) != identityTagCollection {
			continue
		}
		item, itemKeys := collectionItemIdentity(field.Type)
		out = append(out, IdentityCollection{
			Field:    field.Name,
			YAMLName: yamlFieldName(field),
			Item:     item,
			ItemKeys: itemKeys,
		})
	}
	return out
}

// collectionItemIdentity classifies a collection field's element type and, for
// a struct element, returns the yaml names of its own identity keys.
func collectionItemIdentity(t reflect.Type) (ItemIdentity, []string) {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() == reflect.Map {
		return ItemIdentityMapKey, nil
	}
	if t.Kind() != reflect.Slice && t.Kind() != reflect.Array {
		return ItemIdentityValue, nil
	}
	elem := t.Elem()
	for elem.Kind() == reflect.Ptr {
		elem = elem.Elem()
	}
	if elem.Kind() != reflect.Struct {
		return ItemIdentityValue, nil
	}
	var keys []string
	for i := 0; i < elem.NumField(); i++ {
		field := elem.Field(i)
		if field.Tag.Get(identityTag) != identityTagKey {
			continue
		}
		keys = append(keys, yamlFieldName(field))
	}
	return ItemIdentityFields, keys
}

// structValue dereferences t down to the struct value behind it.
func structValue(t Task) (reflect.Value, bool) {
	if t == nil {
		return reflect.Value{}, false
	}
	v := reflect.ValueOf(t)
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return reflect.Value{}, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}, false
	}
	return v, true
}

// renderIdentityValue stringifies one key field and reports whether it holds
// anything. A zero value is not present, which is what drops `global` from an
// app-scoped address and `app` from a global-scoped one.
func renderIdentityValue(v reflect.Value) (string, bool) {
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return "", false
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.String:
		s := v.String()
		return s, s != ""
	case reflect.Bool:
		if !v.Bool() {
			return "false", false
		}
		return "true", true
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n := v.Int()
		return strconv.FormatInt(n, 10), n != 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n := v.Uint()
		return strconv.FormatUint(n, 10), n != 0
	}
	return "", false
}

// IdentityAddress renders the resource t addresses as
// `dokku_config[app=api]`. Keys appear in declaration order; keys holding
// their zero value are omitted, so a task with no key value at all renders as
// the bare type key.
//
// The result is a parseable surface, not just a label: it is the auto-generated
// task name in the `--json` event stream, the argument `--start-at-task`
// accepts, and the argument `export --resource` accepts. Values are therefore
// never truncated - a lossy address could not be resolved back.
func IdentityAddress(typeKey string, t Task) string {
	keys := TaskIdentity(t)
	pairs := make([]string, 0, len(keys))
	for _, key := range keys {
		if !key.Present {
			continue
		}
		pairs = append(pairs, key.YAMLName+"="+quoteIdentityValue(key.Value))
	}
	if len(pairs) == 0 {
		return typeKey
	}
	return typeKey + "[" + strings.Join(pairs, ",") + "]"
}

// IdentityMatches reports whether every key in want matches t's value for that
// key. A key naming a field t does not declare never matches. Used by
// `export --resource` to narrow the bodies an exporter returned.
func IdentityMatches(t Task, want map[string]string) bool {
	if len(want) == 0 {
		return true
	}
	have := make(map[string]string, 8)
	declared := make(map[string]bool, 8)
	for _, key := range TaskIdentity(t) {
		declared[key.YAMLName] = true
		if key.Present {
			have[key.YAMLName] = key.Value
		}
	}
	for name, value := range want {
		if !declared[name] {
			return false
		}
		if have[name] != value {
			return false
		}
	}
	return true
}

// identityQuoteChars are the characters that force a value to be quoted.
//
// The set is deliberately small: only what a bare value cannot survive.
// ParseIdentityAddress splits pairs on unquoted commas and each pair on its
// first `=`, so `option=--memory=512m` and `option=-v /host:/container` both
// round-trip unquoted - which matters, because dokku_docker_options addresses
// are full of both and quoting them all would make the names unreadable.
const identityQuoteChars = ",]\""

// quoteIdentityValue renders one key value, quoting it only when a bare form
// would not parse back. An empty value is quoted so it is distinguishable
// from an omitted key.
//
// strconv.Quote also escapes what it wraps, so the value can reach the address
// spelled differently from the literal it was rendered from. Masking is literal
// substring replacement over the finished address, which is why
// subprocess.cleanSensitive registers a secret's Go-escaped spelling alongside
// its literal one; without that, a sensitive value carrying one of these
// characters printed in the clear inside its own address (#475). The address
// cannot be masked before it is quoted: it is generated while the recipe is
// parsed, before the mask registry is populated.
func quoteIdentityValue(value string) string {
	if value == "" || strings.ContainsAny(value, identityQuoteChars) {
		return strconv.Quote(value)
	}
	return value
}

// ParseIdentityAddress parses `dokku_config[app=api]` into its type key and
// key map. A bare type key parses to an empty map, which `export --resource`
// reads as "every resource of this type".
//
// The parse is strict: it rejects an auto-generated task name carrying a
// collision ordinal (`dokku_config[app=api] #2`), because that names a task,
// not a resource, and two tasks can address one resource.
func ParseIdentityAddress(address string) (string, map[string]string, error) {
	trimmed := strings.TrimSpace(address)
	if trimmed == "" {
		return "", nil, fmt.Errorf("resource address is empty")
	}

	open := strings.IndexByte(trimmed, '[')
	if open < 0 {
		// A bare type key is a legal address meaning "every resource of this
		// type". Anything else without brackets is a task name (an ordinal
		// suffix, a loop `(item=…)` suffix, a hand-written label), not one.
		if strings.ContainsAny(trimmed, "]\"= ") {
			return "", nil, fmt.Errorf("invalid resource address %q: expected <task_type> or <task_type>[key=value,...]", address)
		}
		return trimmed, map[string]string{}, nil
	}

	typeKey := trimmed[:open]
	if typeKey == "" {
		return "", nil, fmt.Errorf("invalid resource address %q: missing task type before %q", address, "[")
	}
	if !strings.HasSuffix(trimmed, "]") {
		return "", nil, fmt.Errorf("invalid resource address %q: missing closing %q", address, "]")
	}

	body := trimmed[open+1 : len(trimmed)-1]
	if strings.TrimSpace(body) == "" {
		return "", nil, fmt.Errorf("invalid resource address %q: %q holds no keys", address, "[]")
	}

	keys := map[string]string{}
	for _, pair := range splitIdentityPairs(body) {
		eq := strings.IndexByte(pair, '=')
		if eq <= 0 {
			return "", nil, fmt.Errorf("invalid resource address %q: %q is not key=value", address, pair)
		}
		name := pair[:eq]
		raw := pair[eq+1:]
		value := raw
		if strings.HasPrefix(raw, `"`) {
			unquoted, err := strconv.Unquote(raw)
			if err != nil {
				return "", nil, fmt.Errorf("invalid resource address %q: %s", address, err)
			}
			value = unquoted
		} else if strings.ContainsAny(raw, "]\"") {
			return "", nil, fmt.Errorf("invalid resource address %q: value for %q must be quoted", address, name)
		}
		if _, dup := keys[name]; dup {
			return "", nil, fmt.Errorf("invalid resource address %q: key %q appears twice", address, name)
		}
		keys[name] = value
	}
	return typeKey, keys, nil
}

// splitIdentityPairs splits an address body on commas that are not inside a
// quoted value.
func splitIdentityPairs(body string) []string {
	var (
		out      []string
		start    int
		inQuotes bool
		escaped  bool
	)
	for i := 0; i < len(body); i++ {
		switch {
		case escaped:
			escaped = false
		case body[i] == '\\' && inQuotes:
			escaped = true
		case body[i] == '"':
			inQuotes = !inQuotes
		case body[i] == ',' && !inQuotes:
			out = append(out, body[start:i])
			start = i + 1
		}
	}
	return append(out, body[start:])
}

// IdentityKeyNames returns the yaml names of t's declared keys, in declaration
// order. Used by the docs generator, which reads a registry prototype and so
// has no values to render.
func IdentityKeyNames(t Task) []string {
	keys := TaskIdentity(t)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, key.YAMLName)
	}
	return out
}

// sortedIdentityKeyNames is IdentityKeyNames sorted, for error messages that
// list what a task accepts.
func sortedIdentityKeyNames(t Task) []string {
	out := IdentityKeyNames(t)
	sort.Strings(out)
	return out
}
