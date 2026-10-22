package tasks

import "strings"

// serializeTag is the struct tag naming the object a field identifies: the app
// or the datastore service a task changes. Its value is the object kind, and
// for a kind identified by more than one field, the field's role within it:
// `serialize:"app"`, `serialize:"service.type"`, `serialize:"service.name"`.
const serializeTag = "serialize"

// ObjectKind is a kind of server object a task changes. Concurrent tasks on
// the same object are serialized, and the catalog publishes which tasks
// create and restart one.
type ObjectKind string

// Object kinds, as TaskObject.Kind.
//
//sdk:export
const (
	// ObjectApp is a dokku app, identified by its name.
	ObjectApp ObjectKind = "app"

	// ObjectService is a datastore service, identified by its datastore type
	// and its name.
	ObjectService ObjectKind = "service"
)

// objectRoles are the roles of the fields identifying each kind, in the order
// their values make up the object's identity. A kind named by one field has
// the single empty role. The order is fixed by the kind rather than by field
// declaration order, so dokku_acl_service - which declares the instance name
// before the datastore type - names the same service as dokku_service_property.
var objectRoles = map[ObjectKind][]string{
	ObjectApp:     {""},
	ObjectService: {"type", "name"},
}

// objectKinds is every kind, in the order TaskObjects returns them and the
// order their locks are taken.
var objectKinds = []ObjectKind{ObjectApp, ObjectService}

// Restart says when applying a task restarts or redeploys an object. Every
// condition must hold: one of When is true (when When is set), none of Unless
// is true, and the task's state is one of States (when States is set). An
// empty Restart means the task can restart the object whenever it applies.
type Restart struct {
	// When lists recipe keys of bool fields. The task restarts the object
	// only when at least one of them is true. An omitted key takes the
	// field's default.
	When []string `json:"when,omitempty"`

	// Unless lists recipe keys of bool fields. A true value for any of them
	// stops the restart.
	Unless []string `json:"unless,omitempty"`

	// States lists the values of the task's state field in which it restarts
	// the object. Empty means every state.
	States []State `json:"states,omitempty"`
}

// ObjectRestart is one object kind a task can restart, and when.
type ObjectRestart struct {
	Kind ObjectKind
	Restart
}

// ObjectEffects is what applying a task does to its objects beyond changing
// them: whether it creates one, and whether it restarts or redeploys one.
//
//sdk:export
type ObjectEffects struct {
	// Creates lists the kinds the task creates, and destroys in its absent
	// state. Every other task on the same object depends on it: a consumer
	// applies it before them and tears it down after them.
	Creates []ObjectKind

	// Restarts lists the kinds the task can restart or redeploy.
	Restarts []ObjectRestart
}

// ObjectEffectsDocer is an optional interface a task implements to declare
// that it creates or restarts the objects its serialize-tagged fields name.
// A task that implements nothing only changes them.
type ObjectEffectsDocer interface {
	ObjectEffects() ObjectEffects
}

// TaskObject is one object a task changes.
//
//sdk:export
type TaskObject struct {
	// Kind is what the object is.
	Kind ObjectKind

	// Keys are the recipe keys that identify it, in the kind's role order:
	// `app`, or the datastore type's key then the name's key.
	Keys []string

	// Values are the values Keys hold on the task, in the same order.
	Values []string

	// Set is true when every one of Values is non-empty. An object that is not
	// set names nothing - an app-or-global task with `global: true` - and takes
	// no lock.
	Set bool

	// Creates is true when the task creates the object.
	Creates bool

	// Restarts is non-nil when the task can restart or redeploy the object,
	// and says when.
	Restarts *Restart
}

// TaskObjects returns the app and the datastore service t changes, app first:
// the recipe keys naming each and the values t holds for them, whether t
// creates it, and when t restarts or redeploys it. The catalog publishes the
// same data as TaskSchema.Objects. Nil when t changes neither.
//
//sdk:export
func TaskObjects(t Task) []TaskObject {
	// Centralised so the catalog, the docs generator, the lock table and the sdk
	// share one read site.
	v, ok := structValue(t)
	if !ok {
		return nil
	}
	typ := v.Type()

	type roleField struct {
		key   string
		value string
	}
	byKind := map[ObjectKind]map[string]roleField{}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		tag, ok := field.Tag.Lookup(serializeTag)
		if !ok {
			continue
		}
		kind, role, _ := strings.Cut(tag, ".")
		value, _ := renderIdentityValue(v.Field(i))
		if byKind[ObjectKind(kind)] == nil {
			byKind[ObjectKind(kind)] = map[string]roleField{}
		}
		byKind[ObjectKind(kind)][role] = roleField{key: yamlFieldName(field), value: value}
	}

	var effects ObjectEffects
	if docer, ok := t.(ObjectEffectsDocer); ok {
		effects = docer.ObjectEffects()
	}

	var out []TaskObject
	for _, kind := range objectKinds {
		fields, ok := byKind[kind]
		if !ok {
			continue
		}
		object := TaskObject{Kind: kind, Set: true}
		for _, role := range objectRoles[kind] {
			field := fields[role]
			object.Keys = append(object.Keys, field.key)
			object.Values = append(object.Values, field.value)
			if field.value == "" {
				object.Set = false
			}
		}
		for _, created := range effects.Creates {
			if created == kind {
				object.Creates = true
			}
		}
		for _, restart := range effects.Restarts {
			if restart.Kind == kind {
				r := restart.Restart
				object.Restarts = &r
			}
		}
		out = append(out, object)
	}
	return out
}
