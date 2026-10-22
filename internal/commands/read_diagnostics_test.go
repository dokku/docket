package commands

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

// TestReadCommandErrorsRenderAsDiagnostics pins that a read error prints the
// way plan and apply print a task's error, through tasks.Diagnostics: behind
// `dokku:` when the server or a command reported it, version hint kept, and
// unprefixed when the address itself was refused before anything ran.
func TestReadCommandErrorsRenderAsDiagnostics(t *testing.T) {
	t.Parallel()
	failing := func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		key := strings.Join(in.Args, " ")
		if key == "--quiet config:export --format json web" {
			res := subprocess.ExecCommandResponse{ExitCode: 1, Stderr: "config export exploded"}
			return res, &subprocess.ExecError{Response: res, Err: errors.New(res.Stderr), Ran: true}
		}
		return absentRunner(nil, "web")(context.Background(), in)
	}
	cases := []struct {
		name    string
		runner  subprocess.ExecRunner
		address string
		code    int
		prefix  string
		want    string
	}{
		{"object missing", absentRunner(nil), "dokku_config[app=gone]", readExitObjectMissing, "dokku: ", "app gone does not exist"},
		{"version", absentRunner(map[string]string{"version": "0.30.0\n"}, "web"), "dokku_app[app=web]", 1, "dokku: ", "pass --ignore-version"},
		{"command failed", failing, "dokku_config[app=web]", 1, "dokku: ", "config export exploded"},
		{"unknown type", absentRunner(nil), "dokku_nope[app=web]", 1, "", `unknown task type "dokku_nope"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, stdout, ui := readWithRunner(tc.runner)
			if code := c.Run([]string{tc.address}); code != tc.code {
				t.Fatalf("exit = %d, want %d; stderr:\n%s", code, tc.code, ui.ErrorWriter.String())
			}
			stderr := strings.TrimSpace(ui.ErrorWriter.String())
			if stdout.Len() != 0 || !strings.Contains(stderr, tc.want) {
				t.Errorf("stderr = %q, stdout = %q, want %q", stderr, stdout.String(), tc.want)
			}
			hasPrefix := strings.HasPrefix(stderr, "dokku: ") || strings.HasPrefix(stderr, "ssh: ")
			if tc.prefix == "" && hasPrefix {
				t.Errorf("stderr = %q, want no ssh: or dokku: prefix on a refused address", stderr)
			}
			if tc.prefix != "" && !strings.HasPrefix(stderr, tc.prefix) {
				t.Errorf("stderr = %q, want it to start with %q", stderr, tc.prefix)
			}
		})
	}
}
