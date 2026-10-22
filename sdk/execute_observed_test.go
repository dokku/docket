package sdk_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/dokku/docket/sdk"
	"github.com/dokku/docket/sdk/dokkutest"
)

// newServiceServer is a fake server holding no postgres service until
// `postgres:create` runs, after which it reports one running the image dokku
// picks by default, and app web with one config key.
func newServiceServer(t *testing.T) *dokkutest.Server {
	return dokkutest.New(t).SeedDatastore("postgres", "postgres:17.2").SeedConfig("web", map[string]string{"OTHER": "x"})
}

// TestExecuteReturnsTheObservedTask covers #642 through the sdk: a task built
// with NewTask comes back from Execute as the value type, carrying the image
// the server picked when the task leaves it unset, and a task that declares
// everything a read fills is not probed a second time.
func TestExecuteReturnsTheObservedTask(t *testing.T) {
	t.Run("re-probes a field left to the server", func(t *testing.T) {
		server := newServiceServer(t)
		task, _ := sdk.NewTask("dokku_service_create")
		svc := task.(*sdk.ServiceCreateTask)
		svc.Service, svc.Name = "postgres", "db"

		out := svc.Execute(server.Context(context.Background()))
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
		if got := server.Count("--quiet postgres:exists db"); got != 2 {
			t.Errorf("postgres:exists ran %d times, want 2: the plan and the re-probe", got)
		}
	})

	t.Run("reuses the plan's read", func(t *testing.T) {
		server := newServiceServer(t)
		task, _ := sdk.NewTask("dokku_config")
		cfg := task.(*sdk.ConfigTask)
		cfg.App = "web"
		cfg.Config = map[string]string{"KEY": "value"}

		out := cfg.Execute(server.Context(context.Background()))
		if out.Error != nil || !out.Changed {
			t.Fatalf("apply: changed %v, error %v", out.Changed, out.Error)
		}
		want := sdk.ConfigTask{App: "web", Config: map[string]string{"KEY": "value"}, State: sdk.StatePresent}
		if !reflect.DeepEqual(out.Observed, want) || !out.Exists {
			t.Errorf("Observed = %#v (exists %v), want %#v (exists true)", out.Observed, out.Exists, want)
		}
		if got := server.Count("--quiet config:export --format json web"); got != 1 {
			t.Errorf("config:export ran %d times, want 1", got)
		}
	})

	t.Run("skipped on a context without observed state", func(t *testing.T) {
		server := newServiceServer(t)
		task := sdk.ServiceCreateTask{Service: "postgres", Name: "db", State: sdk.StatePresent}
		out := task.Execute(sdk.ContextWithoutObserved(server.Context(context.Background())))
		if out.Error != nil || out.Observed != nil {
			t.Errorf("Observed = %#v, error %v; want nil, nil", out.Observed, out.Error)
		}
		if got := server.Count("--quiet postgres:exists db"); got != 1 {
			t.Errorf("postgres:exists ran %d times, want 1", got)
		}
	})
}
