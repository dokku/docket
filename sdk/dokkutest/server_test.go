package dokkutest_test

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/sdk"
	"github.com/dokku/docket/sdk/dokkutest"
)

// run sends one dokku command to server.
func run(server *dokkutest.Server, args ...string) (sdk.ExecCommandResponse, error) {
	return server.Run(context.Background(), sdk.ExecCommandInput{Command: "dokku", Args: args})
}

// recordingTB is a testing.TB that records the failures it is handed.
type recordingTB struct {
	testing.TB
	errors []string
}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.errors = append(r.errors, format)
}

func TestUnknownCommandFailsLoudly(t *testing.T) {
	t.Parallel()
	tb := &recordingTB{TB: t}
	server := dokkutest.New(tb)

	_, err := run(server, "--quiet", "network:list", "--format", "json")
	if err == nil || !strings.Contains(err.Error(), "dokku --quiet network:list --format json") {
		t.Errorf("error = %v, want one naming the command", err)
	}
	var execErr *sdk.ExecError
	if errors.As(err, &execErr) {
		t.Error("an unknown command must not read as a command that ran, or a probe takes it for absence")
	}
	if len(tb.errors) != 1 {
		t.Errorf("the test was failed %d times, want once", len(tb.errors))
	}
	if got := server.Unknown(); !reflect.DeepEqual(got, []string{"--quiet network:list --format json"}) {
		t.Errorf("Unknown = %q", got)
	}

	if _, err := server.Run(context.Background(), sdk.ExecCommandInput{Command: "git", Args: []string{"status"}}); err == nil {
		t.Error("a command other than dokku should be unknown")
	}
}

func TestMissingObjectsExitLikeDokku(t *testing.T) {
	t.Parallel()
	server := dokkutest.New(t).SeedDatastore("postgres", "postgres:17.2")

	res, err := run(server, "--quiet", "config:export", "--format", "json", "gone")
	var execErr *sdk.ExecError
	if !errors.As(err, &execErr) || !execErr.Ran || res.ExitCode != 20 || !strings.Contains(res.Stderr, "App gone does not exist") {
		t.Errorf("missing app: response %#v, error %v", res, err)
	}
	res, err = run(server, "--quiet", "postgres:exists", "db")
	if !errors.As(err, &execErr) || res.ExitCode != 1 || !strings.Contains(res.Stderr, "service db does not exist") {
		t.Errorf("missing service: response %#v, error %v", res, err)
	}
}

func TestCallsAreRecorded(t *testing.T) {
	t.Parallel()
	server := dokkutest.New(t).SeedApp("web")
	in := sdk.ExecCommandInput{Command: "dokku", Args: []string{"--quiet", "config:import", "--replace", "--format", "json", "web", "-"}, Stdin: strings.NewReader(`{"A":"1"}`)}
	res, err := server.Run(context.Background(), in)
	if err != nil {
		t.Fatal(err)
	}
	if res.Command != "dokku --quiet config:import --replace --format json web -" {
		t.Errorf("Command = %q, want the command line", res.Command)
	}
	calls := server.Calls()
	if len(calls) != 1 || calls[0].Stdin != `{"A":"1"}` || calls[0].Command != "dokku" || calls[0].Unknown {
		t.Errorf("calls = %#v", calls)
	}
	if server.Count("--quiet config:import --replace --format json web -") != 1 {
		t.Error("Count missed the call")
	}
	if got := server.Config("web"); !reflect.DeepEqual(got, map[string]string{"A": "1"}) {
		t.Errorf("config = %v", got)
	}
	server.ResetCalls()
	if len(server.Calls()) != 0 {
		t.Error("ResetCalls left calls behind")
	}
}

func TestConfigImportRefusesAnEmptyPayload(t *testing.T) {
	t.Parallel()
	server := dokkutest.New(t).SeedConfig("web", map[string]string{"A": "1"})
	in := sdk.ExecCommandInput{Command: "dokku", Args: []string{"--quiet", "config:import", "--replace", "--format", "json", "web", "-"}, Stdin: strings.NewReader(`{}`)}
	if res, err := server.Run(context.Background(), in); err == nil || !strings.Contains(res.Stderr, "No config vars to set") {
		t.Errorf("response %#v, error %v; want dokku's refusal", res, err)
	}
	if got := server.Config("web"); !reflect.DeepEqual(got, map[string]string{"A": "1"}) {
		t.Errorf("config = %v, want it untouched", got)
	}
}

func TestHandlersAnswerInPlaceOfTheModel(t *testing.T) {
	t.Parallel()
	server := dokkutest.New(t).SeedApp("web")
	server.Handle("--quiet apps:exists web", dokkutest.Exit(1, "boom"))
	server.HandlePrefix("--quiet network:", dokkutest.Respond("[]"))

	res, err := run(server, "--quiet", "apps:exists", "web")
	var execErr *sdk.ExecError
	if !errors.As(err, &execErr) || !execErr.Ran || res.ExitCode != 1 || res.Stderr != "boom" {
		t.Errorf("Handle: response %#v, error %v", res, err)
	}
	if res, err := run(server, "--quiet", "network:list", "--format", "json"); err != nil || res.Stdout != "[]" {
		t.Errorf("HandlePrefix: response %#v, error %v", res, err)
	}
	if !server.HasApp("web") {
		t.Error("a handler must not touch the state")
	}

	server.Handle("--quiet apps:exists web", dokkutest.Unavailable(errors.New("no dokku")))
	if _, err := run(server, "--quiet", "apps:exists", "web"); err == nil || errors.As(err, &execErr) {
		t.Errorf("Unavailable: error %v, want one that is not an ExecError", err)
	}
}

func TestFallbackAnswersUnknownCommands(t *testing.T) {
	t.Parallel()
	server := dokkutest.New(t).Fallback(dokkutest.Respond(""))
	if _, err := run(server, "--quiet", "network:list"); err != nil {
		t.Errorf("error = %v, want the fallback's answer", err)
	}
	if len(server.Unknown()) != 0 {
		t.Errorf("Unknown = %q, want nothing the fallback answered", server.Unknown())
	}
}

func TestVersionAndPlugins(t *testing.T) {
	t.Parallel()
	server := dokkutest.New(t).SeedVersion("0.38.8").SeedPlugin("letsencrypt", "0.25.0")
	if res, _ := run(server, "version"); res.Stdout != "dokku version 0.38.8" {
		t.Errorf("version = %q", res.Stdout)
	}
	res, _ := run(server, "--quiet", "plugin:list", "--format", "json")
	if !strings.Contains(res.Stdout, `{"name":"letsencrypt","version":"0.25.0"`) || !strings.Contains(res.Stdout, `{"name":"apps","version":"0.38.8"`) {
		t.Errorf("plugin:list = %s", res.Stdout)
	}
	res, _ = run(server, "--quiet", "plugin:list")
	if !strings.Contains(res.Stdout, "letsencrypt") {
		t.Errorf("plugin:list text = %s", res.Stdout)
	}
}

// TestDestroyingAnAppUnlinksIt pins what dokku's apps:destroy does to a
// service linked to the app.
func TestDestroyingAnAppUnlinksIt(t *testing.T) {
	t.Parallel()
	server := dokkutest.New(t).SeedApp("web").SeedService("redis", "cache", nil).SeedLink("redis", "cache", "web")
	if got := server.Config("web")["REDIS_URL"]; got == "" {
		t.Fatal("the link wrote no REDIS_URL")
	}
	if _, err := run(server, "--quiet", "--force", "redis:destroy", "cache"); err == nil {
		t.Error("a linked service was destroyed")
	}
	if _, err := run(server, "--quiet", "--force", "apps:destroy", "web"); err != nil {
		t.Fatal(err)
	}
	if got := server.Links("redis", "cache"); len(got) != 0 {
		t.Errorf("links = %v after the app was destroyed", got)
	}
}
