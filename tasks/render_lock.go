package tasks

import (
	"bytes"
	"sync"

	"github.com/gliderlabs/sigil"
)

// renderMu serializes every sigil render in the process.
//
// sigil.Execute exports each template variable into the process environment
// with os.Setenv, and restores the environment on the way out by taking a
// snapshot up front and replaying it through os.Clearenv (sigil.go:169-175,
// 179-195 in v0.12.1). None of that is safe to run twice at once: the second
// render's Clearenv wipes the variables the first is still rendering with, and
// because each restores the snapshot *it* took, a variable set between the two
// snapshots is dropped from the process for good.
//
// One render at a time is the whole fix. docket renders a recipe a handful of
// times per run and the work is a template expansion over a file already in
// memory, so nothing here is worth contending for. Concurrency was invisible
// while the command tests were serial; #502 made them parallel and this became
// the environment quietly emptying underneath them.
var renderMu sync.Mutex

// RenderTemplate runs sigil over input with the given variables, holding the
// render lock. Every caller in docket goes through it - calling sigil.Execute
// directly reintroduces the hazard described above.
func RenderTemplate(input []byte, vars map[string]interface{}, name string) (bytes.Buffer, error) {
	renderMu.Lock()
	defer renderMu.Unlock()
	return sigil.Execute(input, vars, name)
}
