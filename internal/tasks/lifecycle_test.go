package tasks

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	yaml "gopkg.in/yaml.v3"

	"github.com/dokku/docket/internal/subprocess"
)

// undoes maps an apply state to the teardown state that removes what it
// declared: absent undoes present, and clear undoes set.
var undoes = map[State]State{
	StatePresent: StateAbsent,
	StateSet:     StateClear,
}

// TestTaskLifecycles holds every registered task to its lifecycle: the apply
// state is one the task offers, the teardown state is one it offers too and is
// the state that undoes the apply, an imperative task has no teardown and
// every other task has one unless it declares none, and the catalog
// publishes the same pair. It also pins which tasks declare a lifecycle the
// generic rule would not derive: dokku_storage_ensure, which offers absent but
// refuses it.
func TestTaskLifecycles(t *testing.T) {
	t.Parallel()
	catalog := wholeCatalog(t)
	published := map[string]LifecycleSchema{}
	for _, schema := range catalog.Tasks {
		published[schema.Type] = schema.Lifecycle
	}

	var declared []string
	for _, name := range TaskTypes() {
		task, _ := Lookup(name)
		if _, ok := task.(LifecycleDocer); ok {
			declared = append(declared, name)
		}
		lifecycle := TaskLifecycle(task)
		options := stateOptions(task)
		if !containsState(options, lifecycle.Apply) {
			t.Errorf("%s: apply state %q is not one of its states %v", name, lifecycle.Apply, options)
		}
		if published[name] != lifecycle {
			t.Errorf("%s: catalog lifecycle = %+v, TaskLifecycle = %+v", name, published[name], lifecycle)
		}
		if TaskImperative(task) {
			if lifecycle.Teardown != "" {
				t.Errorf("%s: imperative task has teardown state %q", name, lifecycle.Teardown)
			}
			continue
		}
		if _, ok := task.(LifecycleDocer); ok && lifecycle.Teardown == "" {
			continue
		}
		if !containsState(options, lifecycle.Teardown) {
			t.Errorf("%s: teardown state %q is not one of its states %v", name, lifecycle.Teardown, options)
		}
		if undoes[lifecycle.Apply] != lifecycle.Teardown {
			t.Errorf("%s: teardown %q does not undo apply %q; want %q", name, lifecycle.Teardown, lifecycle.Apply, undoes[lifecycle.Apply])
		}
	}
	if want := []string{"dokku_storage_ensure"}; !reflect.DeepEqual(declared, want) {
		t.Errorf("tasks declaring their own lifecycle = %v; want %v, and any other explained in docs/embedding.md and pinned here", declared, want)
	}
}

// TestTaskLifecycleRule pins the generic rule on the tasks the rule exists
// for: set beats the default, clear beats absent, and a task with neither set
// nor a teardown applies with its default.
func TestTaskLifecycleRule(t *testing.T) {
	t.Parallel()
	cases := map[string]LifecycleSchema{
		"dokku_app":              {Apply: StatePresent, Teardown: StateAbsent},
		"dokku_config":           {Apply: StateSet, Teardown: StateClear},
		"dokku_ps_scale":         {Apply: StateSet, Teardown: StateClear},
		"dokku_app_clone":        {Apply: StatePresent},
		"dokku_git_sync":         {Apply: StatePresent},
		"dokku_git_from_archive": {Apply: StateDeployed},
		"dokku_git_from_image":   {Apply: StateDeployed},
		"dokku_storage_ensure":   {Apply: StatePresent},
	}
	for name, want := range cases {
		task, ok := Lookup(name)
		if !ok {
			t.Fatalf("%s is not registered", name)
		}
		if got := TaskLifecycle(task); got != want {
			t.Errorf("%s: lifecycle = %+v; want %+v", name, got, want)
		}
	}
}

// lifecycleOverrideTask is a registered task that declares its own
// lifecycle, standing in for a task the generic rule does not fit.
type lifecycleOverrideTask struct {
	AppTask
}

func (lifecycleOverrideTask) Lifecycle() LifecycleSchema {
	return LifecycleSchema{Apply: StateAbsent}
}

// TestTaskLifecycleDeclared checks that a declared lifecycle replaces the
// derived one.
func TestTaskLifecycleDeclared(t *testing.T) {
	t.Parallel()
	got := TaskLifecycle(&lifecycleOverrideTask{})
	if want := (LifecycleSchema{Apply: StateAbsent}); got != want {
		t.Errorf("lifecycle = %+v; want the declared %+v", got, want)
	}
}

// TestTaskInStateResetsForbiddenFields moves tasks into states that refuse
// some of their fields: each refused field is reset to its default, or to its
// zero value without one, every other field is kept, and the task passed in is
// left as it was.
func TestTaskInStateResetsForbiddenFields(t *testing.T) {
	t.Parallel()

	skip := true
	scale := &PsScaleTask{App: "api", Scale: map[string]int{"web": 2}, SkipDeploy: &skip, State: StateSet}
	moved, err := TaskInState(scale, StateClear)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := moved.(*PsScaleTask)
	if !ok {
		t.Fatalf("moved task is %T; want *PsScaleTask", moved)
	}
	if want := (&PsScaleTask{App: "api", SkipDeploy: &skip, State: StateClear}); !reflect.DeepEqual(got, want) {
		t.Errorf("moved = %+v; want %+v", got, want)
	}
	if scale.State != StateSet || scale.Scale["web"] != 2 {
		t.Errorf("the task passed in changed: %+v", scale)
	}

	// image_drift has a default, so it returns to it rather than to "".
	service := ServiceCreateTask{Service: "redis", Name: "cache", Image: "redis", ImageDrift: "error", PostStartNetwork: []string{"net"}, State: StatePresent}
	movedService, err := TaskInState(service, StateAbsent)
	if err != nil {
		t.Fatal(err)
	}
	gotService, ok := movedService.(ServiceCreateTask)
	if !ok {
		t.Fatalf("moved task is %T; want a ServiceCreateTask value", movedService)
	}
	if want := (ServiceCreateTask{Service: "redis", Name: "cache", ImageDrift: "warn", State: StateAbsent}); !reflect.DeepEqual(gotService, want) {
		t.Errorf("moved = %+v; want %+v", gotService, want)
	}
}

// TestTaskInStateSharesNothing checks that the copy owns its lists and maps,
// so changing it leaves the task passed in alone.
func TestTaskInStateSharesNothing(t *testing.T) {
	t.Parallel()
	domains := &DomainsTask{App: "api", Domains: []string{"a.example.com"}, State: StatePresent}
	moved, err := TaskInState(domains, StateSet)
	if err != nil {
		t.Fatal(err)
	}
	moved.(*DomainsTask).Domains[0] = "changed"
	if domains.Domains[0] != "a.example.com" {
		t.Errorf("changing the copy changed the task passed in: %v", domains.Domains)
	}
}

// TestTaskInStateRefusesAnUnofferedState checks the error: a state the task
// does not offer, including an imperative task's empty teardown.
func TestTaskInStateRefusesAnUnofferedState(t *testing.T) {
	t.Parallel()
	if _, err := TaskInState(&PsScaleTask{App: "api"}, StateAbsent); err == nil || !strings.Contains(err.Error(), `offers no state "absent"`) {
		t.Errorf("err = %v; want a refusal of absent", err)
	}
	sync := &GitSyncTask{App: "api"}
	if _, err := TaskInState(sync, TaskLifecycle(sync).Teardown); err == nil {
		t.Error("an imperative task moved into an empty teardown state")
	}
	if _, err := TaskInState(nil, StatePresent); err == nil {
		t.Error("a nil task moved into a state")
	}
}

// lifecycleElement and lifecycleHolder stand in for a task whose list
// elements carry a forbidden_when, which no registered task declares today.
type lifecycleElement struct {
	Name  string `yaml:"name"`
	Extra string `yaml:"extra" forbidden_when:"clear" default:"keep"`
}

type lifecycleHolder struct {
	Items []lifecycleElement `yaml:"items"`
}

// TestResetForbiddenFieldsDescendsIntoElements checks that an element field
// refused in the state is reset in every element.
func TestResetForbiddenFieldsDescendsIntoElements(t *testing.T) {
	t.Parallel()
	holder := lifecycleHolder{Items: []lifecycleElement{{Name: "a", Extra: "x"}, {Name: "b", Extra: "y"}}}
	rv := reflect.ValueOf(&holder).Elem()
	resetForbiddenFields(rv, StateClear)
	want := lifecycleHolder{Items: []lifecycleElement{{Name: "a", Extra: "keep"}, {Name: "b", Extra: "keep"}}}
	if !reflect.DeepEqual(holder, want) {
		t.Errorf("holder = %+v; want %+v", holder, want)
	}
}

// TestTaskInStateKeepsExamplesValid moves every documented example of every
// task into its lifecycle states and validates the result the way `docket
// validate` does. Every example moves into the teardown state. An example
// written in present, set or deployed moves into the apply state too, but one
// written in absent or clear does not: TaskInState removes what a state
// refuses and invents nothing, so a property example in state absent has no
// `value` for present to require.
func TestTaskInStateKeepsExamplesValid(t *testing.T) {
	t.Parallel()
	for name, prototype := range allRegisteredTasks() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			lifecycle := TaskLifecycle(prototype)
			examples, err := TaskExamples(prototype)
			if err != nil {
				t.Fatal(err)
			}
			for _, example := range examples {
				task := decodeExampleTask(t, example)
				if task == nil {
					continue
				}
				if err := lifecycleValidation(task); err != nil {
					t.Fatalf("example %q does not validate as written: %v", example.Name, err)
				}
				state, _ := taskStateOf(task)
				var targets []State
				if state != StateAbsent && state != StateClear {
					targets = append(targets, lifecycle.Apply)
				}
				if lifecycle.Teardown != "" {
					targets = append(targets, lifecycle.Teardown)
				}
				for _, target := range targets {
					moved, err := TaskInState(task, target)
					if err != nil {
						t.Errorf("example %q: TaskInState(%s): %v", example.Name, target, err)
						continue
					}
					if err := lifecycleValidation(moved); err != nil {
						t.Errorf("example %q moved to %s: %v", example.Name, target, err)
					}
				}
			}
		})
	}
}

// decodeExampleTask decodes an example's one task the way the loader does.
func decodeExampleTask(t *testing.T, example Doc) Task {
	t.Helper()
	var body map[string]yaml.Node
	if err := yaml.Unmarshal([]byte(example.Codeblock), &body); err != nil {
		t.Fatalf("example %q: %v", example.Name, err)
	}
	for typeKey, node := range body {
		raw, err := yaml.Marshal(&node)
		if err != nil {
			t.Fatalf("example %q: %v", example.Name, err)
		}
		task, err := decodeTaskBytes(typeKey, raw)
		if err != nil {
			t.Fatalf("example %q: %v", example.Name, err)
		}
		return task
	}
	return nil
}

// lifecycleValidation is what `docket validate` checks of a task body: its
// required keys are set, it breaks no declared constraint, and its own
// Validate() passes.
func lifecycleValidation(task Task) error {
	rv := valueOf(task)
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if field.Tag.Get("required") == "true" && rv.Field(i).IsZero() {
			return fmt.Errorf("missing required field %q", recipeKey(field))
		}
	}
	if violations := ConstraintViolations(task); len(violations) > 0 {
		return violations[0]
	}
	if validator, ok := task.(InputValidator); ok {
		return validator.Validate()
	}
	return nil
}

// TestLifecycleNote pins the Lifecycle section a reference page prints: the
// two states, the fields a teardown resets, and a task with no teardown.
func TestLifecycleNote(t *testing.T) {
	t.Parallel()
	see := " See [Applying and tearing down a task](../embedding.md#applying-and-tearing-down-a-task)."
	cases := map[string]string{
		"dokku_app":            "Applied with state `present` and torn down with state `absent`.",
		"dokku_ps_scale":       "Applied with state `set` and torn down with state `clear`, which refuses `scale`, so a teardown resets it.",
		"dokku_resource_limit": "Applied with state `set` and torn down with state `clear`, which refuses `resources` and `clear_before`, so a teardown resets them.",
		"dokku_git_sync":       "Applied with state `present` and has no teardown state, so tearing it down leaves the server unchanged.",
	}
	seen := 0
	for _, schema := range wholeCatalog(t).Tasks {
		want, ok := cases[schema.Type]
		if !ok {
			continue
		}
		seen++
		note := noteOf(schema.Notes, NoteLifecycle)
		if note == nil || note.Markdown != "## Lifecycle\n\n"+want+see || note.States != nil {
			t.Errorf("%s: lifecycle note = %+v; want %q with no per-state text", schema.Type, note, want)
		}
	}
	if seen != len(cases) {
		t.Errorf("found %d of the %d tasks", seen, len(cases))
	}
}

// TestTeardownUndoesApply plans the lifecycle against each task's observed
// fixture, a server holding the resources its export reads back, and checks
// that the two states disagree about each of them. A resource exported in the
// apply state plans in sync there, plans as drift in the teardown state, and
// applying that teardown ends in the teardown state, so the teardown removes
// what the apply declared. A resource exported in the teardown state, such as
// a disabled toggle, plans as drift in the apply state. One exported in
// another state, such as a single storage mount on a task applied with set,
// still has something for the teardown to remove.
func TestTeardownUndoesApply(t *testing.T) {
	t.Parallel()
	fixtures := observedFixtures()
	var names []string
	for name := range fixtures {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, typeKey := range names {
		fixture := fixtures[typeKey]
		prototype, _ := Lookup(typeKey)
		lifecycle := TaskLifecycle(prototype)
		if fixture.export == nil || lifecycle.Teardown == "" {
			continue
		}
		t.Run(typeKey, func(t *testing.T) {
			t.Parallel()
			ctx := subprocess.ContextWithRunner(testCtx(), observedRunner(t, fixture))
			bodies, err := fixture.export(ctx)
			if err != nil {
				t.Fatalf("export: %v", err)
			}
			for i, body := range bodies {
				task := withDefaults(body).(Task)
				state, _ := taskStateOf(task)
				if state == lifecycle.Teardown {
					apply, err := TaskInState(task, lifecycle.Apply)
					if err != nil {
						t.Fatalf("body %d: %v", i, err)
					}
					if plan := apply.Plan(ctx); plan.Error != nil || plan.InSync {
						t.Errorf("body %d exported in %s: %s plan = {InSync %v, Error %v}; want drift", i, state, lifecycle.Apply, plan.InSync, plan.Error)
					}
					continue
				}
				if state == lifecycle.Apply {
					if plan := task.Plan(ctx); plan.Error != nil || !plan.InSync {
						t.Errorf("body %d in %s: plan = {InSync %v, Error %v, Reason %q}; want in sync", i, state, plan.InSync, plan.Error, plan.Reason)
					}
				}

				teardown, err := TaskInState(task, lifecycle.Teardown)
				if err != nil {
					t.Fatalf("body %d: %v", i, err)
				}
				plan := teardown.Plan(ctx)
				if plan.Error != nil || plan.InSync {
					t.Errorf("body %d exported in %s: %s plan = {InSync %v, Error %v}; want drift to remove", i, state, lifecycle.Teardown, plan.InSync, plan.Error)
					continue
				}
				applyCtx := subprocess.ContextWithRunner(testCtx(), acceptingRunner(fixture))
				out := ExecutePlan(applyCtx, plan)
				if out.Error != nil || !out.Changed || out.State != lifecycle.Teardown {
					t.Errorf("body %d: teardown apply = {Error %v, Changed %v, State %q}; want a change ending in %s", i, out.Error, out.Changed, out.State, lifecycle.Teardown)
				}
			}
		})
	}
}
