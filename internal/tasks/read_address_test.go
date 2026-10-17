package tasks

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

// noServer is a runner that fails the test on any command, for reads that
// must be refused before the server is contacted.
func noServer(t *testing.T) context.Context {
	t.Helper()
	return subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		t.Errorf("unexpected command: %s %s", in.Command, strings.Join(in.Args, " "))
		return subprocess.ExecCommandResponse{}, errors.New("unexpected command")
	})
}

// TestReadAddressRefusesABadAddress pins that an address naming no single
// resource is refused before anything is read.
func TestReadAddressRefusesABadAddress(t *testing.T) {
	t.Parallel()
	cases := []struct {
		address string
		want    string
	}{
		{"dokku_app[app=a", `missing closing "]"`},
		{"dokku_nope[app=a]", `unknown task type "dokku_nope"`},
		{"dokku_app", "names no keys"},
		{"dokku_app[app=a,global=true]", `"global" is not an identity key of dokku_app`},
		{"dokku_logs_property[app=web]", `missing required key "property"`},
		{"dokku_domains[global=yes]", `"yes" is not true or false`},
	}
	for _, tc := range cases {
		_, _, err := ReadAddress(noServer(t), tc.address)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error = %v, want it to mention %q", tc.address, err, tc.want)
		}
	}
}

// TestReadAddressUnreadableTask pins that a task whose probe reads nothing
// says so, without reporting the resource absent and without asking.
func TestReadAddressUnreadableTask(t *testing.T) {
	t.Parallel()
	got, exists, err := ReadAddress(noServer(t), "dokku_storage_ensure[app=web]")
	if !errors.Is(err, ErrStateUnreadable) {
		t.Fatalf("error = %v, want ErrStateUnreadable", err)
	}
	if got != nil || exists {
		t.Errorf("got %#v, exists %v; want nothing", got, exists)
	}
}

// TestReadAddressHonoursTheGate pins that the gate refuses an address read
// with a *VersionError on both paths - a task planned in its owning state, and
// one with its own addressReader, which does not go through Plan - and that
// nothing beyond the version is read.
func TestReadAddressHonoursTheGate(t *testing.T) {
	t.Parallel()
	for _, address := range []string{"dokku_app[app=web]", "dokku_app_clone[app=web]"} {
		var calls []string
		ctx := gatedRunner(t, func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
			calls = append(calls, strings.Join(in.Args, " "))
			return subprocess.ExecCommandResponse{Stdout: "0.30.0\n"}, nil
		})
		_, _, err := ReadAddress(ctx, address)
		var verr *VersionError
		if !errors.As(err, &verr) {
			t.Errorf("%s: error = %v, want a *VersionError", address, err)
		}
		if len(calls) != 1 || calls[0] != "version" {
			t.Errorf("%s: commands = %q, want only the version read", address, calls)
		}
	}
}

// TestReadAddressKeepsTheTasksValidation pins that a task read in its owning
// state still runs its own validation, so an address pairing keys the task
// treats as mutually exclusive is refused.
func TestReadAddressKeepsTheTasksValidation(t *testing.T) {
	t.Parallel()
	_, _, err := ReadAddress(noServer(t), "dokku_domains[app=web,global=true]")
	if err == nil {
		t.Fatal("expected app and global together to be refused")
	}
}

// TestSetReadState pins the owning state the generic path picks: clear where
// a task offers it, absent where it does not, and the default otherwise.
func TestSetReadState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		typeKey string
		want    State
	}{
		{"dokku_domains", StateClear},
		{"dokku_buildpacks", StateAbsent},
		{"dokku_logs_property", StateAbsent},
		{"dokku_git_sync", StatePresent},
	}
	for _, tc := range cases {
		task, err := NewTask(tc.typeKey)
		if err != nil {
			t.Fatal(err)
		}
		setReadState(task)
		if got := State(reflectStateOf(task)); got != tc.want {
			t.Errorf("%s: state = %q, want %q", tc.typeKey, got, tc.want)
		}
	}
}

// reflectStateOf reads a task's State field.
func reflectStateOf(task Task) string {
	v, _ := structValue(task)
	return v.FieldByName("State").String()
}
