package tasks

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

// TestInSyncPlanObservesTheDeclaredTask is the in-sync half of the Observed
// contract (#637): a plan that finds the server holding what the task
// declares observes every field the task sets exactly as declared, so a
// consumer diffing the read against the task sees no change Plan would not
// make. Every exported body and case of every fixture that plans in sync is
// checked: its Exists follows the declared state, and ReadTask returns the
// same task and flag. Every fixture needs at least one such plan.
func TestInSyncPlanObservesTheDeclaredTask(t *testing.T) {
	t.Parallel()
	for typeKey, fixture := range observedFixtures() {
		t.Run(typeKey, func(t *testing.T) {
			t.Parallel()
			rt := registeredTypeOrNil(typeKey)
			inSync := 0
			for _, op := range planObservedFixture(t, fixture) {
				if !op.plan.InSync {
					continue
				}
				inSync++
				if got := reflect.TypeOf(op.plan.Observed); got != rt {
					t.Errorf("%s: Observed is %v, want the registered value type %v", op.name, got, rt)
					continue
				}
				for _, problem := range declaredFieldsDiffer(typeKey, valueOf(op.task), valueOf(op.plan.Observed)) {
					t.Errorf("%s: %s", op.name, problem)
				}
				if want := declaresPresent(op.task); op.plan.Exists != want {
					t.Errorf("%s: Exists = %v, want %v from the declared state", op.name, op.plan.Exists, want)
				}

				ctx := subprocess.ContextWithRunner(testCtx(), observedRunner(t, withObjectProbes(fixture, op.task)))
				got, exists, err := ReadTask(ctx, op.task)
				if err != nil {
					t.Errorf("%s: ReadTask: %v", op.name, err)
					continue
				}
				if !reflect.DeepEqual(got, op.plan.Observed) || exists != op.plan.Exists {
					t.Errorf("%s: ReadTask returned %#v (exists %v), Plan observed %#v (exists %v)", op.name, got, exists, op.plan.Observed, op.plan.Exists)
				}
			}
			if inSync == 0 {
				t.Error("no plan in the fixture is in sync: add a case whose task the server already holds")
			}
		})
	}
}

// declaredFieldsDiffer returns a problem for each field the declared struct
// sets - neither left at its zero value nor tagged write_only or apply_only -
// that observed does not carry as declared, descending into nested structs
// and the elements of a list of structs with identity keys, which must come
// back in the declared order.
func declaredFieldsDiffer(where string, declared, observed reflect.Value) []string {
	var problems []string
	rt := declared.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if !field.IsExported() || field.Tag.Get("write_only") == "true" || field.Tag.Get("apply_only") == "true" {
			continue
		}
		dv, ov := declared.Field(i), observed.Field(i)
		if isUnset(dv) {
			continue
		}
		name := where + "." + yamlFieldName(field)
		switch {
		case dv.Kind() == reflect.Struct:
			problems = append(problems, declaredFieldsDiffer(name, dv, ov)...)
		case dv.Kind() == reflect.Slice && len(itemKeyFields(dv.Type().Elem())) > 0:
			if dv.Len() != ov.Len() {
				problems = append(problems, fmt.Sprintf("%s holds %d items, declared %d", name, ov.Len(), dv.Len()))
				continue
			}
			for j := 0; j < dv.Len(); j++ {
				problems = append(problems, declaredFieldsDiffer(fmt.Sprintf("%s[%d]", name, j), dv.Index(j), ov.Index(j))...)
			}
		case !reflect.DeepEqual(dv.Interface(), ov.Interface()):
			problems = append(problems, fmt.Sprintf("%s = %#v, declared %#v", name, ov.Interface(), dv.Interface()))
		}
	}
	return problems
}

// withObjectProbes answers, as existing, the probe a read makes for each app
// or service task depends on, unless the fixture already answers it. ReadTask
// asks whether the parent exists whenever the read does not find the
// resource, which an in-sync plan of an absent state never does.
func withObjectProbes(f observedFixture, task Task) observedFixture {
	responses := make(map[string]string, len(f.responses))
	for key, out := range f.responses {
		responses[key] = out
	}
	for _, object := range TaskObjects(task) {
		if !object.Set || object.Creates {
			continue
		}
		key := objectExistsKey(object)
		if _, ok := f.exitCodes[key]; ok {
			continue
		}
		if _, ok := responses[key]; !ok {
			responses[key] = ""
		}
	}
	f.responses = responses
	return f
}

// declaredItem and declaredFixtureTask exercise declaredObserved without a
// registered task: a list of keyed structs, write_only and apply_only fields
// at both levels, and an apply_only field with a default.
type declaredItem struct {
	Key    string `identity:"key" yaml:"key"`
	Note   string `yaml:"note"`
	Secret string `write_only:"true" yaml:"secret"`
}

type declaredFixtureTask struct {
	App    string         `identity:"key" yaml:"app"`
	Pairs  map[string]int `yaml:"pairs"`
	Image  string         `yaml:"image"`
	Secret string         `write_only:"true" yaml:"secret"`
	Knob   string         `apply_only:"true" default:"on" yaml:"knob"`
	Items  []declaredItem `yaml:"items"`
	State  State          `yaml:"state"`
}

func (t declaredFixtureTask) Plan(context.Context) PlanResult { return PlanResult{} }

func (t declaredFixtureTask) Execute(context.Context) TaskOutputState { return TaskOutputState{} }

// TestDeclaredObservedKeepsDeclaredFields pins how an in-sync Observed is
// built: every field the declared task sets wins, a field it leaves unset
// keeps the read - inside list elements matched on their identity keys too -
// tagged fields drop to their zero value or default, and the result shares no
// map or slice with either side.
func TestDeclaredObservedKeepsDeclaredFields(t *testing.T) {
	t.Parallel()
	declared := &declaredFixtureTask{
		App:    "web",
		Pairs:  map[string]int{"web": 2, "worker": 0},
		Secret: "hunter2",
		Knob:   "off",
		Items:  []declaredItem{{Key: "b", Note: "mine", Secret: "x"}, {Key: "a"}, {Key: "new"}},
		State:  StateSet,
	}
	read := declaredFixtureTask{
		App:   "web",
		Pairs: map[string]int{"web": 2},
		Image: "postgres:17.2",
		Knob:  "on",
		Items: []declaredItem{{Key: "a", Note: "server a"}, {Key: "b", Note: "server b"}},
		State: StateSet,
	}

	got := declaredObserved(declared, read)
	want := declaredFixtureTask{
		App:   "web",
		Pairs: map[string]int{"web": 2, "worker": 0},
		Image: "postgres:17.2",
		Knob:  "on",
		Items: []declaredItem{{Key: "b", Note: "mine"}, {Key: "a", Note: "server a"}, {Key: "new"}},
		State: StateSet,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("declaredObserved\n got: %#v\nwant: %#v", got, want)
	}

	observed := got.(declaredFixtureTask)
	observed.Pairs["web"] = 9
	observed.Items[0].Note = "changed"
	observed.Items[1].Note = "changed"
	if declared.Pairs["web"] != 2 || declared.Items[0].Note != "mine" || read.Items[0].Note != "server a" {
		t.Errorf("Observed shares a map or slice with its inputs: declared %#v, read %#v", declared, read)
	}
}

// TestDeclaredObservedWithoutARead pins that a nil read leaves every field as
// declared, tagged ones reset.
func TestDeclaredObservedWithoutARead(t *testing.T) {
	t.Parallel()
	got := declaredObserved(declaredFixtureTask{App: "web", Secret: "s", Knob: "off", State: StatePresent}, nil)
	want := declaredFixtureTask{App: "web", Knob: "on", State: StatePresent}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %#v; want %#v", got, want)
	}
}

// TestInSyncPsScaleObservesTheDeclaredFormation covers the case #637 reports:
// a set formation naming a process type at zero plans in sync, and both Plan
// and ReadTask return the formation as declared, while ReadAddress still
// returns the one the server reports.
func TestInSyncPsScaleObservesTheDeclaredFormation(t *testing.T) {
	t.Parallel()
	fixture := observedFixtures()["dokku_ps_scale"]
	ctx := subprocess.ContextWithRunner(testCtx(), observedRunner(t, fixture))

	task := PsScaleTask{App: "web", Scale: map[string]int{"web": 2, "worker": 0, "clock": 1}, State: StateSet}
	p := task.Plan(ctx)
	if !p.InSync {
		t.Fatalf("plan reports drift: %s %v", p.Reason, p.Mutations)
	}
	if !reflect.DeepEqual(p.Observed, task) || !p.Exists {
		t.Errorf("Observed = %#v (exists %v), want the declared task", p.Observed, p.Exists)
	}
	got, exists, err := ReadTask(ctx, task)
	if err != nil || !reflect.DeepEqual(got, task) || !exists {
		t.Errorf("ReadTask = %#v, %v, %v; want the declared task", got, exists, err)
	}

	idle := PsScaleTask{App: "idle", Scale: map[string]int{"web": 0}, State: StateSet}
	if p := idle.Plan(ctx); !p.InSync || !reflect.DeepEqual(p.Observed, idle) || !p.Exists {
		t.Errorf("idle: InSync %v, Observed %#v, Exists %v; want the declared task, existing", p.InSync, p.Observed, p.Exists)
	}

	got, exists, err = ReadAddress(ctx, "dokku_ps_scale[app=web]")
	want := PsScaleTask{App: "web", Scale: map[string]int{"web": 2, "clock": 1}, State: StateSet}
	if err != nil || !reflect.DeepEqual(got, want) || !exists {
		t.Errorf("ReadAddress = %#v, %v, %v; want the formation the server reports", got, exists, err)
	}
}
