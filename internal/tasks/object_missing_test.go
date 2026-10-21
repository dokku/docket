package tasks

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

// missingServerRunner answers every command the way dokku does when the app or
// service it names is missing: a non-zero exit.
func missingServerRunner(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
	res := subprocess.ExecCommandResponse{ExitCode: 1, Stderr: "missing"}
	return res, &subprocess.ExecError{Response: res, Err: errors.New(res.Stderr), Ran: true}
}

// dependencies returns the objects t depends on: the ones it names and does
// not create.
func dependencies(t Task) []TaskObject {
	var out []TaskObject
	for _, object := range TaskObjects(t) {
		if object.Set && !object.Creates {
			out = append(out, object)
		}
	}
	return out
}

// TestReadReportsAMissingObject reads every fixture address - each address
// case and each exported body's identity address - from a server where the
// app or service it names is missing. Whatever a task's own probe makes of
// that - an error, an absent resource, or drift it cannot read - the read is
// an *ObjectMissingError naming one of the objects the task depends on. A task
// that creates its only object reads without one.
func TestReadReportsAMissingObject(t *testing.T) {
	t.Parallel()
	for typeKey, fixture := range observedFixtures() {
		t.Run(typeKey, func(t *testing.T) {
			t.Parallel()
			addresses := []string{}
			for _, c := range fixture.addressCases {
				addresses = append(addresses, c.address)
			}
			if fixture.export != nil {
				ctx := subprocess.ContextWithRunner(testCtx(), observedRunner(t, fixture))
				bodies, err := fixture.export(ctx)
				if err != nil {
					t.Fatalf("export: %v", err)
				}
				for _, body := range bodies {
					addresses = append(addresses, IdentityAddress(typeKey, body.(Task)))
				}
			}

			task, err := NewTask(typeKey)
			if err != nil {
				t.Fatal(err)
			}
			// A property task whose every property is global names no app.
			wantsDependency := false
			for _, object := range TaskObjects(task) {
				if !object.Creates {
					wantsDependency = true
				}
			}
			if docer, ok := task.(PropertyTableDocer); ok {
				if appProps, _ := propertiesByScopeForTest(docer.propertyTable()); len(appProps) == 0 {
					wantsDependency = false
				}
			}

			covered := false
			ctx := subprocess.ContextWithRunner(testCtx(), missingServerRunner)
			for _, address := range addresses {
				_, addressTask, err := taskFromAddress(address)
				if err != nil {
					t.Fatalf("%s: %v", address, err)
				}
				deps := dependencies(addressTask)
				_, _, err = ReadAddress(ctx, address)
				if len(deps) == 0 {
					if errors.Is(err, ErrObjectMissing) {
						t.Errorf("%s: read reports a missing object for a task that depends on none: %v", address, err)
					}
					continue
				}
				covered = true
				var missing *ObjectMissingError
				if !errors.As(err, &missing) {
					t.Errorf("%s: error = %v, want an *ObjectMissingError", address, err)
					continue
				}
				if !errors.Is(err, ErrObjectMissing) {
					t.Errorf("%s: error does not match ErrObjectMissing", address)
				}
				found := false
				for _, object := range deps {
					if reflect.DeepEqual(object, missing.Object) {
						found = true
					}
				}
				if !found {
					t.Errorf("%s: missing object = %#v, want one of %#v", address, missing.Object, deps)
				}
			}
			if wantsDependency && !covered {
				t.Error("no fixture address names the object this task depends on: add an address case")
			}
		})
	}
}

// TestReadTaskReportsAMissingObject checks ReadTask reports a missing app the
// way ReadAddress does, and that the error names the app.
func TestReadTaskReportsAMissingObject(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), missingServerRunner)
	_, exists, err := ReadTask(ctx, ConfigTask{App: "gone", Config: map[string]string{"KEY": "one"}, State: StatePresent})
	var missing *ObjectMissingError
	if !errors.As(err, &missing) || exists {
		t.Fatalf("got exists %v, error %v; want an *ObjectMissingError", exists, err)
	}
	got := missing.Object
	if got.Kind != ObjectApp || !reflect.DeepEqual(got.Keys, []string{"app"}) || !reflect.DeepEqual(got.Values, []string{"gone"}) {
		t.Errorf("Object = %#v, want app gone", got)
	}
	if err.Error() != "app gone does not exist" {
		t.Errorf("error = %q", err.Error())
	}
}

// TestReadKeepsItsErrorWhenTheParentProbeFails checks a parent probe that
// cannot reach the server leaves the read's own error in place.
func TestReadKeepsItsErrorWhenTheParentProbeFails(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		if strings.Join(in.Args, " ") == "--quiet apps:exists web" {
			return subprocess.ExecCommandResponse{}, errors.New("connection reset")
		}
		return missingServerRunner(ctx, in)
	}
	ctx := subprocess.ContextWithRunner(testCtx(), runner)
	_, _, err := ReadAddress(ctx, "dokku_config[app=web]")
	if err == nil || errors.Is(err, ErrObjectMissing) {
		t.Errorf("error = %v, want the read's own error", err)
	}
}

// TestPresentReadSkipsTheParentProbe checks a read that finds its resource
// asks nothing more of the server.
func TestPresentReadSkipsTheParentProbe(t *testing.T) {
	t.Parallel()
	runner := func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		key := strings.Join(in.Args, " ")
		if strings.Contains(key, ":exists") {
			t.Errorf("unexpected parent probe: %s", key)
		}
		if key == "--quiet config:export --format json web" {
			return subprocess.ExecCommandResponse{Stdout: `{"KEY":"one"}`}, nil
		}
		return subprocess.ExecCommandResponse{}, nil
	}
	ctx := subprocess.ContextWithRunner(testCtx(), runner)
	if _, exists, err := ReadAddress(ctx, "dokku_config[app=web]"); err != nil || !exists {
		t.Errorf("got exists %v, error %v; want a present read", exists, err)
	}
}

// TestReadSkipsTheParentProbeOnARefusal checks neither a version refusal nor
// an invalid task is reported as a missing object.
func TestReadSkipsTheParentProbeOnARefusal(t *testing.T) {
	t.Parallel()
	runner := func(ctx context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		key := strings.Join(in.Args, " ")
		if key == "version" {
			return subprocess.ExecCommandResponse{Stdout: "dokku version 0.1.0"}, nil
		}
		if strings.Contains(key, ":exists") {
			t.Errorf("unexpected parent probe: %s", key)
		}
		return missingServerRunner(ctx, in)
	}
	gated := ContextWithVersionGate(subprocess.ContextWithRunner(testCtx(), runner))
	var verr *VersionError
	if _, _, err := ReadTask(gated, ConfigTask{App: "gone", Config: map[string]string{"KEY": "one"}, State: StatePresent}); !errors.As(err, &verr) {
		t.Errorf("ReadTask error = %v, want the *VersionError", err)
	}

	ctx := subprocess.ContextWithRunner(testCtx(), runner)
	_, _, err := ReadTask(ctx, ConfigTask{App: "gone", Config: map[string]string{"KEY": "one"}, State: StateClear})
	if err == nil || errors.Is(err, ErrObjectMissing) {
		t.Errorf("invalid task: error = %v, want its validation error", err)
	}
}
