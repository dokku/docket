package tasks

import (
	"reflect"
	"slices"
	"testing"
)

// unknownRuleFixture carries one rule over a, b and c, so each case states the
// rule it checks beside the values it checks it against.
type unknownRuleFixture struct {
	A     string `yaml:"a"`
	B     string `yaml:"b"`
	C     string `yaml:"c"`
	State State  `default:"present" options:"present,absent" yaml:"state"`

	rule Constraint
}

func (f unknownRuleFixture) Constraints() []Constraint {
	return []Constraint{f.rule}
}

// violationMessages runs ConstraintViolations and keeps only the messages.
func violationMessages(task any, opts ...ConstraintOption) []string {
	var got []string
	for _, v := range ConstraintViolations(task, opts...) {
		got = append(got, v.Message)
	}
	return got
}

// TestConstraintViolationsUnknownRules pins, for each kind, which unknown
// fields leave the rule decided by the known ones, which skip it, and which
// satisfy it by counting as set.
func TestConstraintViolationsUnknownRules(t *testing.T) {
	t.Parallel()
	exactly := Constraint{Kind: ConstraintExactlyOneOf, Fields: []string{"a", "b", "c"}}
	atLeast := Constraint{Kind: ConstraintAtLeastOneOf, Fields: []string{"a", "b"}}
	allOrNone := Constraint{Kind: ConstraintAllOrNoneOf, Fields: []string{"a", "b", "c"}}
	conflicts := Constraint{Kind: ConstraintConflictsWith, Field: "a", Fields: []string{"b", "c"}}
	requiredWith := Constraint{Kind: ConstraintRequiredWith, Field: "a", Fields: []string{"b", "c"}}

	cases := []struct {
		name    string
		rule    Constraint
		a, b, c string
		unknown []string
		want    []string
	}{
		{"exactly one, unknown and none set, skipped", exactly, "", "", "", []string{"a"}, nil},
		{"exactly one, unknown and one set, skipped", exactly, "", "1", "", []string{"a"}, nil},
		{"exactly one, unknown and two set, decided", exactly, "", "1", "1", []string{"a"}, []string{"'b' and 'c' are mutually exclusive"}},
		{"exactly one, unknown value ignored", exactly, "1", "", "", []string{"b", "c"}, nil},
		{"exactly one, unrelated unknown, decided", exactly, "", "", "", []string{"state_unrelated"}, []string{"exactly one of 'a', 'b' or 'c' is required"}},

		{"at least one, unknown counts as set", atLeast, "", "", "", []string{"b"}, nil},
		{"at least one, unknown elsewhere, decided", atLeast, "", "", "1", []string{"c"}, []string{"at least one of 'a' or 'b' is required"}},

		{"all or none, unknown completes the set ones", allOrNone, "1", "1", "", []string{"c"}, nil},
		{"all or none, set beside unset, decided", allOrNone, "1", "", "", []string{"c"}, []string{"'a', 'b' and 'c' must be set together"}},
		{"all or none, unknown beside unset, skipped", allOrNone, "", "", "", []string{"a"}, nil},

		{"conflicts, unknown field, skipped", conflicts, "", "1", "1", []string{"a"}, nil},
		{"conflicts, unknown target, skipped", conflicts, "1", "", "", []string{"b"}, nil},
		{"conflicts, known target beside unknown, decided", conflicts, "1", "", "1", []string{"b"}, []string{"'a' and 'c' are mutually exclusive"}},

		{"required with, unknown field, skipped", requiredWith, "", "", "", []string{"a"}, nil},
		{"required with, unknown target counts as set", requiredWith, "1", "", "1", []string{"b"}, nil},
		{"required with, known target missing, decided", requiredWith, "1", "", "", []string{"b"}, []string{"'c' is required when 'a' is set"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			task := unknownRuleFixture{A: tc.a, B: tc.b, C: tc.c, rule: tc.rule}
			if got := violationMessages(task, WithUnknown(tc.unknown...)); !slices.Equal(got, tc.want) {
				t.Errorf("messages = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestConstraintViolationsUnknownFieldRules pins required_when, forbidden_when
// and a list item's required keys against unknown fields.
func TestConstraintViolationsUnknownFieldRules(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		task    constraintFixture
		unknown []string
		want    []string
	}{
		{
			name:    "required_when counts an unknown field as set",
			task:    constraintFixture{Left: "l", List: []string{"a"}},
			unknown: []string{"value"},
		},
		{
			name:    "required_when on a known field is still reported",
			task:    constraintFixture{Left: "l"},
			unknown: []string{"value"},
			want:    []string{"'list' is required for state 'present'"},
		},
		{
			name:    "forbidden_when on an unknown field is skipped",
			task:    constraintFixture{Left: "l", State: StateAbsent},
			unknown: []string{"value", "mode"},
		},
		{
			name:    "forbidden_when on a known field is still reported",
			task:    constraintFixture{Left: "l", Mode: "manual", State: StateAbsent},
			unknown: []string{"value"},
			want:    []string{"'mode' must not be set for state 'absent'"},
		},
		{
			name:    "an unknown value outranks the known one",
			task:    constraintFixture{Left: "l", Value: "v", State: StateAbsent},
			unknown: []string{"value"},
		},
		{
			name:    "a list item's unknown required key counts as set",
			task:    constraintFixture{Left: "l", Value: "v", List: []string{"a"}, Items: []constraintFixtureEntry{{One: "1"}}},
			unknown: []string{"items[0].key"},
		},
		{
			name:    "an unknown key names the list item, not another",
			task:    constraintFixture{Left: "l", Value: "v", List: []string{"a"}, Items: []constraintFixtureEntry{{One: "1"}, {One: "1"}}},
			unknown: []string{"items[0].key"},
			want:    []string{"'items[1].key' is required"},
		},
		{
			name:    "an element key without its index is not the item's",
			task:    constraintFixture{Left: "l", Value: "v", List: []string{"a"}, Items: []constraintFixtureEntry{{One: "1"}}},
			unknown: []string{"key"},
			want:    []string{"'items[0].key' is required"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := violationMessages(tc.task, WithUnknown(tc.unknown...)); !slices.Equal(got, tc.want) {
				t.Errorf("messages = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestConstraintViolationsUnknownState pins that an unknown state skips every
// state-scoped rule, the task's and its items', and keeps the rest.
func TestConstraintViolationsUnknownState(t *testing.T) {
	t.Parallel()
	task := constraintFixture{
		Value:  "v",
		Mode:   "manual",
		Left:   "l",
		Right:  "r",
		Source: "s",
		Items:  []constraintFixtureEntry{{Key: "a", One: "1", Other: "2"}, {}},
		State:  StateAbsent,
	}
	got := ConstraintViolations(task, WithUnknown("state"))
	want := []Violation{
		{Field: "", Message: "'left' and 'right' are mutually exclusive"},
		{Field: "items[0].one", Message: "'items[0].one' and 'items[0].other' are mutually exclusive"},
		{Field: "items[1].key", Message: "'items[1].key' is required"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ConstraintViolations =\n%s\nwant\n%s", formatViolations(got), formatViolations(want))
	}
}

// TestConstraintViolationsUnknownLists pins how an unknown list and an unknown
// item relate to the items checked.
func TestConstraintViolationsUnknownLists(t *testing.T) {
	t.Parallel()
	items := []constraintFixtureEntry{
		{Key: "a", One: "1", Other: "2"},
		{},
		{Key: "c"},
	}
	cases := []struct {
		name    string
		unknown []string
		want    []Violation
	}{
		{
			name: "every item checked when all are known",
			want: []Violation{
				{Field: "items[0].one", Message: "'items[0].one' and 'items[0].other' are mutually exclusive"},
				{Field: "items[1].key", Message: "'items[1].key' is required"},
				{Field: "items[1]", Message: "at least one of 'items[1].one' or 'items[1].other' is required for state 'present'"},
				{Field: "items[2]", Message: "at least one of 'items[2].one' or 'items[2].other' is required for state 'present'"},
			},
		},
		{
			name:    "an unknown list checks none of its items",
			unknown: []string{"items"},
		},
		{
			name:    "an unknown item is skipped and the rest checked",
			unknown: []string{"items[1]"},
			want: []Violation{
				{Field: "items[0].one", Message: "'items[0].one' and 'items[0].other' are mutually exclusive"},
				{Field: "items[2]", Message: "at least one of 'items[2].one' or 'items[2].other' is required for state 'present'"},
			},
		},
		{
			name:    "unknown item fields decide only their own rules",
			unknown: []string{"items[0].other", "items[1].one", "items[2].other"},
			want: []Violation{
				{Field: "items[1].key", Message: "'items[1].key' is required"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			task := constraintFixture{Value: "v", List: []string{"a"}, Left: "l", Items: items}
			got := ConstraintViolations(task, WithUnknown(tc.unknown...))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ConstraintViolations =\n%s\nwant\n%s", formatViolations(got), formatViolations(tc.want))
			}
		})
	}
}

// TestConstraintViolationsUnknownListField pins that an unknown list counts as
// set for its own required_when and skips its forbidden_when.
func TestConstraintViolationsUnknownListField(t *testing.T) {
	t.Parallel()
	present := HttpAuthUserTask{App: "api", State: StatePresent}
	if got := violationMessages(present); !slices.Equal(got, []string{"'users' is required for state 'present'"}) {
		t.Fatalf("known messages = %q, want the users requirement", got)
	}
	if got := violationMessages(present, WithUnknown("users")); got != nil {
		t.Errorf("unknown users, present: messages = %q, want none", got)
	}
	cleared := HttpAuthUserTask{App: "api", State: StateClear, Users: []HttpAuthUser{{Username: "u"}}}
	if got := violationMessages(cleared, WithUnknown("users")); got != nil {
		t.Errorf("unknown users, clear: messages = %q, want none", got)
	}
}

// TestConstraintViolationsUnknownRealTask runs the issue's own case against a
// registered task: an unknown hash satisfies a user's at_least_one_of, and a
// known password beside a known hash still conflicts.
func TestConstraintViolationsUnknownRealTask(t *testing.T) {
	t.Parallel()
	task := &HttpAuthUserTask{
		State: StatePresent,
		Users: []HttpAuthUser{
			{Username: "a", Password: "p", Hash: "h"},
			{Username: "b"},
		},
	}
	got := ConstraintViolations(task, WithUnknown("users[1].hash", "app"))
	want := []Violation{
		{Field: "users[0].password", Message: "'users[0].password' and 'users[0].hash' are mutually exclusive"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ConstraintViolations =\n%s\nwant\n%s", formatViolations(got), formatViolations(want))
	}
}

// TestConstraintViolationsUnknownProperty pins that an unknown property skips
// the managed-family rejection, which turns on the property's value.
func TestConstraintViolationsUnknownProperty(t *testing.T) {
	t.Parallel()
	task := SchedulerK3sPropertyTask{Property: "chart.traefik.replicas", State: StatePresent}
	for _, v := range ConstraintViolations(task, WithUnknown(PropertyFieldName)) {
		if v.Field == PropertyFieldName {
			t.Errorf("violation %q on %s, want the rejection skipped", v.Message, v.Field)
		}
	}
}

// TestConstraintViolationsNoUnknown pins that an empty WithUnknown, a nil
// option and no option at all check the same thing.
func TestConstraintViolationsNoUnknown(t *testing.T) {
	t.Parallel()
	task := constraintFixture{Left: "l", Right: "r", Source: "s", Items: []constraintFixtureEntry{{}}}
	want := ConstraintViolations(task)
	if want == nil {
		t.Fatal("fixture breaks no rule")
	}
	for name, opts := range map[string][]ConstraintOption{
		"empty WithUnknown": {WithUnknown()},
		"nil option":        {nil},
	} {
		if got := ConstraintViolations(task, opts...); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: ConstraintViolations =\n%s\nwant\n%s", name, formatViolations(got), formatViolations(want))
		}
	}
}
