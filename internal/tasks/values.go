package tasks

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
)

// An embedder that holds task values in its own structure - Terraform
// attribute values, a JSON API body - used to build a task by marshaling them
// to YAML for DecodeTask, and to read one back by marshaling the task to YAML
// and walking the catalog to restore what the round trip lost: an int read
// back as a float, a dict of ints, a key `omitempty` dropped (#644). The two
// functions here do that once, keyed by recipe key and typed by the catalog.

// TaskFromValues builds a task of the registered type typeKey from values
// keyed by recipe key, with defaults applied, the way DecodeTask builds one
// from a YAML body. Like DecodeTask it checks shape and type only; the task's
// own rules - required fields, choices, constraints - are for
// ConstraintViolations and the task's Validate method.
//
// Each value must match its field's catalog type, and is accepted where the
// conversion is lossless:
//
//   - string: any Go string, including a named string type such as State. A
//     json.Number is a number, not a string, and is refused.
//   - bool: a bool.
//   - int: any Go integer type, a float32 or float64 holding a whole number,
//     or a json.Number holding an integer, within the field's range.
//   - float: any Go integer or float type, or a json.Number.
//   - list: any slice or array, []any or typed such as []string, whose
//     elements each match the item type.
//   - dict and object: any map with string keys, map[string]any or typed
//     such as map[string]string; a dict's values each match the item type,
//     and an object's keys are its element's recipe keys.
//   - any: nil, a bool, a string, a number, or a list or string-keyed map of
//     those, stored with integers as int64 and floats as float64.
//
// A nil value leaves the field unset, so it takes its default; a nil list
// element or dict value is refused unless the item type is any. An unknown key
// or a value of the wrong type is an error naming the recipe key path, such as
// `users[1].hash` or `scale["web"]`, and the first one in sorted key order is
// reported.
//
//sdk:export
func TaskFromValues(typeKey string, values map[string]any) (Task, error) {
	v, err := allocateTask(typeKey)
	if err != nil {
		return nil, err
	}
	if err := assignStruct(v.Elem(), values, ""); err != nil {
		return nil, fmt.Errorf("%s: %w", typeKey, err)
	}
	return finishTask(typeKey, v)
}

// TaskValues returns every field of t keyed by recipe key, typed per the
// catalog's FieldSchema: a string, a bool, an int64, a float64, a
// map[string]any for a dict or an object, and a []any for a list. An any item
// comes back with the same types, integers as int64. t may be the registered
// pointer or the value type, as Observed is.
//
// Every field is present, including one left at its zero value or default, so
// a caller sees the same keys for every task of a type and applies no defaults
// itself. An empty list is []any{} and an empty dict map[string]any{}, never
// nil. A pointer field with no value reads as the default its catalog entry
// documents, which is what the task coerces nil to, so an unset `restart` on
// dokku_config reads true; a pointer field with no default would read nil,
// and no task declares one today. Feeding the map back to TaskFromValues sets
// the pointer to that default, which the task treats the same as nil.
//
// Values are raw: mask sensitive ones before showing them. A nil t returns nil.
//
//sdk:export
func TaskValues(t Task) map[string]any {
	rv := reflect.ValueOf(t)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		return nil
	}
	return structValues(rv)
}

// recipeFields maps each recipe key of a struct to its field index.
func recipeFields(rt reflect.Type) map[string]int {
	fields := make(map[string]int, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if field.PkgPath != "" {
			continue
		}
		if name := catalogYAMLName(field.Tag, field.Name); name != "" {
			fields[name] = i
		}
	}
	return fields
}

// joinKey appends a struct key to a recipe key path.
func joinKey(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// sortedKeys returns a string-keyed map's keys in order, so errors and map
// building do not depend on Go's map iteration order.
func sortedKeys(m reflect.Value) []reflect.Value {
	keys := m.MapKeys()
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	return keys
}

// assignStruct sets the fields of the struct dst from values keyed by recipe
// key. path is the recipe key path of dst, "" for the task itself.
func assignStruct(dst reflect.Value, values map[string]any, path string) error {
	fields := recipeFields(dst.Type())
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		index, ok := fields[key]
		if !ok {
			return fmt.Errorf("unknown key %q", joinKey(path, key))
		}
		value := values[key]
		if value == nil {
			continue
		}
		if err := assignValue(dst.Field(index), value, joinKey(path, key)); err != nil {
			return err
		}
	}
	return nil
}

// assignValue sets dst from src, converting where it is lossless.
func assignValue(dst reflect.Value, src any, path string) error {
	if dst.Kind() == reflect.Interface {
		value, err := normalizeAnyValue(src, path)
		if err != nil {
			return err
		}
		if value != nil {
			dst.Set(reflect.ValueOf(value))
		}
		return nil
	}
	if src == nil {
		return fmt.Errorf("%s: expected %s, got null", path, valueTypeName(dst.Type()))
	}
	if dst.Kind() == reflect.Pointer {
		elem := reflect.New(dst.Type().Elem())
		if err := assignValue(elem.Elem(), src, path); err != nil {
			return err
		}
		dst.Set(elem)
		return nil
	}

	sv := reflect.ValueOf(src)
	mismatch := func() error {
		return fmt.Errorf("%s: expected %s, got %s", path, valueTypeName(dst.Type()), sourceTypeName(src))
	}
	switch dst.Kind() {
	case reflect.String:
		if _, isNumber := src.(json.Number); isNumber || sv.Kind() != reflect.String {
			return mismatch()
		}
		dst.SetString(sv.String())
	case reflect.Bool:
		if sv.Kind() != reflect.Bool {
			return mismatch()
		}
		dst.SetBool(sv.Bool())
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, ok, err := integerOf(src)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if !ok {
			return mismatch()
		}
		if dst.OverflowInt(n) {
			return fmt.Errorf("%s: %d is out of range for %s", path, n, valueTypeName(dst.Type()))
		}
		dst.SetInt(n)
	case reflect.Float32, reflect.Float64:
		f, ok := floatOf(src)
		if !ok {
			return mismatch()
		}
		if dst.OverflowFloat(f) {
			return fmt.Errorf("%s: %v is out of range for %s", path, f, valueTypeName(dst.Type()))
		}
		dst.SetFloat(f)
	case reflect.Slice:
		if sv.Kind() != reflect.Slice && sv.Kind() != reflect.Array {
			return mismatch()
		}
		out := reflect.MakeSlice(dst.Type(), sv.Len(), sv.Len())
		for i := 0; i < sv.Len(); i++ {
			if err := assignValue(out.Index(i), sv.Index(i).Interface(), fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		dst.Set(out)
	case reflect.Map:
		if sv.Kind() != reflect.Map || sv.Type().Key().Kind() != reflect.String {
			return mismatch()
		}
		out := reflect.MakeMapWithSize(dst.Type(), sv.Len())
		for _, key := range sortedKeys(sv) {
			elem := reflect.New(dst.Type().Elem()).Elem()
			if err := assignValue(elem, sv.MapIndex(key).Interface(), fmt.Sprintf("%s[%q]", path, key.String())); err != nil {
				return err
			}
			out.SetMapIndex(reflect.ValueOf(key.String()).Convert(dst.Type().Key()), elem)
		}
		dst.Set(out)
	case reflect.Struct:
		if sv.Kind() != reflect.Map || sv.Type().Key().Kind() != reflect.String {
			return mismatch()
		}
		values := make(map[string]any, sv.Len())
		for _, key := range sv.MapKeys() {
			values[key.String()] = sv.MapIndex(key).Interface()
		}
		return assignStruct(dst, values, path)
	default:
		return fmt.Errorf("%s: fields of Go type %s cannot be set from values", path, dst.Type())
	}
	return nil
}

// integerOf reads src as an integer. ok is false when src is not a number at
// all; err is set when it is one that cannot become an integer losslessly.
func integerOf(src any) (int64, bool, error) {
	if number, isNumber := src.(json.Number); isNumber {
		n, err := strconv.ParseInt(string(number), 10, 64)
		if err != nil {
			return 0, true, fmt.Errorf("%s is not a whole number in range for int", number)
		}
		return n, true, nil
	}
	sv := reflect.ValueOf(src)
	switch sv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return sv.Int(), true, nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if sv.Uint() > math.MaxInt64 {
			return 0, true, fmt.Errorf("%d is out of range for int", sv.Uint())
		}
		return int64(sv.Uint()), true, nil
	case reflect.Float32, reflect.Float64:
		f := sv.Float()
		// 2^63 is the first float64 past MaxInt64; anything at or above it, or
		// below -2^63, does not fit.
		if f != math.Trunc(f) || f >= math.Exp2(63) || f < -math.Exp2(63) {
			return 0, true, fmt.Errorf("%v is not a whole number in range for int", f)
		}
		return int64(f), true, nil
	}
	return 0, false, nil
}

// floatOf reads src as a float. ok is false when src is not a number.
func floatOf(src any) (float64, bool) {
	if number, isNumber := src.(json.Number); isNumber {
		f, err := number.Float64()
		return f, err == nil
	}
	sv := reflect.ValueOf(src)
	switch sv.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return float64(sv.Int()), true
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return float64(sv.Uint()), true
	case reflect.Float32, reflect.Float64:
		return sv.Float(), true
	}
	return 0, false
}

// normalizeAnyValue converts a value bound for an any-typed field to the
// forms TaskValues returns, refusing what a recipe could not spell.
func normalizeAnyValue(src any, path string) (any, error) {
	if src == nil {
		return nil, nil
	}
	if number, isNumber := src.(json.Number); isNumber {
		if n, err := strconv.ParseInt(string(number), 10, 64); err == nil {
			return n, nil
		}
		f, err := number.Float64()
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not a number", path, string(number))
		}
		return f, nil
	}
	sv := reflect.ValueOf(src)
	switch sv.Kind() {
	case reflect.Bool:
		return sv.Bool(), nil
	case reflect.String:
		return sv.String(), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		n, _, err := integerOf(src)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		return n, nil
	case reflect.Float32, reflect.Float64:
		return sv.Float(), nil
	case reflect.Pointer:
		if sv.IsNil() {
			return nil, nil
		}
		return normalizeAnyValue(sv.Elem().Interface(), path)
	case reflect.Slice, reflect.Array:
		out := make([]any, sv.Len())
		for i := 0; i < sv.Len(); i++ {
			value, err := normalizeAnyValue(sv.Index(i).Interface(), fmt.Sprintf("%s[%d]", path, i))
			if err != nil {
				return nil, err
			}
			out[i] = value
		}
		return out, nil
	case reflect.Map:
		if sv.Type().Key().Kind() != reflect.String {
			return nil, fmt.Errorf("%s: expected a map with string keys, got %s", path, sourceTypeName(src))
		}
		out := make(map[string]any, sv.Len())
		for _, key := range sortedKeys(sv) {
			value, err := normalizeAnyValue(sv.MapIndex(key).Interface(), fmt.Sprintf("%s[%q]", path, key.String()))
			if err != nil {
				return nil, err
			}
			out[key.String()] = value
		}
		return out, nil
	}
	return nil, fmt.Errorf("%s: a value of Go type %T cannot be stored", path, src)
}

// valueTypeName names a field's type in the catalog's vocabulary, with an
// element struct called object rather than unknown.
func valueTypeName(t reflect.Type) string {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Struct {
		return TypeObject
	}
	return catalogFieldType(t)
}

// sourceTypeName names a caller's value in the catalog's vocabulary, falling
// back to its Go type for anything the catalog has no name for.
func sourceTypeName(src any) string {
	if _, isNumber := src.(json.Number); isNumber {
		return "number"
	}
	t := reflect.TypeOf(src)
	switch t.Kind() {
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return TypeInt
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			return fmt.Sprintf("a dict with %s keys", t.Key())
		}
	}
	if name := valueTypeName(t); name != TypeUnknown && name != TypeObject {
		return name
	}
	return fmt.Sprintf("Go type %s", t)
}

// structValues reads every recipe field of the struct rv.
func structValues(rv reflect.Value) map[string]any {
	rt := rv.Type()
	out := make(map[string]any, rt.NumField())
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name := catalogYAMLName(field.Tag, field.Name)
		if name == "" {
			continue
		}
		out[name] = fieldValue(rv.Field(i), field)
	}
	return out
}

// fieldValue reads one field, giving a nil pointer the default its tag
// documents.
func fieldValue(v reflect.Value, field reflect.StructField) any {
	if v.Kind() == reflect.Pointer && v.IsNil() {
		itemType := ""
		if item, err := buildItem(field.Type, false); err == nil && item != nil {
			itemType = item.Type
		}
		value, err := ParseFieldDefault(catalogFieldType(field.Type), itemType, field.Tag.Get("default"))
		if err != nil {
			// The catalog refuses to build over such a tag, and a test builds it
			// for every task, so this is unreachable for a registered task.
			return nil
		}
		return value
	}
	return plainValue(v)
}

// plainValue converts a reflected value to the types TaskValues returns.
func plainValue(v reflect.Value) any {
	switch v.Kind() {
	case reflect.Pointer, reflect.Interface:
		if v.IsNil() {
			return nil
		}
		return plainValue(v.Elem())
	case reflect.String:
		return v.String()
	case reflect.Bool:
		return v.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return v.Int()
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		if v.Uint() <= math.MaxInt64 {
			return int64(v.Uint())
		}
		return v.Uint()
	case reflect.Float32, reflect.Float64:
		return v.Float()
	case reflect.Slice, reflect.Array:
		out := make([]any, v.Len())
		for i := 0; i < v.Len(); i++ {
			out[i] = plainValue(v.Index(i))
		}
		return out
	case reflect.Map:
		out := make(map[string]any, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			key := iter.Key()
			for key.Kind() == reflect.Interface && !key.IsNil() {
				key = key.Elem()
			}
			name := fmt.Sprint(key.Interface())
			if key.Kind() == reflect.String {
				name = key.String()
			}
			out[name] = plainValue(iter.Value())
		}
		return out
	case reflect.Struct:
		return structValues(v)
	}
	if v.IsValid() && v.CanInterface() {
		return v.Interface()
	}
	return nil
}
