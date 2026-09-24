package tasks

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"github.com/gobuffalo/flect"
)

// registeredTasks maps each registry type-key to its task's struct type.
//
// It holds types rather than prototype instances so there is no shared value
// for anything - in this package or out of it - to mutate: every task the
// registry hands out is allocated for that caller. Reach it through Lookup,
// TaskTypes and NewTask (#565).
var registeredTasks map[string]reflect.Type

// RegisterTask registers a task under `dokku_` plus its type name in
// snake_case with the `_task` suffix dropped, so &AppTask{} becomes dokku_app.
func RegisterTask(t Task) {
	if len(registeredTasks) == 0 {
		registeredTasks = make(map[string]reflect.Type)
	}

	var name string
	structType := reflect.TypeOf(t)
	if structType.Kind() == reflect.Ptr {
		structType = structType.Elem()
		name = "*" + structType.Name()
	} else {
		name = structType.Name()
	}

	name = flect.Underscore(name)
	registeredTasks[fmt.Sprintf("dokku_%s", strings.TrimSuffix(name, "_task"))] = structType
}

// registeredType returns the struct type registered under typeKey. It is the
// presence check for callers that do not need an instance.
func registeredType(typeKey string) (reflect.Type, bool) {
	t, ok := registeredTasks[typeKey]
	return t, ok
}

// Lookup reports whether typeKey is a registered task type and, when it is,
// returns a zero-value instance of it for reading the task's metadata - its
// synopsis, identity keys, export support and so on.
//
// Every call allocates, so a caller may do anything with the result without
// affecting the registry or any other caller. The instance has no `default:`
// tags applied; use NewTask for a task to set fields on and run.
func Lookup(typeKey string) (Task, bool) {
	t, ok := registeredType(typeKey)
	if !ok {
		return nil, false
	}
	task, ok := reflect.New(t).Interface().(Task)
	return task, ok
}

// TaskTypes returns every registered task type, sorted. The result is a fresh
// slice, so a caller may sort, filter or truncate it without disturbing the
// registry.
func TaskTypes() []string {
	names := make([]string, 0, len(registeredTasks))
	for name := range registeredTasks {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
