package tasks

import (
	"reflect"

	defaults "github.com/mcuadros/go-defaults"
)

// declaredObserved returns the task an in-sync plan observes, in its value
// type and sharing no map or slice with either argument. Every field the
// declared task sets comes from it, so a value the probe reads back in another
// form - a process type scaled to zero, a list in another order - is reported
// as declared. A field the declared task leaves at its zero value declares
// nothing, so it keeps what the probe read, and so does such a field of a
// declared list element the read holds an element with the same identity keys
// for. Each field tagged write_only or apply_only is reset to its zero value
// or default, since a read never fills those.
func declaredObserved(declared, read Task) Task {
	dv := valueOf(declared)
	out := reflect.New(dv.Type()).Elem()
	out.Set(deepCopyValue(dv))
	if rv := valueOf(read); rv.IsValid() && rv.Type() == dv.Type() {
		fillUnset(out, rv)
	}
	resetUnreadFields(out)
	return out.Interface().(Task)
}

// fillUnset gives each field of the struct out that declares nothing the
// value read holds for it, descending into nested structs and into each
// element of a list of structs that read holds an element with the same
// identity keys for.
func fillUnset(out, read reflect.Value) {
	for i := 0; i < out.NumField(); i++ {
		if !out.Type().Field(i).IsExported() {
			continue
		}
		fv, rv := out.Field(i), read.Field(i)
		if isUnset(fv) {
			fv.Set(deepCopyValue(rv))
			continue
		}
		switch fv.Kind() {
		case reflect.Struct:
			fillUnset(fv, rv)
		case reflect.Slice:
			keys := itemKeyFields(fv.Type().Elem())
			if len(keys) == 0 {
				continue
			}
			for j := 0; j < fv.Len(); j++ {
				if match, ok := findItem(rv, fv.Index(j), keys); ok {
					fillUnset(fv.Index(j), match)
				}
			}
		}
	}
}

// itemKeyFields returns the indexes of the identity key fields of a list's
// struct element type, or nil when the element is not a struct or has none.
func itemKeyFields(elem reflect.Type) []int {
	if elem.Kind() != reflect.Struct {
		return nil
	}
	var keys []int
	for i := 0; i < elem.NumField(); i++ {
		if elem.Field(i).Tag.Get(identityTag) == identityTagKey {
			keys = append(keys, i)
		}
	}
	return keys
}

// findItem returns the element of list whose identity key fields equal item's.
func findItem(list, item reflect.Value, keys []int) (reflect.Value, bool) {
	for j := 0; j < list.Len(); j++ {
		candidate := list.Index(j)
		match := true
		for _, k := range keys {
			if !reflect.DeepEqual(candidate.Field(k).Interface(), item.Field(k).Interface()) {
				match = false
				break
			}
		}
		if match {
			return candidate, true
		}
	}
	return reflect.Value{}, false
}

// valueOf returns the struct a task value or pointer holds, or the zero Value
// for a nil task.
func valueOf(t Task) reflect.Value {
	rv := reflect.ValueOf(t)
	for rv.Kind() == reflect.Pointer && !rv.IsNil() {
		rv = rv.Elem()
	}
	return rv
}

// isUnset reports whether a declared field declares nothing: its zero value,
// or an empty map or list.
func isUnset(fv reflect.Value) bool {
	switch fv.Kind() {
	case reflect.Map, reflect.Slice:
		return fv.Len() == 0
	}
	return fv.IsZero()
}

// declaresPresent reports whether a task value declares its resource there:
// false for a State of absent, and for a State of clear on a task with no
// collection; true for any other State and for a task with none. A clear
// collection is the whole collection with no members, which is still there.
func declaresPresent(task Task) bool {
	state, ok := taskStateOf(task)
	if !ok {
		return true
	}
	switch state {
	case StateAbsent:
		return false
	case StateClear:
		return ownsWholeCollection(task)
	}
	return true
}

// ownsWholeCollection reports whether a task value is a collection task in a
// state that owns its whole collection: set or clear. For such a task the
// resource is the app's, service's or scope's whole collection, which is
// there whether or not it has members, so a read that succeeds always finds
// it. The additive states, present and absent, own only the members they name.
func ownsWholeCollection(task Task) bool {
	state, ok := taskStateOf(task)
	if !ok || (state != StateSet && state != StateClear) {
		return false
	}
	return len(TaskIdentityCollections(task)) > 0
}

// emptyWholeCollection reports whether a task value owns its whole collection
// and holds no member in any of its collection fields. A read that returns
// one is the only read reporting a resource there that has not shown the
// server holds the app or service the collection belongs to.
func emptyWholeCollection(task Task) bool {
	if task == nil || !ownsWholeCollection(task) {
		return false
	}
	rv := valueOf(task)
	for _, collection := range TaskIdentityCollections(task) {
		if !isUnset(rv.FieldByName(collection.Field)) {
			return false
		}
	}
	return true
}

// taskStateOf returns the State a task value holds, and false for a task with
// no State field.
func taskStateOf(task Task) (State, bool) {
	rv := valueOf(task)
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		return "", false
	}
	field := rv.FieldByName("State")
	if !field.IsValid() || field.Kind() != reflect.String {
		return "", false
	}
	return State(field.String()), true
}

// deepCopyValue returns a copy of v that shares no map, slice or pointer with
// it. Unexported struct fields are copied shallowly.
func deepCopyValue(v reflect.Value) reflect.Value {
	out := reflect.New(v.Type()).Elem()
	out.Set(v)
	switch v.Kind() {
	case reflect.Map:
		if v.IsNil() {
			return out
		}
		m := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			m.SetMapIndex(iter.Key(), deepCopyValue(iter.Value()))
		}
		out.Set(m)
	case reflect.Slice:
		if v.IsNil() {
			return out
		}
		s := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			s.Index(i).Set(deepCopyValue(v.Index(i)))
		}
		out.Set(s)
	case reflect.Pointer:
		if v.IsNil() {
			return out
		}
		p := reflect.New(v.Type().Elem())
		p.Elem().Set(deepCopyValue(v.Elem()))
		out.Set(p)
	case reflect.Interface:
		if v.IsNil() {
			return out
		}
		out.Set(deepCopyValue(v.Elem()))
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if v.Type().Field(i).IsExported() {
				out.Field(i).Set(deepCopyValue(v.Field(i)))
			}
		}
	}
	return out
}

// resetUnreadFields sets every field of the struct v tagged write_only or
// apply_only to the value a read leaves it at - its default, or its zero value
// without one - descending into nested structs and the elements of a list of
// structs.
func resetUnreadFields(v reflect.Value) {
	if v.Kind() != reflect.Struct {
		return
	}
	rt := v.Type()
	fresh := reflect.New(rt)
	defaults.SetDefaults(fresh.Interface())
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if !field.IsExported() {
			continue
		}
		fv := v.Field(i)
		if field.Tag.Get("write_only") == "true" || field.Tag.Get("apply_only") == "true" {
			fv.Set(fresh.Elem().Field(i))
			continue
		}
		switch fv.Kind() {
		case reflect.Struct:
			resetUnreadFields(fv)
		case reflect.Slice:
			for j := 0; j < fv.Len(); j++ {
				resetUnreadFields(fv.Index(j))
			}
		}
	}
}
