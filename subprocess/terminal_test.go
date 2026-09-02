package subprocess

import (
	"os"
	"testing"

	"github.com/fatih/color"
	"github.com/mattn/go-isatty"
)

// terminal_test.go pins the decoupling done in #502. Whether a child may
// inherit this process's stdin used to be spelled `!color.NoColor`, which
// borrowed a colouring decision to answer a terminal question: `NO_COLOR=1`
// silently stopped a subprocess inheriting stdin, and any caller flipping the
// process-wide flag - `docket fmt --color never` does - changed how a
// concurrent dispatch behaved.
//
// The test is serial and writes the global deliberately, which is the only way
// to show the two are no longer the same answer - and the reason it is the one
// test in this file without t.Parallel(). Under `go test` stdout is a pipe, so
// a check that merely read the environment would pass either way.

func TestStdoutIsTerminalIgnoresTheColorGlobal(t *testing.T) {
	want := isatty.IsTerminal(os.Stdout.Fd()) || isatty.IsCygwinTerminal(os.Stdout.Fd())

	prev := color.NoColor
	t.Cleanup(func() { color.NoColor = prev })

	for _, noColor := range []bool{true, false} {
		color.NoColor = noColor
		if got := stdoutIsTerminal(); got != want {
			t.Errorf("with color.NoColor = %v, stdoutIsTerminal() = %v, want %v", noColor, got, want)
		}
	}
}
