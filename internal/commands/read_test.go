package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/mitchellh/cli"
)

// readWithRunner returns a read command whose dokku is runner, and the
// buffers its stdout and Ui write to.
func readWithRunner(runner subprocess.ExecRunner) (*ReadCommand, *bytes.Buffer, *cli.MockUi) {
	ui := cli.NewMockUi()
	var stdout bytes.Buffer
	c := &ReadCommand{Meta: command.Meta{Ui: ui}, Stdout: &stdout}
	c.Ctx = subprocess.ContextWithRunner(context.Background(), runner)
	return c, &stdout, ui
}

// absentRunner answers responses, and fails `apps:exists` for any app not in
// present the way dokku reports a missing app: a non-zero exit.
func absentRunner(responses map[string]string, present ...string) subprocess.ExecRunner {
	return func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		key := strings.Join(in.Args, " ")
		if strings.HasPrefix(key, "--quiet apps:exists ") {
			for _, app := range present {
				if key == "--quiet apps:exists "+app {
					return subprocess.ExecCommandResponse{}, nil
				}
			}
			res := subprocess.ExecCommandResponse{ExitCode: 1, Stderr: "App does not exist"}
			return res, &subprocess.ExecError{Response: res, Err: errors.New(res.Stderr), Ran: true}
		}
		return fixtureResponse(responses, key), nil
	}
}

func TestReadCommandMetadata(t *testing.T) {
	t.Parallel()
	c := &ReadCommand{}
	if c.Name() != "read" {
		t.Errorf("Name = %q, want read", c.Name())
	}
	if c.Synopsis() == "" {
		t.Error("Synopsis must not be empty")
	}
}

// TestReadCommandPrintsARecipe pins the default output: a one-task recipe in
// the default format, carrying every member the resource has.
func TestReadCommandPrintsARecipe(t *testing.T) {
	t.Parallel()
	c, stdout, ui := readWithRunner(fakeExecRunner(map[string]string{
		"--quiet buildpacks:list web": "=====> web buildpack urls\nhttps://github.com/heroku/heroku-buildpack-nodejs.git\nhttps://github.com/heroku/heroku-buildpack-ruby.git",
	}))
	if code := c.Run([]string{"dokku_buildpacks[app=web]"}); code != 0 {
		t.Fatalf("exit = %d; stderr:\n%s", code, ui.ErrorWriter.String())
	}
	out := stdout.String()
	for _, want := range []string{"dokku_buildpacks:", "app: web", "heroku-buildpack-nodejs.git", "heroku-buildpack-ruby.git"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// TestReadCommandFormats pins that --format picks the codec the recipe is
// printed in.
func TestReadCommandFormats(t *testing.T) {
	t.Parallel()
	c, stdout, ui := readWithRunner(absentRunner(nil, "web"))
	if code := c.Run([]string{"--format", "hcl", "dokku_app[app=web]"}); code != 0 {
		t.Fatalf("exit = %d; stderr:\n%s", code, ui.ErrorWriter.String())
	}
	if out := stdout.String(); !strings.Contains(out, `dokku_app`) || !strings.Contains(out, `"web"`) || strings.Contains(out, "app: web") {
		t.Errorf("output is not HCL:\n%s", out)
	}
}

// TestReadCommandJSONMasksSecrets pins --json's shape and that a sensitive
// value read off the server is masked, while the address's own key - the
// user's argument - prints as given even when a secret shares its spelling.
func TestReadCommandJSONMasksSecrets(t *testing.T) {
	t.Parallel()
	c, stdout, ui := readWithRunner(fakeExecRunner(map[string]string{
		"--quiet config:export --format json web": `{"API_KEY":"abc123","ALIAS":"web"}`,
	}))
	if code := c.Run([]string{"--json", "dokku_config[app=web]"}); code != 0 {
		t.Fatalf("exit = %d; stderr:\n%s", code, ui.ErrorWriter.String())
	}
	var got readJSON
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, stdout.String())
	}
	if got.Address != "dokku_config[app=web]" || got.Type != "dokku_config" || !got.Exists {
		t.Errorf("envelope = %+v", got)
	}
	if got.Task["app"] != "web" {
		t.Errorf("task app = %v, want the address key unmasked", got.Task["app"])
	}
	config, _ := got.Task["config"].(map[string]interface{})
	if config["API_KEY"] != "***" || config["ALIAS"] != "***" {
		t.Errorf("config = %v, want every value masked", config)
	}
	if strings.Contains(stdout.String(), "abc123") {
		t.Errorf("a secret reached stdout:\n%s", stdout.String())
	}
}

// TestReadCommandShowSensitive pins that --show-sensitive prints the values.
func TestReadCommandShowSensitive(t *testing.T) {
	t.Parallel()
	c, stdout, ui := readWithRunner(fakeExecRunner(map[string]string{
		"--quiet config:export --format json web": `{"API_KEY":"abc123"}`,
	}))
	if code := c.Run([]string{"--show-sensitive", "dokku_config[app=web]"}); code != 0 {
		t.Fatalf("exit = %d; stderr:\n%s", code, ui.ErrorWriter.String())
	}
	if !strings.Contains(stdout.String(), "abc123") {
		t.Errorf("--show-sensitive should print the value:\n%s", stdout.String())
	}
}

// TestReadCommandAbsentExitsTwo pins that a resource that is not there still
// prints, in the state that says so, and exits 2.
func TestReadCommandAbsentExitsTwo(t *testing.T) {
	t.Parallel()
	c, stdout, ui := readWithRunner(absentRunner(nil))
	if code := c.Run([]string{"--json", "dokku_app[app=gone]"}); code != readExitAbsent {
		t.Fatalf("exit = %d, want %d; stderr:\n%s", code, readExitAbsent, ui.ErrorWriter.String())
	}
	var got readJSON
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, stdout.String())
	}
	if got.Exists || got.Task["state"] != "absent" {
		t.Errorf("read = %+v, want an absent app", got)
	}
}

// TestReadCommandVersionGate pins the refusal and its hint, and that
// --ignore-version reads anyway.
func TestReadCommandVersionGate(t *testing.T) {
	t.Parallel()
	old := map[string]string{"version": "0.30.0\n"}
	c, stdout, ui := readWithRunner(absentRunner(old, "web"))
	if code := c.Run([]string{"dokku_app[app=web]"}); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(ui.ErrorWriter.String(), "pass --ignore-version") || stdout.Len() != 0 {
		t.Errorf("stderr = %q, stdout = %q", ui.ErrorWriter.String(), stdout.String())
	}

	c, _, ui = readWithRunner(absentRunner(old, "web"))
	if code := c.Run([]string{"--ignore-version", "dokku_app[app=web]"}); code != 0 {
		t.Fatalf("--ignore-version exit = %d; stderr:\n%s", code, ui.ErrorWriter.String())
	}
}

// TestReadCommandRefusesBadInput pins the usage errors, each raised before
// the server is asked anything.
func TestReadCommandRefusesBadInput(t *testing.T) {
	t.Parallel()
	cases := []struct {
		args []string
		want string
	}{
		{nil, "exactly one resource address, got 0"},
		{[]string{"dokku_app[app=a]", "dokku_app[app=b]"}, "exactly one resource address, got 2"},
		{[]string{"--json", "--format", "hcl", "dokku_app[app=a]"}, "--json and --format cannot be combined"},
		{[]string{"--format", "toml", "dokku_app[app=a]"}, `invalid --format "toml"`},
		{[]string{"dokku_app"}, "names no keys"},
		{[]string{"dokku_storage_ensure[app=a]"}, "cannot read its state"},
	}
	for _, tc := range cases {
		c, stdout, ui := readWithRunner(func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
			t.Errorf("%v: unexpected command %v", tc.args, in.Args)
			return subprocess.ExecCommandResponse{}, errors.New("unexpected")
		})
		if code := c.Run(tc.args); code != 1 {
			t.Errorf("%v: exit = %d, want 1", tc.args, code)
		}
		if !strings.Contains(ui.ErrorWriter.String(), tc.want) {
			t.Errorf("%v: stderr = %q, want it to mention %q", tc.args, ui.ErrorWriter.String(), tc.want)
		}
		if stdout.Len() != 0 {
			t.Errorf("%v: stdout = %q, want nothing", tc.args, stdout.String())
		}
	}
}
