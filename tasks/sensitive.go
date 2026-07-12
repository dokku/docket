package tasks

import (
	"reflect"

	"github.com/dokku/docket/subprocess"
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
// either:
//
//   - it lives in a struct field carrying the `sensitive:"true"` struct tag, or
//   - the task implements SensitiveOverride and the method returns it.
//
// String, []string, and map[string]string fields are supported. Nested
// structs and pointers are walked recursively. block/rescue/always group
// envelopes carry no task of their own, so their child envelopes are walked
// recursively to any depth. Empty values are dropped by the subprocess masker,
// not here.
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

// registerSensitiveMapValues adds every non-empty value of m to the global
// mask registry at plan time. It is for values that are secret but only become
// known after a server probe - the pre-run collection in commands/apply.go and
// commands/plan.go walks task structs, so it cannot see them (e.g. a
// scheduler-k3s trigger's surviving metadata read back from the server). The
// keys are not masked; only the values.
func registerSensitiveMapValues(m map[string]string) {
	if len(m) == 0 {
		return
	}
	values := make([]string, 0, len(m))
	for _, v := range m {
		values = append(values, v)
	}
	subprocess.AddGlobalSensitive(values...)
}

// sensitiveValuesFromTask returns the masked-value set for a single task.
func sensitiveValuesFromTask(t Task) []string {
	var out []string
	if override, ok := t.(SensitiveOverride); ok {
		out = append(out, override.SensitiveValues()...)
	}
	out = append(out, walkSensitiveTags(reflect.ValueOf(t))...)
	return out
}

// walkSensitiveTags recursively walks v and returns the value of any field
// whose `sensitive:"true"` struct tag is set. Pointers and embedded structs
// are followed; everything else is leaf-evaluated.
func walkSensitiveTags(v reflect.Value) []string {
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
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
		case fv.Kind() == reflect.Ptr && !fv.IsNil() && fv.Elem().Kind() == reflect.Struct:
			out = append(out, walkSensitiveTags(fv)...)
		}
	}
	return out
}

// sensitiveLeafValues extracts string values from a sensitive-tagged field.
// Supports string, []string, and map[string]string. For other types, returns
// empty (the field can't safely be string-masked).
func sensitiveLeafValues(v reflect.Value) []string {
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
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
