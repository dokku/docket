package tasks

import (
	"fmt"
	"reflect"
	"strings"
)

// Field constraints are the rules about which recipe keys a task needs, and
// which it refuses, beyond the unconditional `required:"true"`. They used to
// live only in each task's hand-written Validate() and in description prose,
// so a consumer such as a Terraform provider could find them only by running
// Validate(), which stops at the first violation (#620).
//
// Now they are declared, and the declarations are what enforces them: one
// checker reads the same tags and Constraints() groups the catalog publishes,
// so the published rules and the enforced ones cannot drift apart. A task's
// Validate() keeps only the rules that depend on a value rather than on
// whether a key is set at all - enums, formats, property tables.
//
// Two declarations live on a field, as struct tags naming states from the
// task's `state` choices:
//
//	required_when:"present,absent"  the field must be set in these states
//	forbidden_when:"clear"          the field must not be set in these states
//
// Rules over several fields are Constraint values a task, or an element
// struct, returns from Constraints().

// Field tags the checker and the catalog read.
const (
	requiredWhenTag  = "required_when"
	forbiddenWhenTag = "forbidden_when"
)

// ConstraintKind names one kind of multi-field rule. The values are the
// strings the catalog publishes.
type ConstraintKind string

const (
	// ConstraintExactlyOneOf requires exactly one of Fields to be set.
	ConstraintExactlyOneOf ConstraintKind = "exactly_one_of"

	// ConstraintAtLeastOneOf requires one or more of Fields to be set.
	ConstraintAtLeastOneOf ConstraintKind = "at_least_one_of"

	// ConstraintAllOrNoneOf requires Fields to be set together or not at
	// all.
	ConstraintAllOrNoneOf ConstraintKind = "all_or_none_of"

	// ConstraintConflictsWith refuses any of Fields while Field is set.
	ConstraintConflictsWith ConstraintKind = "conflicts_with"

	// ConstraintRequiredWith requires every one of Fields while Field is set.
	ConstraintRequiredWith ConstraintKind = "required_with"
)

// ConstraintKinds lists every kind, in the order the catalog documents them.
var ConstraintKinds = []ConstraintKind{
	ConstraintExactlyOneOf,
	ConstraintAtLeastOneOf,
	ConstraintAllOrNoneOf,
	ConstraintConflictsWith,
	ConstraintRequiredWith,
}

// Directional reports whether the kind hangs off one Field rather than
// treating Fields as a symmetric group.
func (k ConstraintKind) Directional() bool {
	return k == ConstraintConflictsWith || k == ConstraintRequiredWith
}

// Constraint is one rule over several recipe keys of a task or of an element
// struct. Every name in it is a recipe key, never a Go field name. The Go
// field order is the key order of the emitted JSON.
type Constraint struct {
	// Kind is what the rule requires.
	Kind ConstraintKind `json:"kind"`

	// Field is the key the rule hangs off. Present only for conflicts_with
	// and required_with.
	Field string `json:"field,omitempty"`

	// Fields are the group, or for a directional kind the keys Field
	// conflicts with or requires.
	Fields []string `json:"fields"`

	// States are the task states the rule applies in. Absent means every
	// state. On an element struct's constraint they are still the task's
	// states.
	States []State `json:"states,omitempty"`
}

// ConstraintDocer is implemented by a task, or an element struct, that
// declares multi-field rules.
type ConstraintDocer interface {
	Constraints() []Constraint
}

// TaskConstraints returns the multi-field rules v declares: a task, an
// element struct, or a pointer to either. Nil when it declares none.
func TaskConstraints(v any) []Constraint {
	if d, ok := v.(ConstraintDocer); ok {
		return d.Constraints()
	}
	rv := reflect.ValueOf(v)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
		if d, ok := rv.Interface().(ConstraintDocer); ok {
			return d.Constraints()
		}
	}
	return nil
}

// structConstraints returns the rules rt declares, from its zero value. The
// catalog reads element structs this way, since it has a type rather than an
// instance.
func structConstraints(rt reflect.Type) []Constraint {
	if rt == nil || rt.Kind() != reflect.Struct {
		return nil
	}
	return TaskConstraints(reflect.Zero(rt).Interface())
}

// cloneConstraints deep-copies constraints, so a caller holding a catalog
// cannot rewrite the shared declarations - appOrGlobalConstraints among them -
// that every later check reads.
func cloneConstraints(constraints []Constraint) []Constraint {
	if len(constraints) == 0 {
		return nil
	}
	out := make([]Constraint, len(constraints))
	for i, c := range constraints {
		c.Fields = append([]string(nil), c.Fields...)
		if c.States != nil {
			c.States = append([]State(nil), c.States...)
		}
		out[i] = c
	}
	return out
}

// appliesIn reports whether c is checked in state.
func (c Constraint) appliesIn(state State) bool {
	if len(c.States) == 0 {
		return true
	}
	for _, s := range c.States {
		if s == state {
			return true
		}
	}
	return false
}

// appOrGlobalConstraints is the scoping rule every task that takes either an
// app or the --global flag shares.
var appOrGlobalConstraints = []Constraint{
	{Kind: ConstraintExactlyOneOf, Fields: []string{"app", "global"}},
}

// Violation is one broken constraint. Field is the path of the key at fault,
// such as `username` or `users[1].hash`; for a group rule, which no single key
// breaks, it is the path of the struct the group sits on - `users[1]` for an
// element, empty for the task itself.
type Violation struct {
	Field   string
	Message string
}

// Error makes a Violation usable as the error a Validate() returns.
func (v Violation) Error() string {
	return v.Message
}

// ConstraintViolations returns every declared constraint task breaks, in
// field declaration order, then the task's group rules, then each element of
// its structured lists in turn. Nil when the task satisfies all of them.
//
// A property task naming a property another task manages is the one
// exception: that is reported alone, because the user wrote a name this task
// will never manage, and naming the task that does is more use than telling
// them the app field is missing too.
func ConstraintViolations(task any) []Violation {
	rv := reflect.ValueOf(task)
	for rv.IsValid() && rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}
		rv = rv.Elem()
	}
	if !rv.IsValid() || rv.Kind() != reflect.Struct {
		return nil
	}
	if err := rejectedPropertyError(rv); err != nil {
		return []Violation{{Field: PropertyFieldName, Message: err.Error()}}
	}
	return structViolations(rv, taskState(rv), "")
}

// checkConstraints returns the first constraint task breaks, or nil. It is
// what every Validate() and gatePlan start with.
func checkConstraints(task any) error {
	if violations := ConstraintViolations(task); len(violations) > 0 {
		return violations[0]
	}
	return nil
}

// rejectedPropertyError returns the rejection for a property task whose
// `property` belongs to a family another task manages.
func rejectedPropertyError(rv reflect.Value) error {
	docer, ok := rv.Interface().(PropertyTableDocer)
	if !ok {
		return nil
	}
	property, ok := fieldByRecipeKey(rv, PropertyFieldName)
	if !ok || property.Kind() != reflect.String {
		return nil
	}
	if family, ok := docer.propertyTable().rejectedFamilyFor(property.String()); ok {
		return family.err()
	}
	return nil
}

// taskState is the state rv's constraints are checked in: its `state` key,
// the field's default when that is empty, and present when there is no
// default either - which is how every task's Plan reads an empty state. A
// struct with no state key has no state, so a state-scoped rule never applies
// to it; TestConstraintDeclarationsAreWellFormed keeps one from being declared.
func taskState(rv reflect.Value) State {
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if field.PkgPath != "" || catalogYAMLName(field.Tag, field.Name) != "state" {
			continue
		}
		if value := rv.Field(i); value.Kind() == reflect.String && value.String() != "" {
			return State(value.String())
		}
		if def := field.Tag.Get("default"); def != "" {
			return State(def)
		}
		return StatePresent
	}
	return ""
}

// structViolations checks one task or element struct. prefix qualifies every
// key it names: empty for the task, `users[1].` for an element.
func structViolations(rv reflect.Value, state State, prefix string) []Violation {
	var violations []Violation
	rt := rv.Type()

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		name := recipeKey(field)
		if name == "" {
			continue
		}
		path := prefix + name
		set := fieldIsSet(rv.Field(i), field.Tag.Get("default"))

		// A task's own required keys are the loader's to check - docket
		// validate reports them as missing_required_field, and a read builds
		// a task from its identity keys alone - but nothing else checks an
		// element's, so an element's are checked here.
		if prefix != "" && !set && field.Tag.Get("required") == "true" && field.Tag.Get("default") == "" {
			violations = append(violations, Violation{Field: path, Message: fmt.Sprintf("'%s' is required", path)})
			continue
		}
		if !set && stateListed(field.Tag.Get(requiredWhenTag), state) {
			violations = append(violations, Violation{Field: path, Message: fmt.Sprintf("'%s' is required for state '%s'", path, state)})
		}
		if set && stateListed(field.Tag.Get(forbiddenWhenTag), state) {
			violations = append(violations, Violation{Field: path, Message: fmt.Sprintf("'%s' must not be set for state '%s'", path, state)})
		}
	}

	for _, constraint := range TaskConstraints(rv.Interface()) {
		if constraint.appliesIn(state) {
			violations = append(violations, constraint.violations(rv, state, prefix)...)
		}
	}

	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		name := recipeKey(field)
		if name == "" {
			continue
		}
		list := rv.Field(i)
		if list.Kind() != reflect.Slice && list.Kind() != reflect.Array {
			continue
		}
		if structElem(list.Type().Elem()) == nil {
			continue
		}
		for j := 0; j < list.Len(); j++ {
			elem := list.Index(j)
			for elem.Kind() == reflect.Pointer {
				if elem.IsNil() {
					break
				}
				elem = elem.Elem()
			}
			if elem.Kind() != reflect.Struct {
				continue
			}
			violations = append(violations, structViolations(elem, state, fmt.Sprintf("%s%s[%d].", prefix, name, j))...)
		}
	}

	return violations
}

// violations checks c against rv, whose keys prefix qualifies.
func (c Constraint) violations(rv reflect.Value, state State, prefix string) []Violation {
	isSet := func(name string) bool {
		value, ok := fieldByRecipeKey(rv, name)
		if !ok {
			return false
		}
		return fieldIsSet(value, recipeKeyDefault(rv.Type(), name))
	}
	qualified := func(names []string) []string {
		out := make([]string, len(names))
		for i, name := range names {
			out[i] = prefix + name
		}
		return out
	}
	group := strings.TrimSuffix(prefix, ".")
	when := ""
	if len(c.States) > 0 {
		when = fmt.Sprintf(" for state '%s'", state)
	}

	var set []string
	for _, name := range c.Fields {
		if isSet(name) {
			set = append(set, name)
		}
	}

	switch c.Kind {
	case ConstraintExactlyOneOf:
		switch {
		case len(set) == 0:
			return []Violation{{Field: group, Message: fmt.Sprintf("exactly one of %s is required%s", quotedList(qualified(c.Fields), "or"), when)}}
		case len(set) > 1:
			return []Violation{{Field: group, Message: fmt.Sprintf("%s are mutually exclusive", quotedList(qualified(set), "and"))}}
		}
	case ConstraintAtLeastOneOf:
		if len(set) == 0 {
			return []Violation{{Field: group, Message: fmt.Sprintf("at least one of %s is required%s", quotedList(qualified(c.Fields), "or"), when)}}
		}
	case ConstraintAllOrNoneOf:
		if len(set) > 0 && len(set) < len(c.Fields) {
			return []Violation{{Field: group, Message: fmt.Sprintf("%s must be set together%s", quotedList(qualified(c.Fields), "and"), when)}}
		}
	case ConstraintConflictsWith:
		if !isSet(c.Field) {
			return nil
		}
		var out []Violation
		for _, name := range set {
			out = append(out, Violation{Field: prefix + c.Field, Message: fmt.Sprintf("'%s' and '%s' are mutually exclusive", prefix+c.Field, prefix+name)})
		}
		return out
	case ConstraintRequiredWith:
		if !isSet(c.Field) {
			return nil
		}
		var out []Violation
		for _, name := range c.Fields {
			if !isSet(name) {
				out = append(out, Violation{Field: prefix + name, Message: fmt.Sprintf("'%s' is required when '%s' is set%s", prefix+name, prefix+c.Field, when)})
			}
		}
		return out
	}
	return nil
}

// fieldIsSet reports whether a recipe supplied value. A nil pointer is unset,
// and a pointer is otherwise read through, so an explicit `clear_before:
// false` is unset too. A list or map is set when it has an element. Anything
// else is set when it is neither its zero value nor its default, because the
// loader fills a default in before any check runs and a recipe writing the
// default cannot be told apart from one that left the key out. A
// whitespace-only string is set: a task that refuses one says so itself.
func fieldIsSet(value reflect.Value, def string) bool {
	for value.Kind() == reflect.Pointer || value.Kind() == reflect.Interface {
		if value.IsNil() {
			return false
		}
		value = value.Elem()
	}
	switch value.Kind() {
	case reflect.Slice, reflect.Map:
		return value.Len() > 0
	}
	if value.IsZero() {
		return false
	}
	if def != "" && fmt.Sprint(value.Interface()) == def {
		return false
	}
	return true
}

// stateListed reports whether state is one of the comma-separated states in a
// required_when or forbidden_when tag.
func stateListed(tag string, state State) bool {
	if tag == "" || state == "" {
		return false
	}
	for _, s := range strings.Split(tag, ",") {
		if State(s) == state {
			return true
		}
	}
	return false
}

// conditionalStates splits a required_when or forbidden_when tag into the
// states the catalog publishes. Nil for an empty tag.
func conditionalStates(tag string) []string {
	if tag == "" {
		return nil
	}
	return strings.Split(tag, ",")
}

// recipeKey is a field's recipe key, or "" for an unexported field or one a
// recipe cannot set.
func recipeKey(field reflect.StructField) string {
	if field.PkgPath != "" {
		return ""
	}
	return catalogYAMLName(field.Tag, field.Name)
}

// fieldByRecipeKey returns rv's field whose recipe key is name.
func fieldByRecipeKey(rv reflect.Value, name string) (reflect.Value, bool) {
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		if recipeKey(rt.Field(i)) == name {
			return rv.Field(i), true
		}
	}
	return reflect.Value{}, false
}

// recipeKeyDefault returns the default tag of rt's field whose recipe key is
// name.
func recipeKeyDefault(rt reflect.Type, name string) string {
	for i := 0; i < rt.NumField(); i++ {
		if recipeKey(rt.Field(i)) == name {
			return rt.Field(i).Tag.Get("default")
		}
	}
	return ""
}

// quotedList renders names as `'a'`, `'a' or 'b'`, or `'a', 'b' or 'c'`.
func quotedList(names []string, conjunction string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = "'" + name + "'"
	}
	if len(quoted) <= 1 {
		return strings.Join(quoted, "")
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " " + conjunction + " " + quoted[len(quoted)-1]
}
