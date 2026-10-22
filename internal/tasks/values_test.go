package tasks

import (
	"encoding/json"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// TestTaskValuesMatchCatalog pins that TaskValues returns exactly the
// catalog's fields for every registered task, each typed per its FieldSchema
// and holding its default, so a caller never applies one itself (#644).
func TestTaskValuesMatchCatalog(t *testing.T) {
	t.Parallel()
	catalog, err := Catalog()
	if err != nil {
		t.Fatalf("Catalog: %v", err)
	}
	for _, schema := range catalog.Tasks {
		task, err := NewTask(schema.Type)
		if err != nil {
			t.Fatalf("NewTask(%s): %v", schema.Type, err)
		}
		values := TaskValues(task)

		var names []string
		for _, field := range schema.Fields {
			names = append(names, field.Name)
			value, ok := values[field.Name]
			if !ok {
				t.Errorf("%s: TaskValues has no %q", schema.Type, field.Name)
				continue
			}
			checkValueType(t, schema.Type+"."+field.Name, field, value)
			if want := defaultOrZero(field); !reflect.DeepEqual(value, want) {
				t.Errorf("%s.%s = %#v, want %#v", schema.Type, field.Name, value, want)
			}
		}
		var got []string
		for name := range values {
			got = append(got, name)
		}
		sort.Strings(names)
		sort.Strings(got)
		if !reflect.DeepEqual(got, names) {
			t.Errorf("%s: TaskValues keys = %v, want the catalog's %v", schema.Type, got, names)
		}
	}
}

// checkValueType fails unless value has the Go type TaskValues documents for
// field's catalog type.
func checkValueType(t *testing.T, path string, field FieldSchema, value any) {
	t.Helper()
	ok := false
	switch field.Type {
	case TypeString:
		_, ok = value.(string)
	case TypeBool:
		_, ok = value.(bool)
	case TypeInt:
		_, ok = value.(int64)
	case TypeFloat:
		_, ok = value.(float64)
	case TypeList:
		var list []any
		list, ok = value.([]any)
		ok = ok && list != nil
	case TypeDict:
		var dict map[string]any
		dict, ok = value.(map[string]any)
		ok = ok && dict != nil
	}
	if !ok {
		t.Errorf("%s: %#v is not the Go type for a %s", path, value, field.Type)
	}
}

// defaultOrZero is what TaskValues reads for a field nothing set: its catalog
// default, or the zero value of its type.
func defaultOrZero(field FieldSchema) any {
	if field.Default != nil {
		return field.Default
	}
	switch field.Type {
	case TypeString:
		return ""
	case TypeBool:
		return false
	case TypeInt:
		return int64(0)
	case TypeFloat:
		return float64(0)
	case TypeList:
		return []any{}
	case TypeDict:
		return map[string]any{}
	}
	return nil
}

// TestTaskValuesPointerReadsDefault pins the pointer rule: an unset *bool
// reads as the default the task coerces nil to, and a set one as its value.
func TestTaskValuesPointerReadsDefault(t *testing.T) {
	t.Parallel()
	if got := TaskValues(&ConfigTask{})["restart"]; got != true {
		t.Errorf("unset restart = %#v, want true", got)
	}
	if got := TaskValues(&ConfigTask{Restart: boolPtr(false)})["restart"]; got != false {
		t.Errorf("restart: false = %#v, want false", got)
	}
	if got := TaskValues(&PsScaleTask{})["skip_deploy"]; got != false {
		t.Errorf("unset skip_deploy = %#v, want false", got)
	}

	task, err := TaskFromValues("dokku_config", map[string]any{"app": "web", "restart": nil})
	if err != nil {
		t.Fatalf("TaskFromValues: %v", err)
	}
	if restart := task.(*ConfigTask).Restart; restart != nil {
		t.Errorf("restart: nil = %v, want an unset pointer", *restart)
	}
	task, err = TaskFromValues("dokku_config", map[string]any{"app": "web", "restart": false})
	if err != nil {
		t.Fatalf("TaskFromValues: %v", err)
	}
	if restart := task.(*ConfigTask).Restart; restart == nil || *restart {
		t.Errorf("restart: false = %v, want a pointer to false", restart)
	}
}

// TestTaskValuesValueAndPointer pins that the value type Observed carries
// reads the same as the registered pointer, and that nil reads nil.
func TestTaskValuesValueAndPointer(t *testing.T) {
	t.Parallel()
	task := AppTask{App: "web", State: StatePresent}
	if !reflect.DeepEqual(TaskValues(task), TaskValues(&task)) {
		t.Errorf("value %v != pointer %v", TaskValues(task), TaskValues(&task))
	}
	if got := TaskValues(nil); got != nil {
		t.Errorf("TaskValues(nil) = %v", got)
	}
	var nilTask *AppTask
	if got := TaskValues(nilTask); got != nil {
		t.Errorf("TaskValues(nil pointer) = %v", got)
	}
}

// TestTaskFromValuesAcceptsLosslessInputs pins the inputs TaskFromValues
// converts: any integer type, a whole float, and a json.Number for an int;
// typed slices and maps; a named string type; and nil leaving a default.
func TestTaskFromValuesAcceptsLosslessInputs(t *testing.T) {
	t.Parallel()
	for name, host := range map[string]any{
		"int":         80,
		"int32":       int32(80),
		"uint8":       uint8(80),
		"float64":     float64(80),
		"float32":     float32(80),
		"json.Number": json.Number("80"),
	} {
		task, err := TaskFromValues("dokku_ports", map[string]any{
			"app":           "web",
			"port_mappings": []map[string]any{{"scheme": "http", "host": host, "container": 5000}},
		})
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		got := task.(*PortsTask).PortMappings
		if len(got) != 1 || got[0].Host != 80 || got[0].Container != 5000 || got[0].Scheme != "http" {
			t.Errorf("%s: port_mappings = %+v", name, got)
		}
	}

	task, err := TaskFromValues("dokku_buildpacks", map[string]any{
		"app":        "web",
		"buildpacks": []string{"a", "b"},
		"state":      StateSet,
	})
	if err != nil {
		t.Fatalf("buildpacks: %v", err)
	}
	if bp := task.(*BuildpacksTask); !reflect.DeepEqual(bp.Buildpacks, []string{"a", "b"}) || bp.State != StateSet {
		t.Errorf("buildpacks = %+v", bp)
	}

	task, err = TaskFromValues("dokku_config", map[string]any{
		"app":    "web",
		"config": map[string]string{"A": "1"},
		"state":  nil,
	})
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	if config := task.(*ConfigTask); !reflect.DeepEqual(config.Config, map[string]string{"A": "1"}) || config.State != StatePresent {
		t.Errorf("config = %+v, want the map and the default state", config)
	}

	task, err = TaskFromValues("dokku_ps_scale", map[string]any{
		"app":   "web",
		"scale": map[string]any{"web": int64(2), "worker": json.Number("3")},
	})
	if err != nil {
		t.Fatalf("ps_scale: %v", err)
	}
	if scale := task.(*PsScaleTask).Scale; !reflect.DeepEqual(scale, map[string]int{"web": 2, "worker": 3}) {
		t.Errorf("scale = %v", scale)
	}
}

// TestTaskFromValuesNormalizesAny pins that an any-typed dict stores what a
// YAML decode would carry, with integers as int64, and refuses what a recipe
// could not spell.
func TestTaskFromValuesNormalizesAny(t *testing.T) {
	t.Parallel()
	task, err := TaskFromValues("dokku_scheduler_k3s_chart", map[string]any{
		"chart": "traefik",
		"values": map[string]any{
			"replicas": 2,
			"ratio":    json.Number("0.5"),
			"ports":    []int{80, 443},
			"tls":      map[string]bool{"enabled": true},
			"empty":    nil,
		},
	})
	if err != nil {
		t.Fatalf("TaskFromValues: %v", err)
	}
	want := map[string]any{
		"replicas": int64(2),
		"ratio":    0.5,
		"ports":    []any{int64(80), int64(443)},
		"tls":      map[string]any{"enabled": true},
		"empty":    nil,
	}
	if got := task.(*SchedulerK3sChartTask).Values; !reflect.DeepEqual(got, want) {
		t.Errorf("values = %#v, want %#v", got, want)
	}
	if got := TaskValues(task)["values"]; !reflect.DeepEqual(got, want) {
		t.Errorf("TaskValues values = %#v, want %#v", got, want)
	}
}

// TestTaskFromValuesErrors pins that unknown keys and type mismatches are
// errors naming the recipe key path.
func TestTaskFromValuesErrors(t *testing.T) {
	t.Parallel()
	users := func(second any) map[string]any {
		return map[string]any{
			"app":   "web",
			"users": []any{map[string]any{"username": "a", "password": "p"}, second},
		}
	}
	cases := []struct {
		name     string
		typeKey  string
		values   map[string]any
		contains string
	}{
		{"unknown type", "dokku_nope", nil, `unknown task type "dokku_nope"`},
		{"unknown key", "dokku_app", map[string]any{"ap": "web"}, `dokku_app: unknown key "ap"`},
		{"unknown nested key", "dokku_http_auth_user", users(map[string]any{"username": "b", "hsh": "x"}), `unknown key "users[1].hsh"`},
		{"nested mismatch", "dokku_http_auth_user", users(map[string]any{"username": "b", "hash": 5}), "users[1].hash: expected string, got int"},
		{"element not an object", "dokku_http_auth_user", users(nil), "users[1]: expected object, got null"},
		{"string for bool", "dokku_config", map[string]any{"restart": "yes"}, "restart: expected bool, got string"},
		{"int for string", "dokku_app", map[string]any{"app": 5}, "app: expected string, got int"},
		{"json.Number for string", "dokku_app", map[string]any{"app": json.Number("5")}, "app: expected string, got number"},
		{"dict value mismatch", "dokku_ps_scale", map[string]any{"scale": map[string]any{"web": "two"}}, `scale["web"]: expected int, got string`},
		{"fractional int", "dokku_ps_scale", map[string]any{"scale": map[string]any{"web": 1.5}}, `scale["web"]: 1.5 is not a whole number`},
		{"fractional json.Number", "dokku_ps_scale", map[string]any{"scale": map[string]any{"web": json.Number("1.5")}}, `scale["web"]: 1.5 is not a whole number`},
		{"uint overflow", "dokku_ps_scale", map[string]any{"scale": map[string]any{"web": uint64(math.MaxUint64)}}, "out of range for int"},
		{"float overflow", "dokku_ps_scale", map[string]any{"scale": map[string]any{"web": 1e19}}, "not a whole number in range"},
		{"string for list", "dokku_buildpacks", map[string]any{"buildpacks": "a"}, "buildpacks: expected list, got string"},
		{"null list element", "dokku_buildpacks", map[string]any{"buildpacks": []any{"a", nil}}, "buildpacks[1]: expected string, got null"},
		{"list for dict", "dokku_config", map[string]any{"config": []any{"A=1"}}, "config: expected dict, got list"},
		{"int-keyed dict", "dokku_config", map[string]any{"config": map[int]string{1: "a"}}, "config: expected dict, got a dict with int keys"},
		{"unstorable any", "dokku_scheduler_k3s_chart", map[string]any{"values": map[string]any{"c": make(chan int)}}, `values["c"]: a value of Go type chan int cannot be stored`},
		{"first error in key order", "dokku_app", map[string]any{"zz": 1, "aa": 1}, `unknown key "aa"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := TaskFromValues(tc.typeKey, tc.values)
			if err == nil {
				t.Fatalf("TaskFromValues(%v) succeeded, want an error containing %q", tc.values, tc.contains)
			}
			if !strings.Contains(err.Error(), tc.contains) {
				t.Errorf("error = %q, want it to contain %q", err, tc.contains)
			}
		})
	}
}
