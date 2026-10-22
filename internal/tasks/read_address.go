package tasks

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// addressReader is implemented by a task whose identity keys alone do not make
// a task that plans a read of the whole resource - one that requires a payload
// in every state, or whose states all read only the members they name. The
// receiver holds its identity keys and its defaults and nothing else.
// readAddress returns the resource those keys name, and whether it exists,
// as ReadAddress does. ReadAddress checks the version gate before calling it,
// so it need not.
type addressReader interface {
	readAddress(ctx context.Context) (Task, bool, error)
}

// ReadAddress returns the resource an identity address names, such as
// `dokku_buildpacks[app=api]`, as it exists on the server, and whether it
// exists, from the address alone; nothing is applied. It reads every member
// the resource has, the way a task in state set or clear would own them: a
// dokku_config leaves out the keys docket keeps (linked-service DSNs,
// NO_VHOST, the git rev env var), and a dokku_ps_scale leaves out process
// types scaled to zero. The returned task and errors are ReadTask's: the value
// type registered for the task, the *VersionError when ctx carries a gate the
// server is behind, an *ObjectMissingError when the app or service the
// resource depends on is missing, and an error wrapping ErrStateUnreadable for
// a task whose probe cannot read its state.
//
// The address must name one resource. A bare type, a key the task does not
// declare, or a missing required key is an error, raised before the server is
// contacted. IdentityAddress renders the address of a task you hold.
//
//sdk:export
func ReadAddress(ctx context.Context, address string) (Task, bool, error) {
	typeKey, task, err := taskFromAddress(address)
	if err != nil {
		return nil, false, err
	}
	if support, _ := TaskProbeSupport(task); support.Status == ProbeUnsupported {
		return nil, false, fmt.Errorf("%w: %s cannot read its state", ErrStateUnreadable, typeKey)
	}
	if err := CheckMinimumVersions(ctx, typeKey, task); err != nil {
		return nil, false, err
	}

	if reader, ok := task.(addressReader); ok {
		got, exists, err := reader.readAddress(ctx)
		return readResult(ctx, task, got, exists, err)
	}
	setReadState(task)
	return planRead(ctx, task, true)
}

// taskFromAddress parses address and builds the task it names: a new task of
// its type, with defaults applied, carrying the address's keys.
func taskFromAddress(address string) (string, Task, error) {
	typeKey, keys, err := ParseIdentityAddress(address)
	if err != nil {
		return "", nil, err
	}
	prototype, ok := Lookup(typeKey)
	if !ok {
		msg := fmt.Sprintf("unknown task type %q in resource address %q", typeKey, address)
		if near := nearestEnvelopeOrTaskKey(typeKey); near != "" {
			msg += fmt.Sprintf(" (did you mean %q?)", near)
		}
		return "", nil, errors.New(msg)
	}
	if len(keys) == 0 {
		return "", nil, fmt.Errorf("resource address %q names no keys, so it is every resource of %s rather than one (keys: %s)",
			address, typeKey, strings.Join(sortedIdentityKeyNames(prototype), ", "))
	}

	task, err := NewTask(typeKey)
	if err != nil {
		return "", nil, err
	}
	v := reflect.ValueOf(task).Elem()
	declared := map[string]reflect.StructField{}
	for _, key := range TaskIdentity(task) {
		field, _ := v.Type().FieldByName(key.Field)
		declared[key.YAMLName] = field
	}
	// Keys are checked in a fixed order - the address's by name, the task's
	// in declaration order - so an address with several bad keys always
	// reports the same one.
	for _, name := range sortedPairKeys(keys) {
		raw := keys[name]
		field, ok := declared[name]
		if !ok {
			return "", nil, fmt.Errorf("resource address %q: %q is not an identity key of %s (keys: %s)",
				address, name, typeKey, strings.Join(sortedIdentityKeyNames(prototype), ", "))
		}
		if err := setIdentityValue(v.FieldByIndex(field.Index), raw); err != nil {
			return "", nil, fmt.Errorf("resource address %q: %s: %w", address, name, err)
		}
	}
	for _, key := range TaskIdentity(task) {
		if _, ok := keys[key.YAMLName]; !ok && declared[key.YAMLName].Tag.Get("required") == "true" {
			return "", nil, fmt.Errorf("resource address %q: missing required key %q of %s", address, key.YAMLName, typeKey)
		}
	}
	return typeKey, task, nil
}

// setIdentityValue sets one key field from its address spelling. Every task
// key is a string or a bool.
func setIdentityValue(field reflect.Value, raw string) error {
	switch field.Kind() {
	case reflect.String:
		field.SetString(raw)
	case reflect.Bool:
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("%q is not true or false", raw)
		}
		field.SetBool(b)
	default:
		return fmt.Errorf("a %s key cannot be read from an address", field.Kind())
	}
	return nil
}

// setReadState sets the state a task built from keys alone plans a whole read
// in: clear, which reads every member a collection owns; otherwise absent,
// which reads a property known only by its keys, a list declared empty, or
// whether a single resource is there; otherwise the default.
func setReadState(task Task) {
	v := reflect.ValueOf(task).Elem()
	field, ok := v.Type().FieldByName("State")
	if !ok {
		return
	}
	options := strings.Split(field.Tag.Get("options"), ",")
	for _, want := range []State{StateClear, StateAbsent} {
		for _, option := range options {
			if State(option) == want {
				v.FieldByIndex(field.Index).SetString(string(want))
				return
			}
		}
	}
}
