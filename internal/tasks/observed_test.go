package tasks

import (
	"context"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	defaults "github.com/mcuadros/go-defaults"

	"github.com/dokku/docket/internal/subprocess"
)

// observedFixture is a fake server and the plans PlanResult.Observed is checked
// against on it.
type observedFixture struct {
	// responses answers each command by its joined arguments. A command not
	// listed fails the test rather than answering empty, so a fixture states
	// every read its plans make.
	responses map[string]string

	// exitCodes makes a listed command exit non-zero instead, the way a probe
	// such as `apps:exists` reports an absent resource.
	exitCodes map[string]int

	// export reads the fixture back through the task's exporter. Every body
	// it returns is planned, and must plan in sync and observe itself. Set for
	// every task that exports.
	export func(ctx context.Context) ([]interface{}, error)

	// cases are plans whose Observed is stated outright. Every fixture carries
	// at least one whose resource is absent.
	cases []observedCase

	// addressCases are ReadAddress reads whose result is stated outright.
	// Every exported body is also read back from its address, so these cover
	// what export cannot: a task that does not export, and an absent resource.
	addressCases []observedAddressCase
}

// observedAddressCase is one address read against a fixture and what it must
// return.
type observedAddressCase struct {
	address string
	want    Task
	exists  bool
}

// observedCase is one task planned against a fixture and what it must read.
type observedCase struct {
	name   string
	task   Task
	want   Task
	exists bool
}

// observedRunner answers from a fixture and fails t on any command the fixture
// does not list.
func observedRunner(t *testing.T, f observedFixture) func(context.Context, subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
	return func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		key := strings.Join(in.Args, " ")
		if code, ok := f.exitCodes[key]; ok {
			res := subprocess.ExecCommandResponse{ExitCode: code, Stderr: "exit " + key}
			return res, &subprocess.ExecError{Response: res, Err: errors.New(res.Stderr), Ran: true}
		}
		out, ok := f.responses[key]
		if !ok {
			t.Errorf("unexpected command: %s %s", in.Command, key)
			return subprocess.ExecCommandResponse{}, fmt.Errorf("unexpected command %q", key)
		}
		return subprocess.ExecCommandResponse{Stdout: out}, nil
	}
}

// observedFixtureSets are the functions that contribute fixtures, one per
// observed_*_test.go file, each registered from that file's init.
var observedFixtureSets []func() map[string]observedFixture

// registerObservedFixtures adds a file's fixtures to observedFixtures.
func registerObservedFixtures(set func() map[string]observedFixture) {
	observedFixtureSets = append(observedFixtureSets, set)
}

func init() {
	registerObservedFixtures(observedCoreFixtures)
}

// observedFixtures holds one fixture per registered task that can read its
// state. TestObservedFixturesCoverEveryTask fails when one is missing, and a
// task given fixtures by two files fails TestObservedFixtureSetsAreDisjoint.
func observedFixtures() map[string]observedFixture {
	out := map[string]observedFixture{}
	for _, set := range observedFixtureSets {
		for name, fixture := range set() {
			out[name] = fixture
		}
	}
	for name, cases := range observedAddressCases() {
		if fixture, ok := out[name]; ok {
			fixture.addressCases = append(fixture.addressCases, cases...)
			out[name] = fixture
		}
	}
	for name, fixture := range out {
		out[name] = withParentProbes(fixture)
	}
	return out
}

// withParentProbes answers, as existing, the probe a read makes for each app or
// service an address case's task depends on, unless the fixture already
// answers it. A read that does not find its resource asks whether the parent
// exists, so every absent address case makes that probe.
func withParentProbes(f observedFixture) observedFixture {
	responses := make(map[string]string, len(f.responses))
	for key, out := range f.responses {
		responses[key] = out
	}
	for _, c := range f.addressCases {
		_, task, err := taskFromAddress(c.address)
		if err != nil {
			continue
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
	}
	f.responses = responses
	return f
}

// objectExistsKey is the joined arguments of the probe objectExists runs for
// object.
func objectExistsKey(object TaskObject) string {
	if object.Kind == ObjectApp {
		return "--quiet apps:exists " + object.Values[0]
	}
	return fmt.Sprintf("--quiet %s:exists %s", object.Values[0], object.Values[1])
}

// TestObservedFixtureSetsAreDisjoint catches two files fixturing one task,
// where observedFixtures would silently keep only one of them.
func TestObservedFixtureSetsAreDisjoint(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, set := range observedFixtureSets {
		for name := range set() {
			if seen[name] {
				t.Errorf("task %q has observed fixtures in two files", name)
			}
			seen[name] = true
		}
	}
}

// exportAppFrom reads app "web" back through an app exporter.
func exportAppFrom(task AppExporter) func(ctx context.Context) ([]interface{}, error) {
	return func(ctx context.Context) ([]interface{}, error) { return task.ExportApp(ctx, "web") }
}

// exportGlobalFrom reads the server back through a global exporter.
func exportGlobalFrom(task GlobalExporter) func(ctx context.Context) ([]interface{}, error) {
	return task.ExportGlobal
}

// observedCoreFixtures covers dokku_app and dokku_config, the two the
// fixture format was first written against.
func observedCoreFixtures() map[string]observedFixture {
	app := exportAppFrom

	return map[string]observedFixture{
		"dokku_app": {
			responses: map[string]string{"--quiet apps:exists web": ""},
			exitCodes: map[string]int{"--quiet apps:exists gone": 1},
			export:    app(AppTask{}),
			cases: []observedCase{
				{name: "absent", task: AppTask{App: "gone", State: StatePresent}, want: AppTask{App: "gone", State: StateAbsent}},
				{name: "present, planned absent", task: AppTask{App: "web", State: StateAbsent}, want: AppTask{App: "web", State: StatePresent}, exists: true},
			},
		},
		"dokku_config": {
			responses: map[string]string{
				"--quiet config:export --format json web":   `{"KEY":"one","OTHER":"two","DATABASE_URL":"postgres://u:p@db:5432/x"}`,
				"--quiet config:export --format json empty": `{}`,
				"git:report web --git-rev-env-var":          "GIT_REV",
				"--quiet plugin:trigger service-list":       "",
			},
			export: app(ConfigTask{}),
			cases: []observedCase{
				{
					name:   "present reads only the declared keys",
					task:   ConfigTask{App: "web", Config: map[string]string{"KEY": "changed", "MISSING": "x"}, State: StatePresent},
					want:   ConfigTask{App: "web", Config: map[string]string{"KEY": "one"}, State: StatePresent},
					exists: true,
				},
				{
					name: "absent",
					task: ConfigTask{App: "web", Config: map[string]string{"MISSING": ""}, State: StateAbsent},
					want: ConfigTask{App: "web", State: StateAbsent},
				},
				{
					name:   "clear on an empty app",
					task:   ConfigTask{App: "empty", State: StateClear},
					want:   ConfigTask{App: "empty", State: StateClear},
					exists: true,
				},
				{
					name:   "set of nothing on an empty app",
					task:   ConfigTask{App: "empty", Config: map[string]string{}, State: StateSet},
					want:   ConfigTask{App: "empty", State: StateClear},
					exists: true,
				},
			},
		},
	}
}

// observedPlan is one plan a fixture makes: an exported body planned back, or
// a case. task is what was planned, want is the task its probe must read, and
// exported is set for a body.
type observedPlan struct {
	name     string
	task     Task
	plan     PlanResult
	want     interface{}
	exists   bool
	exported bool
}

// planObservedFixture plans every exported body and every case of a fixture,
// failing t on an export or plan error.
func planObservedFixture(t *testing.T, fixture observedFixture) []observedPlan {
	t.Helper()
	ctx := subprocess.ContextWithRunner(testCtx(), observedRunner(t, fixture))
	var plans []observedPlan
	if fixture.export != nil {
		bodies, err := fixture.export(ctx)
		if err != nil {
			t.Fatalf("export: %v", err)
		}
		if len(bodies) == 0 {
			t.Fatal("the fixture exported nothing, so Observed went unchecked")
		}
		for i, body := range bodies {
			// A recipe gets its defaults when it is loaded, so the body is
			// planned the way a re-read export would be.
			task, ok := withDefaults(body).(Task)
			if !ok {
				t.Fatalf("body %d is %T, not a Task", i, body)
			}
			p := task.Plan(ctx)
			if p.Error != nil {
				t.Fatalf("body %d: plan error: %v", i, p.Error)
			}
			plans = append(plans, observedPlan{name: fmt.Sprintf("body %d", i), task: task, plan: p, want: body, exists: declaresPresent(task), exported: true})
		}
	}
	for _, c := range fixture.cases {
		p := c.task.Plan(ctx)
		if p.Error != nil {
			t.Errorf("%s: plan error: %v", c.name, p.Error)
			continue
		}
		plans = append(plans, observedPlan{name: c.name, task: c.task, plan: p, want: c.want, exists: c.exists})
	}
	return plans
}

// TestPlanObservesServerState checks what each plan's probe reads against
// every fixture: an exported body plans in sync and reads itself, and each
// case reads what it states. A plan reporting drift observes exactly that
// read; TestInSyncPlanObservesTheDeclaredTask covers the in-sync half.
func TestPlanObservesServerState(t *testing.T) {
	t.Parallel()
	for typeKey, fixture := range observedFixtures() {
		t.Run(typeKey, func(t *testing.T) {
			t.Parallel()
			want := registeredTypeOrNil(typeKey)
			for _, op := range planObservedFixture(t, fixture) {
				if op.exported && !op.plan.InSync {
					t.Errorf("%s: exported body plans as drift: %s %v", op.name, op.plan.Reason, op.plan.Mutations)
				}
				if !op.plan.InSync && (!reflect.DeepEqual(op.plan.Observed, op.plan.read) || op.plan.Exists != op.plan.readExists) {
					t.Errorf("%s: a drift plan's Observed %#v (exists %v) is not what the probe read, %#v (exists %v)", op.name, op.plan.Observed, op.plan.Exists, op.plan.read, op.plan.readExists)
				}
				checkObserved(t, op.name, typeKey, want, op.plan, op.want, op.exists)
			}
		})
	}
}

// TestObservedReadsEveryUntaggedField is the read-path half of the catalog's
// write_only and apply_only flags (#616): a consumer refreshing state through
// sdk.ReadTask keeps the configured value only for a tagged field, so a field
// Observed never fills must carry one of the tags. Every field that is not an
// identity key - those are copied from the planned task, not read - or tagged
// must be set, away from its zero value and default, in at least one Observed
// across the task's fixture, element fields of a list of structs included.
func TestObservedReadsEveryUntaggedField(t *testing.T) {
	t.Parallel()
	for typeKey, fixture := range observedFixtures() {
		t.Run(typeKey, func(t *testing.T) {
			t.Parallel()
			var observed []reflect.Value
			for _, op := range planObservedFixture(t, fixture) {
				// The probe's read, not Observed: an in-sync plan observes
				// the declared task, which sets whatever the fixture does.
				if op.plan.read != nil {
					observed = append(observed, reflect.ValueOf(op.plan.read))
				}
			}
			for _, name := range unobservedFields(typeKey, registeredTypeOrNil(typeKey), observed) {
				t.Errorf("%s is never observed: tag it write_only or apply_only, or extend the fixture so the server reports it", name)
			}
		})
	}
}

// unobservedFields returns the fields of rt that are neither identity keys nor
// tagged write_only or apply_only and that no value in values sets away from
// its zero value or default, descending into the elements of a slice of
// structs.
func unobservedFields(where string, rt reflect.Type, values []reflect.Value) []string {
	var missing []string
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if !field.IsExported() || field.Tag.Get("yaml") == "-" {
			continue
		}
		if field.Tag.Get("identity") == string(IdentityRoleKey) || field.Tag.Get("write_only") == "true" || field.Tag.Get("apply_only") == "true" {
			continue
		}
		name := where + "." + yamlFieldName(field)
		set := false
		var elems []reflect.Value
		for _, v := range values {
			for v.Kind() == reflect.Pointer || v.Kind() == reflect.Interface {
				v = v.Elem()
			}
			fv := v.Field(i)
			switch fv.Kind() {
			case reflect.Slice, reflect.Map:
				if fv.Len() > 0 {
					set = true
				}
				if fv.Kind() == reflect.Slice {
					for j := 0; j < fv.Len(); j++ {
						elems = append(elems, fv.Index(j))
					}
				}
			default:
				if !isZeroOrDefault(fv, field.Tag.Get("default")) {
					set = true
				}
			}
		}
		if !set {
			missing = append(missing, name)
			continue
		}
		if field.Type.Kind() != reflect.Slice {
			continue
		}
		if elem := structElem(field.Type.Elem()); elem != nil {
			missing = append(missing, unobservedFields(name+"[]", elem, elems)...)
		}
	}
	return missing
}

// TestUnobservedFieldsCatchesAGap proves TestObservedReadsEveryUntaggedField
// is not vacuous: a field no Observed sets away from its default is reported,
// an element field of a list of structs included, while identity keys and
// tagged fields are not.
func TestUnobservedFieldsCatchesAGap(t *testing.T) {
	t.Parallel()
	type item struct {
		Key  string `identity:"key" yaml:"key"`
		Note string `yaml:"note"`
	}
	type task struct {
		App    string `identity:"key" yaml:"app"`
		Secret string `write_only:"true" yaml:"secret"`
		Knob   bool   `apply_only:"true" yaml:"knob"`
		Items  []item `yaml:"items"`
		Size   string `default:"1" yaml:"size"`
	}
	got := unobservedFields("task", reflect.TypeOf(task{}), []reflect.Value{
		reflect.ValueOf(task{App: "web", Items: []item{{Key: "a"}}, Size: "1"}),
	})
	if want := []string{"task.items[].note", "task.size"}; !reflect.DeepEqual(got, want) {
		t.Errorf("got %q; want %q", got, want)
	}
	got = unobservedFields("task", reflect.TypeOf(task{}), []reflect.Value{
		reflect.ValueOf(task{Items: []item{{Note: "n"}}, Size: "2"}),
	})
	if len(got) != 0 {
		t.Errorf("a read setting every untagged field reported %q", got)
	}
}

// TestReadAddressReadsEveryResource checks ReadAddress against every fixture:
// each exported body that declares its resource there is read back from its
// identity address alone and must come back whole - every field the body
// carries, whatever state the read settles on - and each address case returns
// what it states. Every task that can read its state needs an address read of
// a present resource and of an absent one - for a collection, of an empty
// collection, which exists - so a task whose keys alone cannot plan a whole
// read fails here until it gets an addressReader.
func TestReadAddressReadsEveryResource(t *testing.T) {
	t.Parallel()
	for typeKey, fixture := range observedFixtures() {
		t.Run(typeKey, func(t *testing.T) {
			t.Parallel()
			ctx := subprocess.ContextWithRunner(testCtx(), observedRunner(t, fixture))
			present, absent := false, false

			if fixture.export != nil {
				bodies, err := fixture.export(ctx)
				if err != nil {
					t.Fatalf("export: %v", err)
				}
				for _, body := range bodies {
					if !declaresPresent(withDefaults(body).(Task)) {
						continue
					}
					address := IdentityAddress(typeKey, body.(Task))
					got, exists, err := ReadAddress(ctx, address)
					if err != nil {
						t.Errorf("%s: %v", address, err)
						continue
					}
					present = true
					if !exists {
						t.Errorf("%s: exists = false for an exported resource", address)
					}
					gotFields := withField(normalizeObserved(got), "State")
					wantFields := withField(normalizeObserved(body), "State")
					if !reflect.DeepEqual(gotFields, wantFields) {
						t.Errorf("%s: read back differs from the export\n got: %#v\nwant: %#v", address, gotFields, wantFields)
					}
				}
			}

			for _, c := range fixture.addressCases {
				got, exists, err := ReadAddress(ctx, c.address)
				if err != nil {
					t.Errorf("%s: %v", c.address, err)
					continue
				}
				switch {
				case c.exists && emptyWholeCollection(c.want):
					absent = true
				case c.exists:
					present = true
				default:
					absent = true
				}
				if exists != c.exists {
					t.Errorf("%s: exists = %v, want %v", c.address, exists, c.exists)
				}
				if g, w := normalizeObserved(got), normalizeObserved(c.want); !reflect.DeepEqual(g, w) {
					t.Errorf("%s: read mismatch\n got: %#v\nwant: %#v", c.address, g, w)
				}
			}

			if !present {
				t.Error("no address read of a present resource: add an exported body or an address case")
			}
			if !absent {
				t.Error("no address read of an absent resource or an empty collection: add an address case")
			}
		})
	}
}

// withField returns a copy of a task value with one field zeroed.
func withField(v interface{}, name string) interface{} {
	rv := reflect.ValueOf(v)
	cp := reflect.New(rv.Type())
	cp.Elem().Set(rv)
	field := cp.Elem().FieldByName(name)
	field.Set(reflect.Zero(field.Type()))
	return cp.Elem().Interface()
}

// withDefaults returns a copy of a task value with its default tags applied.
func withDefaults(v interface{}) interface{} {
	rv := reflect.ValueOf(v)
	cp := reflect.New(rv.Type())
	cp.Elem().Set(rv)
	defaults.SetDefaults(cp.Interface())
	return cp.Elem().Interface()
}

// checkObserved compares what p's probe read with want, ignoring the
// write_only and apply_only fields, and checks those fields hold nothing read
// from the server, in the read and in Observed.
func checkObserved(t *testing.T, name, typeKey string, rt reflect.Type, p PlanResult, want interface{}, exists bool) {
	t.Helper()
	if p.Observed == nil || p.read == nil {
		t.Errorf("%s: Observed is %#v and the read %#v; want both set", name, p.Observed, p.read)
		return
	}
	for _, v := range []Task{p.Observed, p.read} {
		if got := reflect.TypeOf(v); got != rt {
			t.Errorf("%s: Observed is %v, want the registered value type %v", name, got, rt)
			return
		}
		if _, problems := checkReadbackFields(typeKey, reflect.ValueOf(v), nil); len(problems) > 0 {
			for _, problem := range problems {
				t.Errorf("%s: %s", name, problem)
			}
		}
	}
	if p.readExists != exists {
		t.Errorf("%s: read exists = %v, want %v", name, p.readExists, exists)
	}
	got, wanted := normalizeObserved(p.read), normalizeObserved(want)
	if !reflect.DeepEqual(got, wanted) {
		t.Errorf("%s: read mismatch\n got: %#v\nwant: %#v", name, got, wanted)
	}
}

// normalizeObserved returns a comparable copy of a task value: defaults
// applied, write_only and apply_only fields zeroed, empty maps and slices made
// nil, and lists whose order does not matter sorted.
func normalizeObserved(v interface{}) interface{} {
	rv := reflect.ValueOf(v)
	for rv.Kind() == reflect.Pointer {
		rv = rv.Elem()
	}
	cp := reflect.New(rv.Type())
	cp.Elem().Set(rv)
	defaults.SetDefaults(cp.Interface())
	normalizeStruct(cp.Elem())
	return cp.Elem().Interface()
}

func normalizeStruct(v reflect.Value) {
	if v.Kind() != reflect.Struct {
		return
	}
	rt := v.Type()
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		fv := v.Field(i)
		if !field.IsExported() {
			continue
		}
		if field.Tag.Get("write_only") == "true" || field.Tag.Get("apply_only") == "true" {
			fv.Set(reflect.Zero(field.Type))
			continue
		}
		switch fv.Kind() {
		case reflect.Map, reflect.Slice:
			if fv.Len() == 0 {
				fv.Set(reflect.Zero(field.Type))
				continue
			}
			if fv.Kind() == reflect.Slice {
				cp := reflect.MakeSlice(field.Type, fv.Len(), fv.Len())
				reflect.Copy(cp, fv)
				for j := 0; j < cp.Len(); j++ {
					normalizeStruct(cp.Index(j))
				}
				if field.Tag.Get("ordered") == "false" {
					sort.Slice(cp.Interface(), func(a, b int) bool {
						return fmt.Sprintf("%#v", cp.Index(a).Interface()) < fmt.Sprintf("%#v", cp.Index(b).Interface())
					})
				}
				fv.Set(cp)
			}
		case reflect.Struct:
			normalizeStruct(fv)
		}
	}
}

// TestObservedFixturesCoverEveryTask keeps TestPlanObservesServerState from
// going stale: a task added without a fixture, an exporting task whose fixture
// skips the export, or a fixture with no absent case - or, for a collection,
// no case reading an empty whole collection - fails here. A task whose
// probe cannot read anything is held to TestUnreadableTaskObservesNothing
// instead.
func TestObservedFixturesCoverEveryTask(t *testing.T) {
	t.Parallel()
	fixtures := observedFixtures()
	for name, task := range allRegisteredTasks() {
		support, _ := TaskProbeSupport(task)
		fixture, ok := fixtures[name]
		if support.Status == ProbeUnsupported {
			if ok {
				t.Errorf("task %q cannot probe its state but has an observed fixture", name)
			}
			continue
		}
		if !ok {
			t.Errorf("task %q has no observed fixture", name)
			continue
		}
		_, isApp := task.(AppExporter)
		_, isGlobal := task.(GlobalExporter)
		if (isApp || isGlobal) && fixture.export == nil {
			t.Errorf("task %q exports but its observed fixture does not plan the exported bodies", name)
		}
		absent := false
		for _, c := range fixture.cases {
			if !c.exists || emptyWholeCollection(c.want) {
				absent = true
			}
		}
		if !absent {
			t.Errorf("task %q has no observed case whose resource is absent or an empty collection", name)
		}
	}
	for name := range fixtures {
		if _, ok := Lookup(name); !ok {
			t.Errorf("observed fixture %q names no registered task", name)
		}
	}
}

// TestUnreadableTaskObservesNothing pins the other half of the contract: a
// task whose probe cannot read its state leaves Observed nil, and ReadTask
// says so rather than reporting the resource absent.
func TestUnreadableTaskObservesNothing(t *testing.T) {
	t.Parallel()
	for name, task := range allRegisteredTasks() {
		support, _ := TaskProbeSupport(task)
		if support.Status != ProbeUnsupported {
			continue
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(nil))
			typed, err := NewTask(name)
			if err != nil {
				t.Fatal(err)
			}
			setIdentityForTest(typed)
			if p := typed.Plan(ctx); p.Observed != nil || p.Exists {
				t.Errorf("Observed = %#v, Exists = %v; want nil, false", p.Observed, p.Exists)
			}
			if _, _, err := ReadTask(ctx, typed); !errors.Is(err, ErrStateUnreadable) {
				t.Errorf("ReadTask error = %v, want ErrStateUnreadable", err)
			}
		})
	}
}

// setIdentityForTest fills every required string field of a task pointer, so
// it passes validation without a fixture of its own.
func setIdentityForTest(t Task) {
	rv := reflect.ValueOf(t).Elem()
	rt := rv.Type()
	for i := 0; i < rt.NumField(); i++ {
		if rt.Field(i).Tag.Get("required") == "true" && rv.Field(i).Kind() == reflect.String {
			rv.Field(i).SetString("web")
		}
	}
}

// TestReadTask covers the sdk entry point over a fixture: a present resource,
// an absent one, a plan error, and a version refusal.
func TestReadTask(t *testing.T) {
	t.Parallel()
	fixture := observedFixtures()["dokku_app"]
	ctx := subprocess.ContextWithRunner(testCtx(), observedRunner(t, fixture))

	got, exists, err := ReadTask(ctx, &AppTask{App: "web", State: StatePresent})
	if err != nil || !exists || !reflect.DeepEqual(got, AppTask{App: "web", State: StatePresent}) {
		t.Errorf("present: got %#v, %v, %v", got, exists, err)
	}
	got, exists, err = ReadTask(ctx, AppTask{App: "gone", State: StatePresent})
	if err != nil || exists || !reflect.DeepEqual(got, AppTask{App: "gone", State: StateAbsent}) {
		t.Errorf("absent: got %#v, %v, %v", got, exists, err)
	}
	if _, _, err := ReadTask(ctx, ConfigTask{App: "web", Config: map[string]string{"KEY": "one"}, State: StateClear}); err == nil {
		t.Error("an invalid task read without error")
	}
	if _, _, err := ReadTask(ctx, nil); err == nil {
		t.Error("a nil task read without error")
	}
}

// TestPlanRefusesBelowTheVersionGate checks every task refuses through its own
// Plan when the gate is installed and the server is older than the task's
// floor, carrying the *VersionError and no Observed state.
func TestPlanRefusesBelowTheVersionGate(t *testing.T) {
	t.Parallel()
	for name := range allRegisteredTasks() {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ran := []string{}
			runner := func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
				ran = append(ran, strings.Join(in.Args, " "))
				if strings.Join(in.Args, " ") == "version" {
					return subprocess.ExecCommandResponse{Stdout: "dokku version 0.1.0"}, nil
				}
				return subprocess.ExecCommandResponse{Stdout: "[]"}, nil
			}
			ctx := ContextWithVersionGate(subprocess.ContextWithRunner(testCtx(), runner))
			task, err := NewTask(name)
			if err != nil {
				t.Fatal(err)
			}
			p := task.Plan(ctx)
			var verr *VersionError
			if !errors.As(p.Error, &verr) {
				t.Fatalf("plan error = %v, want a *VersionError", p.Error)
			}
			if verr.TaskType != name {
				t.Errorf("VersionError.TaskType = %q, want %q", verr.TaskType, name)
			}
			if p.Status != PlanStatusError || p.Observed != nil {
				t.Errorf("Status = %q, Observed = %#v; want an error and no Observed", p.Status, p.Observed)
			}
			for _, cmd := range ran {
				if cmd != "version" && !strings.HasPrefix(cmd, "--quiet plugin:list") {
					t.Errorf("the refused plan still ran %q", cmd)
				}
			}
			if _, _, err := ReadTask(ctx, task); !errors.As(err, &verr) {
				t.Errorf("ReadTask error = %v, want the *VersionError", err)
			}
		})
	}
}

// TestEveryPlanChecksVersionGate asserts the Plan of every registered task
// starts with the gatePlan check, so a task planned outside the CLI honours a
// gate on its context, and that no Plan calls Validate itself, since gatePlan
// does. TestPlanRefusesBelowTheVersionGate proves the check works; this one
// keeps a new task from skipping it.
func TestEveryPlanChecksVersionGate(t *testing.T) {
	t.Parallel()
	registered := map[string]bool{}
	for _, task := range allRegisteredTasks() {
		registered[taskStructOf(task).Name()] = true
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	seen := map[string]bool{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "Plan" || len(fn.Recv.List) != 1 {
				continue
			}
			recv := fn.Recv.List[0].Type
			if star, ok := recv.(*ast.StarExpr); ok {
				recv = star.X
			}
			ident, ok := recv.(*ast.Ident)
			if !ok || !registered[ident.Name] {
				continue
			}
			seen[ident.Name] = true
			if !startsWithGatePlan(fn.Body) {
				t.Errorf("%s.Plan does not start with the gatePlan check", ident.Name)
			}
			if callsValidate(fn.Body) {
				t.Errorf("%s.Plan calls Validate itself; gatePlan already runs it", ident.Name)
			}
		}
	}
	for name := range registered {
		if !seen[name] {
			t.Errorf("no Plan method found for registered task %s", name)
		}
	}
}

// callsValidate reports whether body calls a Validate method. gatePlan runs a
// task's Validate() for every Plan (#628), so a Plan calling it again only
// repeats the check, and a property task that skipped it once let `plan` and
// `apply` accept what `docket validate` refused.
func callsValidate(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		if sel, ok := call.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Validate" {
			found = true
		}
		return !found
	})
	return found
}

// startsWithGatePlan reports whether body opens with
// `if r, ok := gatePlan(ctx, t); !ok { return r }`.
func startsWithGatePlan(body *ast.BlockStmt) bool {
	if body == nil || len(body.List) == 0 {
		return false
	}
	stmt, ok := body.List[0].(*ast.IfStmt)
	if !ok {
		return false
	}
	assign, ok := stmt.Init.(*ast.AssignStmt)
	if !ok || len(assign.Rhs) != 1 {
		return false
	}
	call, ok := assign.Rhs[0].(*ast.CallExpr)
	if !ok {
		return false
	}
	ident, ok := call.Fun.(*ast.Ident)
	return ok && ident.Name == "gatePlan"
}
