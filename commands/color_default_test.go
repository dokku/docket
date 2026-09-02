package commands

import (
	"bytes"
	"os"
	"testing"
)

// color_default_test.go pins the colour default, which became the whole colour
// decision when #502 stopped every renderer from consulting the
// `color.NoColor` package global. While both were in play a colour needed the
// two to agree, so a case this code got wrong was still suppressed by
// fatih/color's own detection. Nothing covers for it now.
//
// The environment half is tested through noColorEnv rather than
// noColorDefault: `go test` gives the process a pipe for stdout, so
// noColorDefault answers "no color" whatever the environment holds and every
// case below would pass against a function that ignored TERM entirely.
//
// These are the only tests in the package that set an environment variable, so
// they stay serial - `t.Setenv` panics in a parallel test, and Go holds every
// parallel test at the barrier until the serial ones have finished, so the
// process-wide write cannot reach them.

func TestNoColorEnvMatchesFatihDetection(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantOff bool
	}{
		{
			name:    "NO_COLOR set turns color off",
			env:     map[string]string{"NO_COLOR": "1"},
			wantOff: true,
		},
		{
			// fatih/color reads NO_COLOR="" as unset; docket reads any
			// spelling as a request for no colour, which is the stricter
			// half of what the two switches used to agree on.
			name:    "NO_COLOR set but empty turns color off",
			env:     map[string]string{"NO_COLOR": ""},
			wantOff: true,
		},
		{
			name:    "TERM=dumb turns color off",
			env:     map[string]string{"TERM": "dumb"},
			wantOff: true,
		},
		{
			name:    "a color-capable TERM leaves color on",
			env:     map[string]string{"TERM": "xterm-256color"},
			wantOff: false,
		},
		{
			name:    "an unset TERM leaves color on",
			env:     map[string]string{},
			wantOff: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// t.Setenv first so the key's original value is restored on
			// the way out; os.Unsetenv then gives a genuine absence,
			// which is the state a bare `NO_COLOR=` cannot express.
			for _, key := range []string{"NO_COLOR", "TERM"} {
				t.Setenv(key, "")
				os.Unsetenv(key)
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			if got := noColorEnv(); got != tt.wantOff {
				t.Errorf("noColorEnv() = %v, want %v", got, tt.wantOff)
			}
		})
	}
}

// TestNoColorDefaultRejectsNonFileWriters is the buffer case every test in the
// package relies on: a writer with no file descriptor is never a terminal, so
// captured output stays free of escapes without anyone forcing it.
func TestNoColorDefaultRejectsNonFileWriters(t *testing.T) {
	os.Unsetenv("NO_COLOR")
	t.Setenv("TERM", "xterm-256color")
	if !noColorDefault(&bytes.Buffer{}) {
		t.Error("a bytes.Buffer is not a terminal and must answer no color")
	}
}
