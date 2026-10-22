package dokkutest_test

import (
	"context"
	"reflect"
	"testing"

	"github.com/dokku/docket/sdk"
	"github.com/dokku/docket/sdk/dokkutest"
)

// lifecycleCase is one covered task type driven through create, refresh and
// destroy against a fresh Server.
type lifecycleCase struct {
	typeKey string
	seed    func(*dokkutest.Server)
	create  sdk.Task
	// read is what ReadTask returns for create once it ran, when that is not
	// create itself: an in-sync read fills what the task leaves unset from
	// the server.
	read sdk.Task
	// created checks the Server after create.
	created func(*testing.T, *dokkutest.Server)
	destroy sdk.Task
	// destroyed checks the Server after destroy.
	destroyed func(*testing.T, *dokkutest.Server)
	// gone is what the resource's address reads after destroy, and whether
	// it exists.
	gone       sdk.Task
	goneExists bool
}

func lifecycleCases() []lifecycleCase {
	pg := func(s *dokkutest.Server) { s.SeedDatastore("postgres", "postgres:17.2") }
	return []lifecycleCase{
		{
			typeKey: "dokku_app",
			create:  sdk.AppTask{App: "web", State: sdk.StatePresent},
			created: func(t *testing.T, s *dokkutest.Server) {
				if !s.HasApp("web") {
					t.Error("app web was not created")
				}
			},
			destroy: sdk.AppTask{App: "web", State: sdk.StateAbsent},
			destroyed: func(t *testing.T, s *dokkutest.Server) {
				if s.HasApp("web") {
					t.Error("app web was not destroyed")
				}
			},
			gone: sdk.AppTask{App: "web", State: sdk.StateAbsent},
		},
		{
			typeKey: "dokku_config",
			seed:    func(s *dokkutest.Server) { s.SeedConfig("web", map[string]string{"OTHER": "kept"}) },
			create:  sdk.ConfigTask{App: "web", Config: map[string]string{"KEY": "value", "TOKEN": "s3cr3t"}, State: sdk.StatePresent},
			created: func(t *testing.T, s *dokkutest.Server) {
				want := map[string]string{"KEY": "value", "TOKEN": "s3cr3t", "OTHER": "kept"}
				if got := s.Config("web"); !reflect.DeepEqual(got, want) {
					t.Errorf("config = %v, want %v", got, want)
				}
			},
			destroy: sdk.ConfigTask{App: "web", State: sdk.StateClear},
			destroyed: func(t *testing.T, s *dokkutest.Server) {
				if got := s.Config("web"); len(got) != 0 {
					t.Errorf("config = %v, want it cleared", got)
				}
			},
			gone:       sdk.ConfigTask{App: "web", State: sdk.StateClear},
			goneExists: true,
		},
		{
			typeKey: "dokku_config",
			seed: func(s *dokkutest.Server) {
				s.SeedConfig("web", map[string]string{"STALE": "x"})
			},
			create: sdk.ConfigTask{App: "web", Config: map[string]string{"KEY": "value"}, State: sdk.StateSet},
			created: func(t *testing.T, s *dokkutest.Server) {
				if got := s.Config("web"); !reflect.DeepEqual(got, map[string]string{"KEY": "value"}) {
					t.Errorf("config = %v, want only KEY", got)
				}
			},
			destroy: sdk.ConfigTask{App: "web", Config: map[string]string{"KEY": ""}, State: sdk.StateAbsent},
			destroyed: func(t *testing.T, s *dokkutest.Server) {
				if got := s.Config("web"); len(got) != 0 {
					t.Errorf("config = %v, want KEY unset", got)
				}
			},
			gone:       sdk.ConfigTask{App: "web", State: sdk.StateClear},
			goneExists: true,
		},
		{
			typeKey: "dokku_domains",
			seed:    func(s *dokkutest.Server) { s.SeedApp("web") },
			create:  sdk.DomainsTask{App: "web", Domains: []string{"web.example.com", "www.example.com"}, State: sdk.StateSet},
			created: func(t *testing.T, s *dokkutest.Server) {
				if got, want := s.Domains("web"), []string{"web.example.com", "www.example.com"}; !reflect.DeepEqual(got, want) {
					t.Errorf("domains = %v, want %v", got, want)
				}
			},
			destroy: sdk.DomainsTask{App: "web", State: sdk.StateClear},
			destroyed: func(t *testing.T, s *dokkutest.Server) {
				if got := s.Domains("web"); len(got) != 0 {
					t.Errorf("domains = %v, want none", got)
				}
			},
			gone:       sdk.DomainsTask{App: "web", State: sdk.StateClear},
			goneExists: true,
		},
		{
			typeKey: "dokku_domains",
			create:  sdk.DomainsTask{Global: true, Domains: []string{"example.com"}, State: sdk.StatePresent},
			created: func(t *testing.T, s *dokkutest.Server) {
				if got := s.GlobalDomains(); !reflect.DeepEqual(got, []string{"example.com"}) {
					t.Errorf("global domains = %v", got)
				}
			},
			destroy: sdk.DomainsTask{Global: true, Domains: []string{"example.com"}, State: sdk.StateAbsent},
			destroyed: func(t *testing.T, s *dokkutest.Server) {
				if got := s.GlobalDomains(); len(got) != 0 {
					t.Errorf("global domains = %v, want none", got)
				}
			},
			gone:       sdk.DomainsTask{Global: true, State: sdk.StateClear},
			goneExists: true,
		},
		{
			typeKey: "dokku_ports",
			seed:    func(s *dokkutest.Server) { s.SeedApp("web") },
			create: sdk.PortsTask{App: "web", PortMappings: []sdk.PortMapping{
				{Scheme: "http", Host: 80, Container: 5000},
				{Scheme: "https", Host: 443, Container: 5000},
			}, State: sdk.StateSet},
			created: func(t *testing.T, s *dokkutest.Server) {
				want := []dokkutest.PortMapping{{Scheme: "http", Host: 80, Container: 5000}, {Scheme: "https", Host: 443, Container: 5000}}
				if got := s.Ports("web"); !reflect.DeepEqual(got, want) {
					t.Errorf("ports = %v, want %v", got, want)
				}
			},
			destroy: sdk.PortsTask{App: "web", State: sdk.StateClear},
			destroyed: func(t *testing.T, s *dokkutest.Server) {
				if got := s.Ports("web"); len(got) != 0 {
					t.Errorf("ports = %v, want none", got)
				}
			},
			gone:       sdk.PortsTask{App: "web", State: sdk.StateClear},
			goneExists: true,
		},
		{
			typeKey: "dokku_buildpacks",
			seed:    func(s *dokkutest.Server) { s.SeedApp("web") },
			create: sdk.BuildpacksTask{App: "web", Buildpacks: []string{
				"https://github.com/heroku/heroku-buildpack-nodejs.git",
				"https://github.com/heroku/heroku-buildpack-ruby.git",
			}, State: sdk.StatePresent},
			created: func(t *testing.T, s *dokkutest.Server) {
				want := []string{"https://github.com/heroku/heroku-buildpack-nodejs.git", "https://github.com/heroku/heroku-buildpack-ruby.git"}
				if got := s.Buildpacks("web"); !reflect.DeepEqual(got, want) {
					t.Errorf("buildpacks = %v, want %v", got, want)
				}
			},
			destroy: sdk.BuildpacksTask{App: "web", State: sdk.StateClear},
			destroyed: func(t *testing.T, s *dokkutest.Server) {
				if got := s.Buildpacks("web"); len(got) != 0 {
					t.Errorf("buildpacks = %v, want none", got)
				}
			},
			gone:       sdk.BuildpacksTask{App: "web", State: sdk.StateClear},
			goneExists: true,
		},
		{
			typeKey: "dokku_ps_scale",
			seed:    func(s *dokkutest.Server) { s.SeedScale("web", map[string]int{"web": 1, "release": 1}) },
			create:  sdk.PsScaleTask{App: "web", Scale: map[string]int{"web": 2, "worker": 1}, State: sdk.StateSet},
			created: func(t *testing.T, s *dokkutest.Server) {
				if got, want := s.Scale("web"), map[string]int{"web": 2, "worker": 1, "release": 0}; !reflect.DeepEqual(got, want) {
					t.Errorf("scale = %v, want %v", got, want)
				}
			},
			destroy: sdk.PsScaleTask{App: "web", State: sdk.StateClear},
			destroyed: func(t *testing.T, s *dokkutest.Server) {
				if got, want := s.Scale("web"), map[string]int{"web": 1, "worker": 0, "release": 0}; !reflect.DeepEqual(got, want) {
					t.Errorf("scale = %v, want %v", got, want)
				}
			},
			gone:       sdk.PsScaleTask{App: "web", Scale: map[string]int{"web": 1}, State: sdk.StateSet},
			goneExists: true,
		},
		{
			typeKey: "dokku_nginx_property",
			seed:    func(s *dokkutest.Server) { s.SeedApp("web") },
			create:  sdk.NginxPropertyTask{App: "web", Property: "client-max-body-size", Value: "50m", State: sdk.StatePresent},
			created: func(t *testing.T, s *dokkutest.Server) {
				if got, ok := s.Property("nginx", "web", "client-max-body-size"); !ok || got != "50m" {
					t.Errorf("client-max-body-size = %q (set %v), want 50m", got, ok)
				}
			},
			destroy: sdk.NginxPropertyTask{App: "web", Property: "client-max-body-size", State: sdk.StateAbsent},
			destroyed: func(t *testing.T, s *dokkutest.Server) {
				if got, ok := s.Property("nginx", "web", "client-max-body-size"); ok {
					t.Errorf("client-max-body-size = %q, want it unset", got)
				}
			},
			gone: sdk.NginxPropertyTask{App: "web", Property: "client-max-body-size", State: sdk.StateAbsent},
		},
		{
			typeKey: "dokku_git_property",
			create:  sdk.GitPropertyTask{Global: true, Property: "deploy-branch", Value: "main", State: sdk.StatePresent},
			created: func(t *testing.T, s *dokkutest.Server) {
				if got, ok := s.GlobalProperty("git", "deploy-branch"); !ok || got != "main" {
					t.Errorf("deploy-branch = %q (set %v), want main", got, ok)
				}
			},
			destroy: sdk.GitPropertyTask{Global: true, Property: "deploy-branch", State: sdk.StateAbsent},
			destroyed: func(t *testing.T, s *dokkutest.Server) {
				if got, ok := s.GlobalProperty("git", "deploy-branch"); ok {
					t.Errorf("deploy-branch = %q, want it unset", got)
				}
			},
			gone: sdk.GitPropertyTask{Global: true, Property: "deploy-branch", State: sdk.StateAbsent},
		},
		{
			typeKey: "dokku_service_create",
			seed:    pg,
			create:  sdk.ServiceCreateTask{Service: "postgres", Name: "db", ImageVersion: "16.4", Memory: 512, State: sdk.StatePresent},
			read:    sdk.ServiceCreateTask{Service: "postgres", Name: "db", Image: "postgres", ImageVersion: "16.4", Memory: 512, ImageDrift: "warn", State: sdk.StatePresent},
			created: func(t *testing.T, s *dokkutest.Server) {
				info := s.ServiceInfo("postgres", "db")
				if info["version"] != "postgres:16.4" || info["memory"] != "512" {
					t.Errorf("info = %v, want postgres:16.4 with 512MB", info)
				}
			},
			destroy: sdk.ServiceCreateTask{Service: "postgres", Name: "db", State: sdk.StateAbsent},
			destroyed: func(t *testing.T, s *dokkutest.Server) {
				if got := s.Services("postgres"); len(got) != 0 {
					t.Errorf("services = %v, want none", got)
				}
			},
			gone: sdk.ServiceCreateTask{Service: "postgres", Name: "db", State: sdk.StateAbsent},
		},
		{
			typeKey: "dokku_service_link",
			seed: func(s *dokkutest.Server) {
				s.SeedApp("web").SeedService("postgres", "db", nil)
			},
			create: sdk.ServiceLinkTask{App: "web", Service: "postgres", Name: "db", State: sdk.StatePresent},
			created: func(t *testing.T, s *dokkutest.Server) {
				if got := s.Links("postgres", "db"); !reflect.DeepEqual(got, []string{"web"}) {
					t.Errorf("links = %v, want web", got)
				}
				if got, want := s.Config("web")["DATABASE_URL"], s.ServiceInfo("postgres", "db")["dsn"]; got != want || got == "" {
					t.Errorf("DATABASE_URL = %q, want the DSN %q", got, want)
				}
			},
			destroy: sdk.ServiceLinkTask{App: "web", Service: "postgres", Name: "db", State: sdk.StateAbsent},
			destroyed: func(t *testing.T, s *dokkutest.Server) {
				if got := s.Links("postgres", "db"); len(got) != 0 {
					t.Errorf("links = %v, want none", got)
				}
				if got := s.Config("web"); len(got) != 0 {
					t.Errorf("config = %v, want the link's key gone", got)
				}
			},
			gone: sdk.ServiceLinkTask{App: "web", Service: "postgres", Name: "db", State: sdk.StateAbsent},
		},
	}
}

// TestLifecycle drives each covered task type through create, refresh and
// destroy against the fake, the way an embedder's resource does: apply, read
// it back from the task and from its address, plan again with nothing to do,
// then destroy it and read it back gone.
func TestLifecycle(t *testing.T) {
	t.Parallel()
	for _, c := range lifecycleCases() {
		t.Run(c.typeKey+"/"+string(taskState(c.create)), func(t *testing.T) {
			t.Parallel()
			server := dokkutest.New(t)
			if c.seed != nil {
				c.seed(server)
			}
			ctx := sdk.ContextWithVersionGate(server.Context(context.Background()))

			apply(t, ctx, c.create)
			c.created(t, server)

			got, exists, err := sdk.ReadTask(ctx, c.create)
			if err != nil || !exists {
				t.Fatalf("ReadTask after create: exists %v, err %v", exists, err)
			}
			want := c.read
			if want == nil {
				want = c.create
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("ReadTask after create = %#v, want %#v", got, want)
			}
			address := sdk.IdentityAddress(c.typeKey, c.create)
			if _, exists, err := sdk.ReadAddress(ctx, address); err != nil || !exists {
				t.Errorf("ReadAddress(%s) after create: exists %v, err %v", address, exists, err)
			}
			if plan := c.create.Plan(ctx); plan.Error != nil || !plan.InSync {
				t.Errorf("a second plan is not in sync: %s %v (error %v)", plan.Reason, plan.Mutations, plan.Error)
			}

			apply(t, ctx, c.destroy)
			c.destroyed(t, server)

			got, exists, err = sdk.ReadAddress(ctx, address)
			if err != nil {
				t.Fatalf("ReadAddress(%s) after destroy: %v", address, err)
			}
			if !reflect.DeepEqual(got, c.gone) || exists != c.goneExists {
				t.Errorf("ReadAddress(%s) after destroy = %#v (exists %v), want %#v (exists %v)", address, got, exists, c.gone, c.goneExists)
			}
			if plan := c.destroy.Plan(ctx); plan.Error != nil || !plan.InSync {
				t.Errorf("a second destroy plan is not in sync: %s %v (error %v)", plan.Reason, plan.Mutations, plan.Error)
			}
			if unknown := server.Unknown(); len(unknown) != 0 {
				t.Errorf("unknown commands: %q", unknown)
			}
		})
	}
}

// apply plans and applies task, failing t when either step errors or the
// apply changes nothing.
func apply(t *testing.T, ctx context.Context, task sdk.Task) {
	t.Helper()
	plan := task.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("plan %#v: %v", task, plan.Error)
	}
	if plan.InSync {
		t.Fatalf("plan %#v is already in sync, so the apply would test nothing", task)
	}
	state := sdk.ExecutePlan(ctx, plan)
	if state.Error != nil || !state.Changed {
		t.Fatalf("apply %#v: changed %v, error %v (%s)", task, state.Changed, state.Error, state.Stderr)
	}
}

// taskState is the State field of a task value.
func taskState(task sdk.Task) sdk.State {
	return sdk.State(reflect.ValueOf(task).FieldByName("State").String())
}
