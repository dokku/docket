package tasks

import (
	"context"
	"reflect"

	"github.com/dokku/docket/internal/subprocess"
)

// SensitiveOverride is implemented by tasks whose sensitive values cannot be
// declared with a struct tag - typically because the secret lives in the
// values of an arbitrary map (e.g. dokku_config's `config:` field where every
// value is a secret regardless of key).
//
// SensitiveValues returns the literal string values from this task instance
// that must be masked in any user-facing logging output. The tag-based walker
// runs in addition to this method; both contribute to the final masked set.
type SensitiveOverride interface {
	SensitiveValues() []string
}

// CollectSensitiveValues walks every task in tasks and returns the union of
// values that must be masked in user-facing output. A value is included when
// any of:
//
//   - it lives in a struct field carrying the `sensitive:"true"` struct tag,
//     including a field of an item in a slice or map of structs,
//   - it is the value of a property task whose property is marked Sensitive
//     in the task's property table, or
//   - the task implements SensitiveOverride and the method returns it.
//
// String, []string, and map[string]string fields are supported. Nested
// structs, pointers, and the struct items of slices and maps are walked
// recursively. block/rescue/always group envelopes carry no task of their
// own, so their child envelopes are walked recursively to any depth. Empty
// values are dropped by the subprocess masker, not here.
func CollectSensitiveValues(tasks OrderedStringEnvelopeMap) []string {
	var out []string
	for _, name := range tasks.Keys() {
		out = append(out, sensitiveValuesFromEnvelope(tasks.GetEnvelope(name))...)
	}
	return out
}

// sensitiveValuesFromEnvelope returns the masked-value set for one envelope.
// A leaf envelope contributes its task's values; a block/rescue/always group
// (Task == nil) contributes the union of its children, recursing so a secret
// declared only inside a nested group still surfaces. Mirrors the group
// recursion in CollectEnvelopeNames.
func sensitiveValuesFromEnvelope(env *TaskEnvelope) []string {
	if env == nil {
		return nil
	}
	var out []string
	if env.Task != nil {
		out = append(out, sensitiveValuesFromTask(env.Task)...)
	}
	for _, child := range env.Block {
		out = append(out, sensitiveValuesFromEnvelope(child)...)
	}
	for _, child := range env.Rescue {
		out = append(out, sensitiveValuesFromEnvelope(child)...)
	}
	for _, child := range env.Always {
		out = append(out, sensitiveValuesFromEnvelope(child)...)
	}
	return out
}

// CollectPlaySensitiveValues is the multi-play sibling of
// CollectSensitiveValues. It walks every task across every play and
// returns the union of sensitive values, in play-then-task order.
func CollectPlaySensitiveValues(plays []*Play) []string {
	var out []string
	for _, play := range plays {
		if play == nil {
			continue
		}
		out = append(out, CollectSensitiveValues(play.Tasks)...)
	}
	return out
}

// registerSensitiveMapValues adds every non-empty value of m to the run's
// masker at plan time. It is for values that are secret but only become
// known after a server probe - the pre-run collection in internal/commands/apply.go and
// internal/commands/plan.go walks task structs, so it cannot see them (e.g. a
// scheduler-k3s trigger's surviving metadata read back from the server). The
// keys are not masked; only the values.
func registerSensitiveMapValues(ctx context.Context, m map[string]string) {
	if len(m) == 0 {
		return
	}
	values := make([]string, 0, len(m))
	for _, v := range m {
		values = append(values, v)
	}
	subprocess.MaskerFromContext(ctx).Add(values...)
}

// sensitiveValuesFromTask returns the masked-value set for a single task.
//
// It takes interface{} rather than Task because docket export walks task
// bodies an exporter returns as values, while Lookup and NewTask hand out
// pointers (*ConfigTask). Requiring the interface would silently
// contribute nothing for a task whose Task methods happen to have a pointer
// receiver - the wrong failure mode for a masking collector. Both steps below
// work on any value.
func sensitiveValuesFromTask(t interface{}) []string {
	if isNilPointer(t) {
		return nil
	}
	var out []string
	if override, ok := t.(SensitiveOverride); ok {
		out = append(out, override.SensitiveValues()...)
	}
	out = append(out, sensitivePropertyValues(t)...)
	out = append(out, walkSensitiveTags(reflect.ValueOf(t))...)
	return out
}

// TaskSensitiveValues returns the values that must be masked in anything
// rendered for t: its `sensitive:"true"` fields, including those of nested
// items, the value of a property its property table marks Sensitive, and
// whatever its SensitiveValues method returns. It is the set the CLI collects
// for t before a run. Empty values are dropped and duplicates removed; the
// order is unspecified. It returns nil for a nil task.
func TaskSensitiveValues(t Task) []string {
	var out []string
	seen := map[string]bool{}
	for _, v := range sensitiveValuesFromTask(t) {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

// isNilPointer reports whether t is nil or a nil pointer. The registered
// tasks' methods have value receivers, so calling one through a nil pointer
// panics.
func isNilPointer(t interface{}) bool {
	if t == nil {
		return true
	}
	v := reflect.ValueOf(t)
	return v.Kind() == reflect.Pointer && v.IsNil()
}

// sensitivePropertyValues returns a property task's value when its property
// table marks that property Sensitive. Whether the value is a secret depends
// on which property it is, so no struct tag can say it; planProperty registers
// the same value with the masker at plan time, which is too late for anything
// rendered before Plan runs. Every property task is a conversion of
// PropertyFields or SensitivePropertyFields, so the fields are read by name.
func sensitivePropertyValues(t interface{}) []string {
	task, ok := t.(PropertyTableDocer)
	if !ok || isNilPointer(t) {
		return nil
	}
	v := reflect.Indirect(reflect.ValueOf(t))
	if v.Kind() != reflect.Struct {
		return nil
	}
	property := v.FieldByName("Property")
	value := v.FieldByName("Value")
	if property.Kind() != reflect.String || value.Kind() != reflect.String || value.String() == "" {
		return nil
	}
	if !taskPropertyEntry(task, property.String()).Sensitive {
		return nil
	}
	return []string{value.String()}
}

// walkSensitiveTags recursively walks v and returns the value of any field
// whose `sensitive:"true"` struct tag is set. Pointers, embedded structs, and
// the struct items of slices, arrays and maps are followed; everything else
// is leaf-evaluated.
func walkSensitiveTags(v reflect.Value) []string {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	t := v.Type()

	var out []string
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		fv := v.Field(i)
		if field.Tag.Get("sensitive") == "true" {
			out = append(out, sensitiveLeafValues(fv)...)
			continue
		}
		// Recurse into nested structs and struct pointers regardless of tag
		// so deeply nested sensitive fields surface too.
		switch {
		case fv.Kind() == reflect.Struct:
			out = append(out, walkSensitiveTags(fv)...)
		case fv.Kind() == reflect.Pointer && !fv.IsNil() && fv.Elem().Kind() == reflect.Struct:
			out = append(out, walkSensitiveTags(fv)...)
		case isStructCollection(fv.Type()):
			out = append(out, walkSensitiveItems(fv)...)
		}
	}
	return out
}

// isStructCollection reports whether t is a slice, array or map whose items
// are structs or struct pointers, such as dokku_http_auth_user's users.
func isStructCollection(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
	default:
		return false
	}
	elem := t.Elem()
	if elem.Kind() == reflect.Pointer {
		elem = elem.Elem()
	}
	return elem.Kind() == reflect.Struct
}

// walkSensitiveItems walks each item of a struct collection, so a tagged
// field on an item is collected like one on the task itself.
func walkSensitiveItems(v reflect.Value) []string {
	var out []string
	if v.Kind() == reflect.Map {
		iter := v.MapRange()
		for iter.Next() {
			out = append(out, walkSensitiveTags(iter.Value())...)
		}
		return out
	}
	for i := 0; i < v.Len(); i++ {
		out = append(out, walkSensitiveTags(v.Index(i))...)
	}
	return out
}

// sensitiveLeafValues extracts string values from a sensitive-tagged field.
// Supports string, []string, and map[string]string. For other types, returns
// empty (the field can't safely be string-masked).
func sensitiveLeafValues(v reflect.Value) []string {
	for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.String:
		s := v.String()
		if s == "" {
			return nil
		}
		return []string{s}
	case reflect.Slice, reflect.Array:
		if v.Type().Elem().Kind() != reflect.String {
			return nil
		}
		out := make([]string, 0, v.Len())
		for i := 0; i < v.Len(); i++ {
			s := v.Index(i).String()
			if s == "" {
				continue
			}
			out = append(out, s)
		}
		return out
	case reflect.Map:
		if v.Type().Elem().Kind() != reflect.String {
			return nil
		}
		out := make([]string, 0, v.Len())
		iter := v.MapRange()
		for iter.Next() {
			s := iter.Value().String()
			if s == "" {
				continue
			}
			out = append(out, s)
		}
		return out
	}
	return nil
}
