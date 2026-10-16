package tasks

import (
	"context"
	"reflect"
	"sort"
	"testing"
)

// fakeTask is a minimal Task implementation that lets unit tests construct
// arbitrary structs and exercise the sensitive value walker without needing
// any real Dokku side effect.
type fakeTask struct {
	Public    string `yaml:"public"`
	Secret    string `sensitive:"true" yaml:"secret"`
	SecretMap map[string]string
}

func (f *fakeTask) Plan(ctx context.Context) PlanResult         { return PlanResult{} }
func (f *fakeTask) Execute(ctx context.Context) TaskOutputState { return TaskOutputState{} }

type taggedSliceTask struct {
	Tokens []string `sensitive:"true"`
}

func (t *taggedSliceTask) Plan(ctx context.Context) PlanResult         { return PlanResult{} }
func (t *taggedSliceTask) Execute(ctx context.Context) TaskOutputState { return TaskOutputState{} }

type taggedMapTask struct {
	Headers map[string]string `sensitive:"true"`
}

func (t *taggedMapTask) Plan(ctx context.Context) PlanResult         { return PlanResult{} }
func (t *taggedMapTask) Execute(ctx context.Context) TaskOutputState { return TaskOutputState{} }

type nestedTask struct {
	Outer struct {
		Inner string `sensitive:"true"`
	}
}

func (n *nestedTask) Plan(ctx context.Context) PlanResult         { return PlanResult{} }
func (n *nestedTask) Execute(ctx context.Context) TaskOutputState { return TaskOutputState{} }

type overrideTask struct {
	Field string `sensitive:"true"`
	Map   map[string]string
}

func (o *overrideTask) Plan(ctx context.Context) PlanResult         { return PlanResult{} }
func (o *overrideTask) Execute(ctx context.Context) TaskOutputState { return TaskOutputState{} }
func (o *overrideTask) SensitiveValues() []string {
	out := make([]string, 0, len(o.Map))
	for _, v := range o.Map {
		out = append(out, v)
	}
	return out
}

func sortedEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	x := append([]string(nil), a...)
	y := append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	return true
}

func TestSensitiveValuesFromTaggedString(t *testing.T) {
	got := sensitiveValuesFromTask(&fakeTask{Public: "p", Secret: "s"})
	if !sortedEqual(got, []string{"s"}) {
		t.Errorf("got %v, want [s]", got)
	}
}

func TestSensitiveValuesEmptyForUntaggedStruct(t *testing.T) {
	type plain struct {
		A string
		B int
	}
	type plainTask struct {
		fakeTask
		Plain plain
	}
	pt := &plainTask{fakeTask: fakeTask{Public: "x"}}
	got := sensitiveValuesFromTask(pt)
	for _, v := range got {
		if v == "x" {
			t.Errorf("untagged field surfaced: %v", got)
		}
	}
}

func TestSensitiveValuesFromTaggedSlice(t *testing.T) {
	got := sensitiveValuesFromTask(&taggedSliceTask{Tokens: []string{"a", "", "b"}})
	if !sortedEqual(got, []string{"a", "b"}) {
		t.Errorf("got %v, want [a b]", got)
	}
}

func TestSensitiveValuesFromTaggedMap(t *testing.T) {
	got := sensitiveValuesFromTask(&taggedMapTask{Headers: map[string]string{
		"X-Token":  "secret123",
		"X-Public": "",
		"X-Other":  "more",
	}})
	if !sortedEqual(got, []string{"secret123", "more"}) {
		t.Errorf("got %v, want [secret123 more]", got)
	}
}

func TestSensitiveValuesNestedStruct(t *testing.T) {
	n := &nestedTask{}
	n.Outer.Inner = "hidden"
	got := sensitiveValuesFromTask(n)
	if !sortedEqual(got, []string{"hidden"}) {
		t.Errorf("got %v, want [hidden]", got)
	}
}

func TestSensitiveValuesOverrideMergesWithTags(t *testing.T) {
	o := &overrideTask{
		Field: "tagged",
		Map:   map[string]string{"k1": "viaOverride", "k2": "alsoOverride"},
	}
	got := sensitiveValuesFromTask(o)
	if !sortedEqual(got, []string{"tagged", "viaOverride", "alsoOverride"}) {
		t.Errorf("got %v, want all three values", got)
	}
}

type sensitiveItem struct {
	Name   string
	Secret string `sensitive:"true"`
}

type itemCollectionsTask struct {
	Items    []sensitiveItem
	Pointers []*sensitiveItem
	ByName   map[string]sensitiveItem
	Fixed    [1]sensitiveItem
}

func (i *itemCollectionsTask) Plan(ctx context.Context) PlanResult         { return PlanResult{} }
func (i *itemCollectionsTask) Execute(ctx context.Context) TaskOutputState { return TaskOutputState{} }

func TestSensitiveValuesFromStructCollections(t *testing.T) {
	task := &itemCollectionsTask{
		Items:    []sensitiveItem{{Name: "a", Secret: "slice"}, {Name: "b"}},
		Pointers: []*sensitiveItem{nil, {Name: "c", Secret: "pointer"}},
		ByName:   map[string]sensitiveItem{"d": {Name: "d", Secret: "map"}},
		Fixed:    [1]sensitiveItem{{Name: "e", Secret: "array"}},
	}
	got := sensitiveValuesFromTask(task)
	if !sortedEqual(got, []string{"slice", "pointer", "map", "array"}) {
		t.Errorf("got %v, want the secret of every item", got)
	}
}

func TestHttpAuthUserItemTagsAreCollected(t *testing.T) {
	// A struct carrying the users without HttpAuthUserTask's SensitiveValues
	// method, so only the tags on HttpAuthUser can surface them.
	type usersOnly struct {
		Users []HttpAuthUser
	}
	got := walkSensitiveTags(reflect.ValueOf(usersOnly{Users: []HttpAuthUser{
		{Username: "admin", Password: "pw"},
		{Username: "ops", Hash: "hash"},
	}}))
	if !sortedEqual(got, []string{"pw", "hash"}) {
		t.Errorf("got %v, want [pw hash]", got)
	}
}

func TestSensitivePropertyValues(t *testing.T) {
	cases := []struct {
		name string
		task interface{}
		want []string
	}{
		{"mapped sensitive", TraefikPropertyTask{Global: true, Property: "basic-auth-password", Value: "pw"}, []string{"pw"}},
		{"mapped sensitive pointer", &TraefikPropertyTask{Global: true, Property: "basic-auth-password", Value: "pw"}, []string{"pw"}},
		{"dynamic sensitive", TraefikPropertyTask{Global: true, Property: "dns-provider-CF_API_TOKEN", Value: "token"}, []string{"token"}},
		{"other plugin", SchedulerK3sPropertyTask{Global: true, Property: "token", Value: "k3s"}, []string{"k3s"}},
		{"benign", TraefikPropertyTask{Global: true, Property: "api-enabled", Value: "true"}, nil},
		{"empty value", TraefikPropertyTask{Global: true, Property: "basic-auth-password"}, nil},
		{"nil pointer", (*TraefikPropertyTask)(nil), nil},
		{"not a property task", AppTask{App: "api"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sensitiveValuesFromTask(tc.task); !sortedEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTaskSensitiveValuesDedupesAndDropsEmpties(t *testing.T) {
	// The tags and HttpAuthUserTask's SensitiveValues method both reach the
	// users' credentials; each value comes back once.
	task := &HttpAuthUserTask{App: "api", Users: []HttpAuthUser{
		{Username: "admin", Password: "pw"},
		{Username: "ops", Password: "pw"},
	}}
	if got := TaskSensitiveValues(task); !sortedEqual(got, []string{"pw"}) {
		t.Errorf("got %v, want [pw]", got)
	}

	withEmpty := &overrideTask{Map: map[string]string{"k": ""}}
	if got := TaskSensitiveValues(withEmpty); got != nil {
		t.Errorf("got %v, want nil", got)
	}

	if got := TaskSensitiveValues(nil); got != nil {
		t.Errorf("nil task: got %v, want nil", got)
	}
	if got := TaskSensitiveValues((*HttpAuthUserTask)(nil)); got != nil {
		t.Errorf("nil pointer task: got %v, want nil", got)
	}
}

func TestCollectSensitiveValuesAcrossTasks(t *testing.T) {
	m := OrderedStringEnvelopeMap{}
	m.Set("a", &TaskEnvelope{Name: "a", Task: &fakeTask{Secret: "s1"}})
	m.Set("b", &TaskEnvelope{Name: "b", Task: &taggedSliceTask{Tokens: []string{"s2"}}})
	got := CollectSensitiveValues(m)
	if !sortedEqual(got, []string{"s1", "s2"}) {
		t.Errorf("got %v, want [s1 s2]", got)
	}
}

func TestCollectSensitiveValuesWalksGroupChildren(t *testing.T) {
	// A block/rescue/always group carries no task of its own (Task == nil);
	// its children hold the sensitive values and must still be collected.
	m := OrderedStringEnvelopeMap{}
	m.Set("group", &TaskEnvelope{
		Name: "group",
		Block: []*TaskEnvelope{
			{Name: "b", Task: &fakeTask{Secret: "blockSecret"}},
		},
		Rescue: []*TaskEnvelope{
			{Name: "r", Task: &taggedSliceTask{Tokens: []string{"rescueSecret"}}},
		},
		Always: []*TaskEnvelope{
			{Name: "a", Task: &taggedMapTask{Headers: map[string]string{"X": "alwaysSecret"}}},
		},
	})
	got := CollectSensitiveValues(m)
	if !sortedEqual(got, []string{"blockSecret", "rescueSecret", "alwaysSecret"}) {
		t.Errorf("got %v, want block/rescue/always secrets", got)
	}
}

func TestCollectSensitiveValuesWalksNestedGroups(t *testing.T) {
	// Groups nest: a group inside another group's block. The deepest child's
	// secret must surface regardless of nesting depth.
	m := OrderedStringEnvelopeMap{}
	m.Set("outer", &TaskEnvelope{
		Name: "outer",
		Block: []*TaskEnvelope{
			{
				Name: "inner-group",
				Block: []*TaskEnvelope{
					{Name: "leaf", Task: &fakeTask{Secret: "deepSecret"}},
				},
			},
		},
	})
	got := CollectSensitiveValues(m)
	if !sortedEqual(got, []string{"deepSecret"}) {
		t.Errorf("got %v, want [deepSecret]", got)
	}
}

func TestConfigTaskSensitiveValuesReturnsMapValuesAndBase64(t *testing.T) {
	ct := &ConfigTask{
		App:    "myapp",
		Config: map[string]string{"DATABASE_URL": "postgres://x", "EMPTY": "", "TOKEN": "abc"},
	}
	got := ct.SensitiveValues()
	// Each non-empty value contributes its raw form plus base64 (the form
	// the apply path puts on argv via `config:set --encoded`).
	want := []string{
		"postgres://x", "cG9zdGdyZXM6Ly94", // base64("postgres://x")
		"abc", "YWJj", // base64("abc")
	}
	if !sortedEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestRegistryAuthTaskPasswordIsCollected(t *testing.T) {
	rt := &RegistryAuthTask{
		Server:   "ghcr.io",
		Username: "alice",
		Password: "topsecret",
	}
	got := sensitiveValuesFromTask(rt)
	if !sortedEqual(got, []string{"topsecret"}) {
		t.Errorf("got %v, want [topsecret]", got)
	}
}
