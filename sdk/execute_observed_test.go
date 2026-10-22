package sdk_test

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dokku/docket/sdk"
)

// serviceServer is a fake server holding no postgres service until
// `postgres:create` runs, after which it reports one running the image dokku
// picked. It records every command.
type serviceServer struct {
	mu      sync.Mutex
	created bool
	calls   []string
}

func (s *serviceServer) context() context.Context {
	return sdk.ContextWithRunner(context.Background(), func(_ context.Context, in sdk.ExecCommandInput) (sdk.ExecCommandResponse, error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		key := strings.Join(in.Args, " ")
		s.calls = append(s.calls, key)
		switch {
		case key == "--quiet postgres:exists db" && !s.created:
			return sdk.ExecCommandResponse{ExitCode: 1}, nil
		case strings.HasPrefix(key, "--quiet postgres:create db"):
			s.created = true
		case key == "--quiet postgres:info db --format json":
			return sdk.ExecCommandResponse{Stdout: `{"version":"postgres:17.2"}`}, nil
		case key == "--quiet config:export --format json web":
			return sdk.ExecCommandResponse{Stdout: `{"OTHER":"x"}`}, nil
		}
		return sdk.ExecCommandResponse{}, nil
	})
}

func (s *serviceServer) count(key string) int {
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

// TestExecuteReturnsTheObservedTask covers #642 through the sdk: a task built
// with NewTask comes back from Execute as the value type, carrying the image
// the server picked when the task leaves it unset, and a task that declares
// everything a read fills is not probed a second time.
func TestExecuteReturnsTheObservedTask(t *testing.T) {
	t.Run("re-probes a field left to the server", func(t *testing.T) {
		server := &serviceServer{}
		task, _ := sdk.NewTask("dokku_service_create")
		svc := task.(*sdk.ServiceCreateTask)
		svc.Service, svc.Name = "postgres", "db"

		out := svc.Execute(server.context())
		if out.Error != nil || !out.Changed {
			t.Fatalf("apply: changed %v, error %v", out.Changed, out.Error)
		}
		observed, ok := out.Observed.(sdk.ServiceCreateTask)
		if !ok {
			t.Fatalf("Observed is %T, want sdk.ServiceCreateTask", out.Observed)
		}
		if observed.Image != "postgres" || observed.ImageVersion != "17.2" || !out.Exists {
			t.Errorf("Observed image %s:%s (exists %v), want postgres:17.2 (exists true)", observed.Image, observed.ImageVersion, out.Exists)
		}
		if got := server.count("--quiet postgres:exists db"); got != 2 {
			t.Errorf("postgres:exists ran %d times, want 2: the plan and the re-probe", got)
		}
	})

	t.Run("reuses the plan's read", func(t *testing.T) {
		server := &serviceServer{}
		task, _ := sdk.NewTask("dokku_config")
		cfg := task.(*sdk.ConfigTask)
		cfg.App = "web"
		cfg.Config = map[string]string{"KEY": "value"}

		out := cfg.Execute(server.context())
		if out.Error != nil || !out.Changed {
			t.Fatalf("apply: changed %v, error %v", out.Changed, out.Error)
		}
		want := sdk.ConfigTask{App: "web", Config: map[string]string{"KEY": "value"}, State: sdk.StatePresent}
		if !reflect.DeepEqual(out.Observed, want) || !out.Exists {
			t.Errorf("Observed = %#v (exists %v), want %#v (exists true)", out.Observed, out.Exists, want)
		}
		if got := server.count("--quiet config:export --format json web"); got != 1 {
			t.Errorf("config:export ran %d times, want 1", got)
		}
	})

	t.Run("skipped on a context without observed state", func(t *testing.T) {
		server := &serviceServer{}
		task := sdk.ServiceCreateTask{Service: "postgres", Name: "db", State: sdk.StatePresent}
		out := task.Execute(sdk.ContextWithoutObserved(server.context()))
		if out.Error != nil || out.Observed != nil {
			t.Errorf("Observed = %#v, error %v; want nil, nil", out.Observed, out.Error)
		}
		if got := server.count("--quiet postgres:exists db"); got != 1 {
			t.Errorf("postgres:exists ran %d times, want 1", got)
		}
	})
}
