package tasks

import (
	"reflect"
	"slices"
	"strings"
	"testing"
)

// datastoreTasks are the task types that change a datastore service. Each
// tags its datastore type and its service name, and no other task tags
// either.
var datastoreTasks = map[string]bool{
	"dokku_acl_service":      true,
	"dokku_service_backup":   true,
	"dokku_service_create":   true,
	"dokku_service_expose":   true,
	"dokku_service_link":     true,
	"dokku_service_property": true,
}

// TestSerializeTagsCoverEveryObject pins which fields name a task's objects:
// every `app` identity key names the app, the datastore tasks name their
// service by its type and its name, and nothing else is tagged. A task added
// without the tag would run concurrently with other tasks on its app.
func TestSerializeTagsCoverEveryObject(t *testing.T) {
	t.Parallel()
	for typeKey, task := range allRegisteredTasks() {
		typ := taskStructOf(task)
		roles := map[string]int{}
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			tag, tagged := field.Tag.Lookup(serializeTag)
			isAppKey := yamlFieldName(field) == "app" && field.Tag.Get(identityTag) == identityTagKey
			if isAppKey && tag != "app" {
				t.Errorf("%s.%s is the app identity key but is not tagged serialize:\"app\"", typeKey, field.Name)
			}
			if !tagged {
				continue
			}
			if !serializeTagValid(tag) {
				t.Errorf("%s.%s: serialize tag %q is not a known kind and role", typeKey, field.Name, tag)
			}
			if tag == "app" && !isAppKey {
				t.Errorf("%s.%s is tagged serialize:\"app\" but is not the app identity key", typeKey, field.Name)
			}
			if field.Tag.Get(identityTag) != identityTagKey {
				t.Errorf("%s.%s is tagged serialize but is not an identity key", typeKey, field.Name)
			}
			roles[tag]++
		}
		for tag, n := range roles {
			if n > 1 {
				t.Errorf("%s tags %d fields serialize:%q, want one", typeKey, n, tag)
			}
		}
		service := roles["service.type"] == 1 && roles["service.name"] == 1
		if datastoreTasks[typeKey] && !service {
			t.Errorf("%s changes a datastore service but does not tag both service.type and service.name", typeKey)
		}
		if !datastoreTasks[typeKey] && (roles["service.type"] > 0 || roles["service.name"] > 0) {
			t.Errorf("%s tags a service but is not listed in datastoreTasks", typeKey)
		}
	}
}

// serializeTagValid reports whether a serialize tag names a known kind and
// one of its roles, so a typo TaskObjects would skip fails the build.
func serializeTagValid(tag string) bool {
	kind, role, _ := strings.Cut(tag, ".")
	return slices.Contains(objectRoles[ObjectKind(kind)], role)
}

// TestObjectEffectsPinned pins which tasks create or restart their objects.
// Each entry was checked against the dokku and plugin source; see each
// task's ObjectEffects for the command that does it. A task missing here
// declares nothing, and must not.
func TestObjectEffectsPinned(t *testing.T) {
	t.Parallel()
	want := map[string]string{
		"dokku_app":              "app: creates",
		"dokku_app_clone":        "app: creates, restarts unless [skip_deploy]",
		"dokku_config":           "app: restarts when [restart]",
		"dokku_domains_toggle":   "app: restarts states [present]",
		"dokku_git_from_archive": "app: restarts",
		"dokku_git_from_image":   "app: restarts",
		"dokku_git_sync":         "app: restarts when [build build_if_changes]",
		"dokku_proxy_toggle":     "app: restarts",
		"dokku_ps_scale":         "app: restarts unless [skip_deploy]",
		"dokku_service_create":   "service: creates, restarts",
		"dokku_service_link":     "app: restarts",
	}
	for typeKey, task := range allRegisteredTasks() {
		var got []string
		for _, object := range TaskObjects(task) {
			var effects []string
			if object.Creates {
				effects = append(effects, "creates")
			}
			if r := object.Restarts; r != nil {
				restart := "restarts"
				if len(r.When) > 0 {
					restart += " when [" + strings.Join(r.When, " ") + "]"
				}
				if len(r.Unless) > 0 {
					restart += " unless [" + strings.Join(r.Unless, " ") + "]"
				}
				if len(r.States) > 0 {
					states := make([]string, len(r.States))
					for i, s := range r.States {
						states[i] = string(s)
					}
					restart += " states [" + strings.Join(states, " ") + "]"
				}
				effects = append(effects, restart)
			}
			if len(effects) > 0 {
				got = append(got, string(object.Kind)+": "+strings.Join(effects, ", "))
			}
		}
		if g := strings.Join(got, "; "); g != want[typeKey] {
			t.Errorf("%s effects = %q, want %q", typeKey, g, want[typeKey])
		}
	}
}

// TestObjectEffectsNameRealFields pins that every key and state a declaration
// names exists on the task, and that it declares effects only on objects the
// task's tags name: a consumer reading the catalog resolves them against the
// task's own fields.
func TestObjectEffectsNameRealFields(t *testing.T) {
	t.Parallel()
	boolType := reflect.TypeOf(false)
	boolPtrType := reflect.TypeOf((*bool)(nil))
	for typeKey, task := range allRegisteredTasks() {
		docer, ok := task.(ObjectEffectsDocer)
		if !ok {
			continue
		}
		effects := docer.ObjectEffects()
		typ := taskStructOf(task)
		fields := map[string]reflect.StructField{}
		for i := 0; i < typ.NumField(); i++ {
			fields[yamlFieldName(typ.Field(i))] = typ.Field(i)
		}
		kinds := map[ObjectKind]bool{}
		for _, object := range TaskObjects(task) {
			kinds[object.Kind] = true
		}
		for _, kind := range effects.Creates {
			if !kinds[kind] {
				t.Errorf("%s creates %s but tags no field naming one", typeKey, kind)
			}
		}
		for _, restart := range effects.Restarts {
			if !kinds[restart.Kind] {
				t.Errorf("%s restarts %s but tags no field naming one", typeKey, restart.Kind)
			}
			for _, key := range append(slices.Clone(restart.When), restart.Unless...) {
				field, ok := fields[key]
				if !ok || (field.Type != boolType && field.Type != boolPtrType) {
					t.Errorf("%s restart condition %q is not a bool recipe key of the task", typeKey, key)
				}
			}
			stateField, ok := fields["state"]
			for _, state := range restart.States {
				if !ok || !slices.Contains(strings.Split(stateField.Tag.Get("options"), ","), string(state)) {
					t.Errorf("%s restart state %q is not one of the task's states", typeKey, state)
				}
			}
		}
	}
}

// TestTaskObjectsKeysAndValues pins how an object is read off a task: keys in
// the kind's role order whatever order the fields are declared in, values
// from the task, and an object left unset by `global: true`.
func TestTaskObjectsKeysAndValues(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		task Task
		want []TaskObject
	}{
		{
			name: "app task",
			task: ConfigTask{App: "api"},
			want: []TaskObject{{Kind: ObjectApp, Keys: []string{"app"}, Values: []string{"api"}, Set: true, Restarts: &Restart{When: []string{"restart"}}}},
		},
		{
			name: "acl_service declares the name first",
			task: AclServiceTask{Service: "db", Type: "redis"},
			want: []TaskObject{{Kind: ObjectService, Keys: []string{"type", "service"}, Values: []string{"redis", "db"}, Set: true}},
		},
		{
			name: "service link changes both",
			task: ServiceLinkTask{App: "api", Service: "redis", Name: "db"},
			want: []TaskObject{
				{Kind: ObjectApp, Keys: []string{"app"}, Values: []string{"api"}, Set: true, Restarts: &Restart{}},
				{Kind: ObjectService, Keys: []string{"service", "name"}, Values: []string{"redis", "db"}, Set: true},
			},
		},
		{
			name: "global property names no app",
			task: NginxPropertyTask{Global: true},
			want: []TaskObject{{Kind: ObjectApp, Keys: []string{"app"}, Values: []string{""}}},
		},
		{
			name: "pointer task",
			task: &ServiceCreateTask{Service: "redis", Name: "db"},
			want: []TaskObject{{Kind: ObjectService, Keys: []string{"service", "name"}, Values: []string{"redis", "db"}, Set: true, Creates: true, Restarts: &Restart{}}},
		},
		{
			name: "global task",
			task: PluginTask{Name: "redis"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := TaskObjects(tc.task); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("TaskObjects = %+v, want %+v", got, tc.want)
			}
		})
	}
}
