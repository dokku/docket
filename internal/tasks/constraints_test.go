package tasks

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// constraintFixture exercises every rule the checker enforces, on a struct
// small enough to read its expected violations off.
type constraintFixture struct {
	// Name is required, but a task's own required keys are the loader's to
	// check, so the checker leaves it alone.
	Name   string                   `required:"true" yaml:"name"`
	Value  string                   `required_when:"present" forbidden_when:"absent" yaml:"value"`
	Mode   string                   `default:"auto" forbidden_when:"absent" yaml:"mode"`
	Flag   *bool                    `forbidden_when:"clear" yaml:"flag"`
	List   []string                 `required_when:"present" ordered:"false" yaml:"list"`
	Pairs  map[string]string        `forbidden_when:"clear" yaml:"pairs"`
	Left   string                   `yaml:"left"`
	Right  string                   `yaml:"right"`
	Source string                   `yaml:"source"`
	Target string                   `yaml:"target"`
	Items  []constraintFixtureEntry `ordered:"false" yaml:"items"`
	State  State                    `default:"present" options:"present,absent,clear" yaml:"state"`
}

func (constraintFixture) Constraints() []Constraint {
	return []Constraint{
		{Kind: ConstraintExactlyOneOf, Fields: []string{"left", "right"}},
		{Kind: ConstraintRequiredWith, Field: "source", Fields: []string{"target"}, States: []State{StatePresent}},
	}
}

type constraintFixtureEntry struct {
	Key   string `required:"true" yaml:"key"`
	One   string `yaml:"one"`
	Other string `yaml:"other"`
}

func (constraintFixtureEntry) Constraints() []Constraint {
	return []Constraint{
		{Kind: ConstraintConflictsWith, Field: "one", Fields: []string{"other"}},
		{Kind: ConstraintAtLeastOneOf, Fields: []string{"one", "other"}, States: []State{StatePresent}},
	}
}

func TestConstraintViolationsReportsEveryRule(t *testing.T) {
	t.Parallel()
	got := ConstraintViolations(constraintFixture{
		Left:   "l",
		Right:  "r",
		Source: "s",
		Items: []constraintFixtureEntry{
			{Key: "a", One: "1", Other: "2"},
			{},
		},
	})
	want := []Violation{
		{Field: "value", Message: "'value' is required for state 'present'"},
		{Field: "list", Message: "'list' is required for state 'present'"},
		{Field: "", Message: "'left' and 'right' are mutually exclusive"},
		{Field: "target", Message: "'target' is required when 'source' is set for state 'present'"},
		{Field: "items[0].one", Message: "'items[0].one' and 'items[0].other' are mutually exclusive"},
		{Field: "items[1].key", Message: "'items[1].key' is required"},
		{Field: "items[1]", Message: "at least one of 'items[1].one' or 'items[1].other' is required for state 'present'"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ConstraintViolations =\n%s\nwant\n%s", formatViolations(got), formatViolations(want))
	}
}

func TestConstraintViolationsForbiddenWhen(t *testing.T) {
	t.Parallel()
	got := ConstraintViolations(constraintFixture{
		Value: "v",
		Mode:  "manual",
		Left:  "l",
		State: StateAbsent,
	})
	want := []Violation{
		{Field: "value", Message: "'value' must not be set for state 'absent'"},
		{Field: "mode", Message: "'mode' must not be set for state 'absent'"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ConstraintViolations =\n%s\nwant\n%s", formatViolations(got), formatViolations(want))
	}
}

func TestConstraintViolationsSatisfied(t *testing.T) {
	t.Parallel()
	task := constraintFixture{
		Value:  "v",
		List:   []string{"a"},
		Left:   "l",
		Source: "s",
		Target: "t",
		Items:  []constraintFixtureEntry{{Key: "a", One: "1"}},
	}
	if got := ConstraintViolations(task); got != nil {
		t.Errorf("ConstraintViolations = %s, want none", formatViolations(got))
	}
	if err := checkConstraints(&task); err != nil {
		t.Errorf("checkConstraints(&task) = %v, want nil", err)
	}
}

// TestConstraintViolationsSetSemantics pins what counts as set: a nil pointer
// and a pointer to a zero value are unset, a value equal to its default is
// unset, an empty but non-nil list or map is unset, and a whitespace string is
// set.
func TestConstraintViolationsSetSemantics(t *testing.T) {
	t.Parallel()
	falseValue, trueValue := false, true
	cases := []struct {
		name string
		task constraintFixture
		want []string
	}{
		{"nil pointer is unset", constraintFixture{Left: "l", State: StateClear}, nil},
		{"pointer to false is unset", constraintFixture{Left: "l", Flag: &falseValue, State: StateClear}, nil},
		{"pointer to true is set", constraintFixture{Left: "l", Flag: &trueValue, State: StateClear}, []string{"'flag' must not be set for state 'clear'"}},
		{"value equal to its default is unset", constraintFixture{Left: "l", Mode: "auto", State: StateAbsent}, nil},
		{"empty map is unset", constraintFixture{Left: "l", Pairs: map[string]string{}, State: StateClear}, nil},
		{"map with an entry is set", constraintFixture{Left: "l", Pairs: map[string]string{"k": "v"}, State: StateClear}, []string{"'pairs' must not be set for state 'clear'"}},
		{"empty list is unset", constraintFixture{Left: "l", Value: "v", List: []string{}}, []string{"'list' is required for state 'present'"}},
		{"whitespace is set", constraintFixture{Left: "l", Value: " ", List: []string{"a"}}, nil},
		{"empty state reads as the default", constraintFixture{Left: "l"}, []string{"'value' is required for state 'present'", "'list' is required for state 'present'"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, v := range ConstraintViolations(tc.task) {
				got = append(got, v.Message)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("messages = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestConstraintMessages pins the sentence each kind renders.
func TestConstraintMessages(t *testing.T) {
	t.Parallel()
	type pair struct {
		A string `yaml:"a"`
		B string `yaml:"b"`
		C string `yaml:"c"`
	}
	value := func(a, b, c string) reflect.Value { return reflect.ValueOf(pair{a, b, c}) }
	cases := []struct {
		name       string
		constraint Constraint
		value      reflect.Value
		want       []string
	}{
		{"exactly one, none", Constraint{Kind: ConstraintExactlyOneOf, Fields: []string{"a", "b", "c"}}, value("", "", ""), []string{"exactly one of 'a', 'b' or 'c' is required"}},
		{"exactly one, two", Constraint{Kind: ConstraintExactlyOneOf, Fields: []string{"a", "b", "c"}}, value("1", "", "1"), []string{"'a' and 'c' are mutually exclusive"}},
		{"exactly one, one", Constraint{Kind: ConstraintExactlyOneOf, Fields: []string{"a", "b"}}, value("1", "", ""), nil},
		{"at least one, none", Constraint{Kind: ConstraintAtLeastOneOf, Fields: []string{"a", "b"}, States: []State{StatePresent}}, value("", "", ""), []string{"at least one of 'a' or 'b' is required for state 'present'"}},
		{"all or none, some", Constraint{Kind: ConstraintAllOrNoneOf, Fields: []string{"a", "b"}}, value("1", "", ""), []string{"'a' and 'b' must be set together"}},
		{"all or none, all", Constraint{Kind: ConstraintAllOrNoneOf, Fields: []string{"a", "b"}}, value("1", "1", ""), nil},
		{"conflicts, each target", Constraint{Kind: ConstraintConflictsWith, Field: "a", Fields: []string{"b", "c"}}, value("1", "1", "1"), []string{"'a' and 'b' are mutually exclusive", "'a' and 'c' are mutually exclusive"}},
		{"conflicts, field unset", Constraint{Kind: ConstraintConflictsWith, Field: "a", Fields: []string{"b"}}, value("", "1", ""), nil},
		{"required with, each target", Constraint{Kind: ConstraintRequiredWith, Field: "a", Fields: []string{"b", "c"}}, value("1", "", ""), []string{"'b' is required when 'a' is set", "'c' is required when 'a' is set"}},
		{"required with, field unset", Constraint{Kind: ConstraintRequiredWith, Field: "a", Fields: []string{"b"}}, value("", "", ""), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var got []string
			for _, v := range tc.constraint.violations(nil, tc.value, StatePresent, "") {
				got = append(got, v.Message)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("messages = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestConstraintViolationsRejectedPropertyOutranksEverything pins that a
// property another task manages is the only problem reported, whatever else
// is wrong with the body (#458).
func TestConstraintViolationsRejectedPropertyOutranksEverything(t *testing.T) {
	t.Parallel()
	got := ConstraintViolations(SchedulerK3sPropertyTask{Property: "chart.traefik.replicas", State: StatePresent})
	if len(got) != 1 || !strings.Contains(got[0].Message, "managed by dokku_scheduler_k3s_chart") {
		t.Errorf("ConstraintViolations = %s, want only the rejected-family error", formatViolations(got))
	}
}

// TestCloneConstraintsIsDeep pins that a catalog caller cannot rewrite the
// shared declarations through the slices it was handed.
func TestCloneConstraintsIsDeep(t *testing.T) {
	t.Parallel()
	clone := cloneConstraints(appOrGlobalConstraints)
	clone[0].Fields[0] = "rewritten"
	if appOrGlobalConstraints[0].Fields[0] != "app" {
		t.Fatalf("appOrGlobalConstraints rewritten through a clone: %v", appOrGlobalConstraints)
	}
}

// declaredStruct is one task or element struct and the declarations it makes.
type declaredStruct struct {
	where string
	rt    reflect.Type
}

// declaredStructs returns every registered task's struct, then the element
// structs of its structured lists, so a walk covers every declaration the
// catalog publishes.
func declaredStructs(rt reflect.Type, where string) []declaredStruct {
	out := []declaredStruct{{where: where, rt: rt}}
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		name := recipeKey(field)
		if name == "" {
			continue
		}
		if field.Type.Kind() != reflect.Slice {
			continue
		}
		if elem := structElem(field.Type.Elem()); elem != nil {
			out = append(out, declaredStructs(elem, where+"."+name+"[]")...)
		}
	}
	return out
}

// taskStateChoices returns the options of rt's state field, and whether it has
// one.
func taskStateChoices(rt reflect.Type) ([]string, bool) {
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if recipeKey(field) == "state" {
			return strings.Split(field.Tag.Get("options"), ","), true
		}
	}
	return nil, false
}

// TestConstraintDeclarationsAreWellFormed checks every declaration against the
// struct it sits on: names that are recipe keys of that struct, states that
// are the task's state choices, groups with at least two members, and field
// tags that agree with each other.
func TestConstraintDeclarationsAreWellFormed(t *testing.T) {
	t.Parallel()
	for _, typeKey := range TaskTypes() {
		task, _ := Lookup(typeKey)
		taskType := taskStructOf(task)
		choices, hasState := taskStateChoices(taskType)
		checkStates := func(where string, states []string) {
			t.Helper()
			if len(states) == 0 {
				return
			}
			if !hasState {
				t.Errorf("%s: declares states %q but the task has no state field", where, states)
				return
			}
			for _, state := range states {
				if !slices.Contains(choices, state) {
					t.Errorf("%s: state %q is not one of the task's choices %q", where, state, choices)
				}
			}
		}

		for _, s := range declaredStructs(taskType, typeKey) {
			keys := map[string]bool{}
			for i := 0; i < s.rt.NumField(); i++ {
				field := s.rt.Field(i)
				name := recipeKey(field)
				if name == "" {
					continue
				}
				keys[name] = true
				where := s.where + "." + name
				requiredWhen := conditionalStates(field.Tag.Get(requiredWhenTag))
				forbiddenWhen := conditionalStates(field.Tag.Get(forbiddenWhenTag))
				checkStates(where+" required_when", requiredWhen)
				checkStates(where+" forbidden_when", forbiddenWhen)
				if len(requiredWhen) > 0 && (field.Tag.Get("required") == "true" || field.Tag.Get("default") != "") {
					t.Errorf("%s: required_when on a field that is required or has a default", where)
				}
				for _, state := range requiredWhen {
					if slices.Contains(forbiddenWhen, state) {
						t.Errorf("%s: both required and forbidden for state %q", where, state)
					}
				}
			}

			for _, c := range structConstraints(s.rt) {
				where := fmt.Sprintf("%s %s %v", s.where, c.Kind, c.Fields)
				if !slices.Contains(ConstraintKinds, c.Kind) {
					t.Errorf("%s: unknown kind", where)
				}
				if c.Kind.Directional() {
					if !keys[c.Field] {
						t.Errorf("%s: field %q is not a key of this struct", where, c.Field)
					}
					if len(c.Fields) == 0 || slices.Contains(c.Fields, c.Field) {
						t.Errorf("%s: needs at least one target other than %q", where, c.Field)
					}
				} else {
					if c.Field != "" {
						t.Errorf("%s: field %q is only meaningful on a directional kind", where, c.Field)
					}
					if len(c.Fields) < 2 {
						t.Errorf("%s: a group needs at least two fields", where)
					}
				}
				seen := map[string]bool{}
				for _, name := range c.Fields {
					if !keys[name] {
						t.Errorf("%s: %q is not a key of this struct", where, name)
					}
					if seen[name] {
						t.Errorf("%s: %q listed twice", where, name)
					}
					seen[name] = true
				}
				states := make([]string, len(c.States))
				for i, state := range c.States {
					states[i] = string(state)
				}
				checkStates(where, states)
			}
		}
	}
}

// sampleValue returns a non-zero value of type rt that differs from def, for
// building an instance in which a field is set.
func sampleValue(rt reflect.Type, def string) reflect.Value {
	switch rt.Kind() {
	case reflect.Pointer:
		ptr := reflect.New(rt.Elem())
		ptr.Elem().Set(sampleValue(rt.Elem(), def))
		return ptr
	case reflect.String:
		value := "docket-sample"
		if value == def {
			value = "docket-sample-other"
		}
		return reflect.ValueOf(value).Convert(rt)
	case reflect.Bool:
		return reflect.ValueOf(def != "true").Convert(rt)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value := int64(7)
		if fmt.Sprint(value) == def {
			value = 8
		}
		return reflect.ValueOf(value).Convert(rt)
	case reflect.Slice:
		slice := reflect.MakeSlice(rt, 1, 1)
		slice.Index(0).Set(sampleValue(rt.Elem(), ""))
		return slice
	case reflect.Map:
		m := reflect.MakeMap(rt)
		m.SetMapIndex(sampleValue(rt.Key(), ""), sampleValue(rt.Elem(), ""))
		return m
	case reflect.Interface:
		return reflect.ValueOf("docket-sample")
	case reflect.Struct:
		return reflect.New(rt).Elem()
	}
	panic(fmt.Sprintf("sampleValue: unhandled kind %s", rt.Kind()))
}

// setByKey sets the field of v whose recipe key is name to a sample value.
func setByKey(v reflect.Value, name string) {
	rt := v.Type()
	for i := 0; i < rt.NumField(); i++ {
		if recipeKey(rt.Field(i)) == name {
			v.Field(i).Set(sampleValue(rt.Field(i).Type, rt.Field(i).Tag.Get("default")))
			return
		}
	}
	panic(fmt.Sprintf("setByKey: no key %q on %s", name, rt))
}

// enforcementCase is one way to break one declaration: a task instance built
// to break it, and the message the break must produce.
type enforcementCase struct {
	where string
	task  reflect.Value
	want  string
}

// enforcementCases builds a breaking instance for every declaration on
// taskType and its element structs. Each instance starts from a zero task in
// the state the declaration applies in; an element declaration is broken in
// the first element of a one-element list.
func enforcementCases(typeKey string, taskType reflect.Type) []enforcementCase {
	var cases []enforcementCase
	choices, _ := taskStateChoices(taskType)

	// build returns a zero task in state, and the struct the declaration sits
	// on: the task itself, or the first element of list.
	build := func(state string, list string) (reflect.Value, reflect.Value, string) {
		task := reflect.New(taskType).Elem()
		if state != "" {
			for i := 0; i < taskType.NumField(); i++ {
				if recipeKey(taskType.Field(i)) == "state" {
					task.Field(i).SetString(state)
				}
			}
		}
		if list == "" {
			return task, task, ""
		}
		for i := 0; i < taskType.NumField(); i++ {
			if recipeKey(taskType.Field(i)) == list {
				slice := reflect.MakeSlice(taskType.Field(i).Type, 1, 1)
				task.Field(i).Set(slice)
				return task, task.Field(i).Index(0), list + "[0]."
			}
		}
		panic("no list " + list)
	}
	statesOr := func(states []string) []string {
		if len(states) > 0 {
			return states
		}
		return []string{""}
	}
	stateName := func(task reflect.Value) State {
		return taskState(task)
	}

	for _, s := range declaredStructs(taskType, typeKey) {
		list := ""
		if s.rt != taskType {
			// declaredStructs names an element `<task>.<list>[]`; only
			// element structs one level down are declared today.
			list = strings.TrimSuffix(strings.TrimPrefix(s.where, typeKey+"."), "[]")
		}
		for i := 0; i < s.rt.NumField(); i++ {
			field := s.rt.Field(i)
			name := recipeKey(field)
			if name == "" {
				continue
			}
			if list != "" && field.Tag.Get("required") == "true" && field.Tag.Get("default") == "" {
				task, _, prefix := build(choices0(choices), list)
				cases = append(cases, enforcementCase{s.where + "." + name + " required", task, fmt.Sprintf("'%s%s' is required", prefix, name)})
			}
			for _, state := range conditionalStates(field.Tag.Get(requiredWhenTag)) {
				task, _, prefix := build(state, list)
				cases = append(cases, enforcementCase{s.where + "." + name + " required_when " + state, task, fmt.Sprintf("'%s%s' is required for state '%s'", prefix, name, state)})
			}
			for _, state := range conditionalStates(field.Tag.Get(forbiddenWhenTag)) {
				task, target, prefix := build(state, list)
				setByKey(target, name)
				cases = append(cases, enforcementCase{s.where + "." + name + " forbidden_when " + state, task, fmt.Sprintf("'%s%s' must not be set for state '%s'", prefix, name, state)})
			}
		}

		for _, c := range structConstraints(s.rt) {
			var states []string
			for _, state := range c.States {
				states = append(states, string(state))
			}
			for _, state := range statesOr(states) {
				where := fmt.Sprintf("%s %s %v %s", s.where, c.Kind, c.Fields, state)
				task, target, prefix := build(state, list)
				q := func(names ...string) []string {
					out := make([]string, len(names))
					for i, name := range names {
						out[i] = prefix + name
					}
					return out
				}
				when := ""
				if len(c.States) > 0 {
					when = fmt.Sprintf(" for state '%s'", stateName(task))
				}
				var want string
				switch c.Kind {
				case ConstraintExactlyOneOf:
					want = fmt.Sprintf("exactly one of %s is required%s", quotedList(q(c.Fields...), "or"), when)
				case ConstraintAtLeastOneOf:
					want = fmt.Sprintf("at least one of %s is required%s", quotedList(q(c.Fields...), "or"), when)
				case ConstraintAllOrNoneOf:
					setByKey(target, c.Fields[0])
					want = fmt.Sprintf("%s must be set together%s", quotedList(q(c.Fields...), "and"), when)
				case ConstraintConflictsWith:
					setByKey(target, c.Field)
					setByKey(target, c.Fields[0])
					want = fmt.Sprintf("'%s%s' and '%s%s' are mutually exclusive", prefix, c.Field, prefix, c.Fields[0])
				case ConstraintRequiredWith:
					setByKey(target, c.Field)
					want = fmt.Sprintf("'%s%s' is required when '%s%s' is set%s", prefix, c.Fields[0], prefix, c.Field, when)
				}
				cases = append(cases, enforcementCase{where, task, want})

				if c.Kind == ConstraintExactlyOneOf {
					task, target, prefix := build(state, list)
					setByKey(target, c.Fields[0])
					setByKey(target, c.Fields[1])
					want := fmt.Sprintf("'%s%s' and '%s%s' are mutually exclusive", prefix, c.Fields[0], prefix, c.Fields[1])
					cases = append(cases, enforcementCase{where + " (two set)", task, want})
				}
			}
		}
	}
	return cases
}

// choices0 is the first of a task's state choices, or "" when it has none.
func choices0(choices []string) string {
	if len(choices) == 0 {
		return ""
	}
	return choices[0]
}

// TestEveryDeclaredConstraintIsEnforced breaks every declaration every task
// publishes, one at a time, and checks the checker reports the break and that
// both Validate and Plan refuse the task over it. Plan stops in gatePlan
// before any probe, so no server is involved.
func TestEveryDeclaredConstraintIsEnforced(t *testing.T) {
	t.Parallel()
	total := 0
	for _, typeKey := range TaskTypes() {
		prototype, _ := Lookup(typeKey)
		taskType := taskStructOf(prototype)
		cases := enforcementCases(typeKey, taskType)
		if len(cases) == 0 {
			continue
		}
		if _, ok := prototype.(InputValidator); !ok {
			t.Errorf("%s declares constraints but has no Validate()", typeKey)
		}
		for _, tc := range cases {
			total++
			var messages []string
			for _, v := range ConstraintViolations(tc.task.Interface()) {
				messages = append(messages, v.Message)
			}
			if !slices.Contains(messages, tc.want) {
				t.Errorf("%s: violations %q, want one of them to be %q", tc.where, messages, tc.want)
				continue
			}
			task := tc.task.Interface().(Task)
			if validator, ok := task.(InputValidator); ok {
				if err := validator.Validate(); err == nil {
					t.Errorf("%s: Validate() = nil, want an error", tc.where)
				}
			}
			if plan := task.Plan(testCtx()); plan.Error == nil {
				t.Errorf("%s: Plan error = nil, want an error", tc.where)
			}
		}
	}
	if total == 0 {
		t.Fatal("no declarations found")
	}
}

// TestDeclaredConstraintPins pins the declarations #620 named, so a change to
// one is a deliberate edit here rather than a silent drift in the catalog.
func TestDeclaredConstraintPins(t *testing.T) {
	t.Parallel()
	catalog := wholeCatalog(t)

	gitAuth := schemaFor(t, catalog, "dokku_git_auth")
	for _, name := range []string{"username", "password"} {
		if got := fieldFor(t, gitAuth.Fields, name).RequiredWhen; !slices.Equal(got, []string{"present"}) {
			t.Errorf("dokku_git_auth.%s required_when = %q, want [present]", name, got)
		}
	}

	certs := schemaFor(t, catalog, "dokku_certs")
	wantCerts := []Constraint{
		{Kind: ConstraintConflictsWith, Field: "cert", Fields: []string{"cert_content", "key_content"}},
		{Kind: ConstraintConflictsWith, Field: "key", Fields: []string{"key_content", "cert_content"}},
	}
	for _, want := range wantCerts {
		if !slices.ContainsFunc(certs.Constraints, func(c Constraint) bool { return reflect.DeepEqual(c, want) }) {
			t.Errorf("dokku_certs constraints %+v, want them to include %+v", certs.Constraints, want)
		}
	}

	users := fieldFor(t, schemaFor(t, catalog, "dokku_http_auth_user").Fields, "users")
	wantUser := []Constraint{
		{Kind: ConstraintConflictsWith, Field: "password", Fields: []string{"hash"}},
		{Kind: ConstraintAtLeastOneOf, Fields: []string{"password", "hash"}, States: []State{StatePresent, StateSet}},
	}
	if users.Item == nil || !reflect.DeepEqual(users.Item.Constraints, wantUser) {
		t.Errorf("dokku_http_auth_user users[] constraints = %+v, want %+v", users.Item, wantUser)
	}

	appOrGlobal := Constraint{Kind: ConstraintExactlyOneOf, Fields: []string{"app", "global"}}
	for _, schema := range catalog.Tasks {
		if schema.PropertySchema == nil {
			continue
		}
		if !slices.ContainsFunc(schema.Constraints, func(c Constraint) bool { return reflect.DeepEqual(c, appOrGlobal) }) {
			t.Errorf("%s constraints = %+v, want exactly_one_of [app global]", schema.Type, schema.Constraints)
		}
	}

	// Every create-time option of dokku_service_create is refused under state
	// absent, since `<service>:destroy` takes none of them; only the keys that
	// name the service, and the state itself, are allowed there.
	for _, field := range schemaFor(t, catalog, "dokku_service_create").Fields {
		allowed := field.Name == "service" || field.Name == "name" || field.Name == "state"
		if forbidden := slices.Contains(field.ForbiddenWhen, "absent"); forbidden == allowed {
			t.Errorf("dokku_service_create.%s forbidden_when = %q", field.Name, field.ForbiddenWhen)
		}
	}
}

func formatViolations(violations []Violation) string {
	var b strings.Builder
	for _, v := range violations {
		fmt.Fprintf(&b, "  {%q %q}\n", v.Field, v.Message)
	}
	return b.String()
}
