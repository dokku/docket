package tasks

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

// executeServer is a fake server whose answers an apply can change. A command
// it has no answer for succeeds with no output, which is what the mutating
// commands an apply runs need.
type executeServer struct {
	mu sync.Mutex

	// responses answers a command by its joined arguments.
	responses map[string]string

	// exitCodes makes a command exit non-zero, the way an exists probe
	// reports an absent resource or a failed command reports failure.
	exitCodes map[string]int

	// effects runs after any command whose joined arguments start with the
	// key, changing what later commands answer.
	effects map[string]func(s *executeServer)

	// calls records every command run, by its joined arguments.
	calls []string
}

func (s *executeServer) run(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := strings.Join(in.Args, " ")
	s.calls = append(s.calls, key)
	for prefix, effect := range s.effects {
		if strings.HasPrefix(key, prefix) {
			defer effect(s)
		}
	}
	if code, ok := s.exitCodes[key]; ok {
		res := subprocess.ExecCommandResponse{ExitCode: code, Stderr: "exit " + key}
		return res, &subprocess.ExecError{Response: res, Err: errors.New(res.Stderr), Ran: true}
	}
	return subprocess.ExecCommandResponse{Stdout: s.responses[key]}, nil
}

// count returns how many times key ran.
func (s *executeServer) count(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, call := range s.calls {
		if call == key {
			n++
		}
	}
	return n
}

func (s *executeServer) ctx() context.Context {
	return subprocess.ContextWithRunner(testCtx(), s.run)
}

// TestExecuteObservedReusesThePlansRead checks that an apply whose task
// declares every field a read fills builds Observed from the plan's read,
// with no second probe, through Execute and through ExecutePlan.
func TestExecuteObservedReusesThePlansRead(t *testing.T) {
	t.Parallel()
	t.Run("dokku_app create", func(t *testing.T) {
		t.Parallel()
		server := &executeServer{exitCodes: map[string]int{"--quiet apps:exists web": 1}}
		out := (AppTask{App: "web", State: StatePresent}).Execute(server.ctx())
		if out.Error != nil || !out.Changed {
			t.Fatalf("apply: changed %v, error %v", out.Changed, out.Error)
		}
		if want := (AppTask{App: "web", State: StatePresent}); !reflect.DeepEqual(out.Observed, want) || !out.Exists {
			t.Errorf("Observed = %#v (exists %v), want %#v (exists true)", out.Observed, out.Exists, want)
		}
		want := []string{"--quiet apps:exists web", "--quiet apps:create web"}
		if !equalStrings(server.calls, want) {
			t.Errorf("commands = %v, want %v: the plan's read should be reused", server.calls, want)
		}
	})
	t.Run("dokku_config present through ExecutePlan", func(t *testing.T) {
		t.Parallel()
		server := &executeServer{responses: map[string]string{
			"--quiet config:export --format json web": `{"KEY":"old","OTHER":"kept"}`,
		}}
		ctx := server.ctx()
		task := ConfigTask{App: "web", Config: map[string]string{"KEY": "new"}, State: StatePresent}
		plan := task.Plan(ctx)
		if plan.InSync || plan.Error != nil {
			t.Fatalf("expected drift, got in sync %v, error %v", plan.InSync, plan.Error)
		}
		probes := len(server.calls)
		out := ExecutePlan(ctx, plan)
		if out.Error != nil || !out.Changed {
			t.Fatalf("apply: changed %v, error %v", out.Changed, out.Error)
		}
		want := ConfigTask{App: "web", Config: map[string]string{"KEY": "new"}, State: StatePresent}
		if !reflect.DeepEqual(out.Observed, want) || !out.Exists {
			t.Errorf("Observed = %#v (exists %v), want %#v (exists true)", out.Observed, out.Exists, want)
		}
		applied := server.calls[probes:]
		if len(applied) == 0 {
			t.Fatal("the apply ran no command")
		}
		for _, call := range applied {
			if !strings.Contains(call, "config:set") {
				t.Errorf("the apply ran %q; only the config:set was expected, no second read", call)
			}
		}
	})
}

// TestExecuteObservedReprobesForAFieldLeftToTheServer checks that an apply
// whose task leaves a read field unset plans again, so Observed carries what
// the server picked: the image a service created without one runs.
func TestExecuteObservedReprobesForAFieldLeftToTheServer(t *testing.T) {
	t.Parallel()
	exists := "--quiet postgres:exists my-db"
	info := infoKey("postgres", "my-db")
	create := "--quiet postgres:create my-db"
	server := &executeServer{
		responses: infoFixture("postgres", "my-db", map[string]string{"version": "postgres:17.2", "memory": "512"}),
		exitCodes: map[string]int{exists: 1},
		effects:   map[string]func(s *executeServer){create: func(s *executeServer) { delete(s.exitCodes, exists) }},
	}
	task := ServiceCreateTask{Service: "postgres", Name: "my-db", Password: "s3cret", State: StatePresent}
	if !leavesReadFieldUnset(reflect.ValueOf(task)) {
		t.Fatal("the task should leave a read field unset")
	}
	out := task.Execute(server.ctx())
	if out.Error != nil || !out.Changed {
		t.Fatalf("apply: changed %v, error %v", out.Changed, out.Error)
	}
	// The write_only password stays unread, and the apply_only image_drift
	// carries its default.
	want := ServiceCreateTask{Service: "postgres", Name: "my-db", Image: "postgres", ImageVersion: "17.2", Memory: 512, ImageDrift: imageDriftWarn, State: StatePresent}
	if !reflect.DeepEqual(out.Observed, want) || !out.Exists {
		t.Errorf("Observed = %#v (exists %v), want %#v (exists true)", out.Observed, out.Exists, want)
	}
	if got := server.count(exists); got != 2 {
		t.Errorf("%s ran %d times, want 2: the plan and the re-probe", exists, got)
	}
	if got := server.count(info); got != 1 {
		t.Errorf("%s ran %d times, want 1: only the re-probe finds the service", info, got)
	}
	if last := server.calls[len(server.calls)-1]; last != info {
		t.Errorf("the last command was %q, want the re-probe's %q", last, info)
	}
}

// TestExecuteObservedReprobesAfterADestroy checks that a destroy plans again
// rather than reporting the plan's read, which still holds the destroyed
// service's image.
func TestExecuteObservedReprobesAfterADestroy(t *testing.T) {
	t.Parallel()
	exists := "--quiet redis:exists cache"
	destroy := "--quiet --force redis:destroy cache"
	server := &executeServer{
		responses: infoFixture("redis", "cache", map[string]string{"version": "redis:7.2.5"}),
		exitCodes: map[string]int{},
		effects:   map[string]func(s *executeServer){destroy: func(s *executeServer) { s.exitCodes[exists] = 1 }},
	}
	out := (ServiceCreateTask{Service: "redis", Name: "cache", State: StateAbsent}).Execute(server.ctx())
	if out.Error != nil || !out.Changed {
		t.Fatalf("apply: changed %v, error %v", out.Changed, out.Error)
	}
	want := ServiceCreateTask{Service: "redis", Name: "cache", ImageDrift: imageDriftWarn, State: StateAbsent}
	if !reflect.DeepEqual(out.Observed, want) || out.Exists {
		t.Errorf("Observed = %#v (exists %v), want %#v (exists false)", out.Observed, out.Exists, want)
	}
	if got := server.count(exists); got != 2 {
		t.Errorf("%s ran %d times, want 2: the plan and the re-probe", exists, got)
	}
}

// TestExecuteObservedInSyncCopiesThePlan checks that a plan in sync hands its
// own Observed and Exists to the returned state, probing nothing more.
func TestExecuteObservedInSyncCopiesThePlan(t *testing.T) {
	t.Parallel()
	server := &executeServer{responses: infoFixture("redis", "cache", map[string]string{"version": "redis:7.2.5"})}
	ctx := server.ctx()
	task := ServiceCreateTask{Service: "redis", Name: "cache", State: StatePresent}
	plan := task.Plan(ctx)
	if !plan.InSync || plan.Observed == nil {
		t.Fatalf("expected an in-sync plan that observes, got in sync %v, observed %#v", plan.InSync, plan.Observed)
	}
	probes := len(server.calls)
	out := ExecutePlan(ctx, plan)
	if out.Changed || out.Error != nil {
		t.Fatalf("in sync: changed %v, error %v", out.Changed, out.Error)
	}
	if !reflect.DeepEqual(out.Observed, plan.Observed) || out.Exists != plan.Exists {
		t.Errorf("Observed = %#v (exists %v), want the plan's %#v (exists %v)", out.Observed, out.Exists, plan.Observed, plan.Exists)
	}
	if len(server.calls) != probes {
		t.Errorf("ExecutePlan ran %v on an in-sync plan", server.calls[probes:])
	}
}

// TestExecuteObservedLeavesNothing covers every case that leaves Observed nil.
func TestExecuteObservedLeavesNothing(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		task    Task
		server  *executeServer
		ctx     func(context.Context) context.Context
		wantErr bool
	}{
		{
			name:   "a task whose probe cannot read its state",
			task:   StorageEnsureTask{App: "web", State: StatePresent},
			server: &executeServer{},
		},
		{
			name: "an imperative task",
			task: GitFromImageTask{App: "web", Image: "registry.example.com/org/web:2.0.0", State: StateDeployed},
			server: &executeServer{responses: map[string]string{
				"apps:report web --format json": `{"app-deploy-source":"docker-image","app-deploy-source-metadata":"registry.example.com/org/web:1.2.3"}`,
			}},
		},
		{
			name:    "a failed apply",
			task:    AppTask{App: "web", State: StatePresent},
			server:  &executeServer{exitCodes: map[string]int{"--quiet apps:exists web": 1, "--quiet apps:create web": 1}},
			wantErr: true,
		},
		{
			name:    "a plan that errored",
			task:    ServiceCreateTask{Service: "postgres", Name: "my-db", State: State("bogus")},
			server:  &executeServer{},
			wantErr: true,
		},
		{
			name: "a re-probe that cannot read the state",
			task: ServiceCreateTask{Service: "postgres", Name: "my-db", State: StatePresent},
			server: &executeServer{
				exitCodes: map[string]int{"--quiet postgres:exists my-db": 1, infoKey("postgres", "my-db"): 1},
				effects: map[string]func(s *executeServer){"--quiet postgres:create my-db": func(s *executeServer) {
					delete(s.exitCodes, "--quiet postgres:exists my-db")
				}},
			},
		},
		{
			name: "a context without observed state",
			task: ServiceCreateTask{Service: "postgres", Name: "my-db", State: StatePresent},
			server: &executeServer{
				exitCodes: map[string]int{"--quiet postgres:exists my-db": 1},
			},
			ctx: ContextWithoutObserved,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			ctx := c.server.ctx()
			if c.ctx != nil {
				ctx = c.ctx(ctx)
			}
			out := c.task.Execute(ctx)
			if (out.Error != nil) != c.wantErr {
				t.Fatalf("error = %v, want error %v", out.Error, c.wantErr)
			}
			if out.Observed != nil || out.Exists {
				t.Errorf("Observed = %#v (exists %v), want nil", out.Observed, out.Exists)
			}
		})
	}
}

// TestExecuteWithoutObservedSkipsTheReprobe checks that a context from
// ContextWithoutObserved never plans again.
func TestExecuteWithoutObservedSkipsTheReprobe(t *testing.T) {
	t.Parallel()
	exists := "--quiet postgres:exists my-db"
	server := &executeServer{
		exitCodes: map[string]int{exists: 1},
		effects:   map[string]func(s *executeServer){"--quiet postgres:create my-db": func(s *executeServer) { delete(s.exitCodes, exists) }},
	}
	out := (ServiceCreateTask{Service: "postgres", Name: "my-db", State: StatePresent}).Execute(ContextWithoutObserved(server.ctx()))
	if out.Error != nil {
		t.Fatalf("apply: %v", out.Error)
	}
	if got := server.count(exists); got != 1 {
		t.Errorf("%s ran %d times, want 1", exists, got)
	}
}

// TestLeavesReadFieldUnset checks which fields decide a re-probe: an unset
// field a read fills does, at the top level and in a list element with
// identity keys, while identity keys and write_only and apply_only fields do
// not.
func TestLeavesReadFieldUnset(t *testing.T) {
	t.Parallel()
	type item struct {
		Key  string `identity:"key" yaml:"key"`
		Note string `yaml:"note"`
	}
	type keyless struct {
		Note string `yaml:"note"`
	}
	type task struct {
		App     string    `identity:"key" yaml:"app"`
		Global  bool      `identity:"key" yaml:"global"`
		Secret  string    `write_only:"true" yaml:"secret"`
		Knob    bool      `apply_only:"true" yaml:"knob"`
		Name    string    `yaml:"-"`
		Value   string    `yaml:"value"`
		Items   []item    `yaml:"items"`
		Plain   []keyless `yaml:"plain"`
	}
	full := task{App: "web", Value: "v", Items: []item{{Key: "a", Note: "n"}}, Plain: []keyless{{}}}
	cases := []struct {
		name string
		task task
		want bool
	}{
		{name: "every read field declared", task: full, want: false},
		{name: "an unset value", task: func() task { v := full; v.Value = ""; return v }(), want: true},
		{name: "an empty list", task: func() task { v := full; v.Items = nil; return v }(), want: true},
		{name: "an unset field of a keyed list element", task: func() task { v := full; v.Items = []item{{Key: "a"}}; return v }(), want: true},
	}
	for _, c := range cases {
		if got := leavesReadFieldUnset(reflect.ValueOf(c.task)); got != c.want {
			t.Errorf("%s: leavesReadFieldUnset = %v, want %v", c.name, got, c.want)
		}
	}
}
