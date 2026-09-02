package tasks

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"text/template"

	"github.com/gliderlabs/sigil"
	"github.com/gliderlabs/sigil/builtin"
)

// render.go is docket's recipe template renderer. It is a reimplementation of
// sigil.Execute, kept deliberately close to it, that does one thing
// differently: it never writes the process environment.
//
// sigil.Execute exports every string template variable with os.Setenv and
// restores the environment on the way out by replaying a snapshot through
// os.Clearenv (sigil.go:169-175,179-195 in v0.12.1). That makes rendering
// destructive in two ways. Two renders at once interleave a wipe with a
// restore, and because each replays the snapshot it took, variables are
// dropped from the process for good - an unlocked concurrent render emptied
// the entire environment, PATH and HOME included. And even one render at a
// time leaves a window, between its Clearenv and its replay, where any other
// goroutine reading the environment or spawning a child sees nothing.
//
// The exports exist only to feed sigil's POSIX preprocessor, which expands
// `$VAR` out of the environment before the template runs. That is gated on
// sigil.PosixPreprocess, which only sigil's own command sets - docket has
// never enabled it, so for docket the writes were pure collateral damage.
// Dropping them is why this file exists; the trade is that docket no longer
// honours PosixPreprocess, which it never used.
//
// Everything else is sigil's behaviour, on purpose: the same `$var` scan, the
// same `{{ $k := .k }}` prelude, the same escaped-delimiter fixup, the same
// SIGIL_DELIMS override, and sigil's own builtins.

// renderMu serializes renders. The environment is no longer the reason - it is
// sigil's template path stack, which `include` pushes and pops through package
// state (builtin.go:444-445), and which two concurrent renders would interleave
// the same way they used to interleave the environment.
var renderMu sync.Mutex

var (
	leftDelim  = "{{"
	rightDelim = "}}"
)

// templateVarRe matches `$name` references in a template, which are declared
// as nil when the caller did not supply them so `{{ $x | default "y" }}`
// works for an absent x. Sigil's spelling, kept.
var templateVarRe = regexp.MustCompile(`\$([a-zA-Z_][a-zA-Z0-9_]*)`)

func init() {
	// SIGIL_DELIMS is sigil's delimiter override, read once at startup rather
	// than per render. Sigil indexes the split unguarded and panics on a value
	// with no comma; a malformed value is ignored here instead.
	if delims := os.Getenv("SIGIL_DELIMS"); delims != "" {
		if d := strings.Split(delims, ","); len(d) == 2 {
			leftDelim, rightDelim = d[0], d[1]
		}
	}
}

// renderFuncs is the template function map docket renders with: sigil's
// builtins, with `include` and `render` swapped for versions that recurse into
// this renderer, plus docket's own filters.
//
// Owning the map also settles which functions a render has. Sigil keeps one
// package-global map that `sigil/builtin` fills from an init, so whether a
// builtin existed depended on whether something in the binary happened to
// import that package - `main.go` does, and so do a handful of test files in
// this package, but the commands test binary does not. A recipe using
// `{{ .app | upper }}` therefore worked in the CLI and failed to parse in
// those tests. There is one map now and it is the same everywhere.
//
// The map is filled in an init rather than a literal because `include` and
// `render` recurse back into the renderer, which reads the map - a cycle the
// compiler rejects in a composite literal.
var renderFuncs = template.FuncMap{
	// templating
	"default": builtin.Default,
	"var":     builtin.Var,
	// strings
	"capitalize": builtin.Capitalize,
	"lower":      builtin.Lower,
	"upper":      builtin.Upper,
	"replace":    builtin.Replace,
	"trim":       builtin.Trim,
	"indent":     builtin.Indent,
	"match":      builtin.Match,
	"stdin":      builtin.Stdin,
	"substr":     builtin.Substring,
	"base64enc":  builtin.Base64Encode,
	"base64dec":  builtin.Base64Decode,
	// filesystem
	"file":   builtin.File,
	"exists": builtin.Exists,
	"dir":    builtin.Dir,
	"dirs":   builtin.Dirs,
	"files":  builtin.Files,
	"text":   builtin.Text,
	// external
	"sh":      builtin.Shell,
	"httpget": builtin.HttpGet,
	// structured data
	"pointer":  builtin.Pointer,
	"json":     builtin.Json,
	"jmespath": builtin.JmesPath,
	"tojson":   builtin.ToJson,
	"yaml":     builtin.Yaml,
	"toyaml":   builtin.ToYaml,
	"uniq":     builtin.Uniq,
	"drop":     builtin.Drop,
	"append":   builtin.Append,
	"seq":      builtin.Seq,
	"join":     builtin.Join,
	"joinkv":   builtin.JoinKv,
	"split":    builtin.Split,
	"splitkv":  builtin.SplitKv,
	// docket's own
	"dq": DoubleQuoteEscape,
}

func init() {
	renderFuncs["include"] = includeTemplate
	renderFuncs["render"] = renderNested
}

// RenderTemplate renders input with vars, under the render lock. Every render
// in docket goes through it.
func RenderTemplate(input []byte, vars map[string]interface{}, name string) (bytes.Buffer, error) {
	renderMu.Lock()
	defer renderMu.Unlock()
	return renderTemplate(input, vars, name)
}

// renderTemplate is the unlocked form, so `include` and `render` can recurse
// from inside a render that already holds the lock.
func renderTemplate(input []byte, vars map[string]interface{}, name string) (bytes.Buffer, error) {
	var tmplVars string
	for _, match := range templateVarRe.FindAllSubmatch(input, -1) {
		varName := string(match[1])
		if _, exists := vars[varName]; !exists {
			vars[varName] = nil
		}
	}
	for k := range vars {
		tmplVars += fmt.Sprintf("%s $%s := .%s %s", leftDelim, k, k, rightDelim)
	}

	inputStr := strings.ReplaceAll(
		string(input),
		fmt.Sprintf("\\%s\n%s", rightDelim, leftDelim),
		fmt.Sprintf("%s%s", rightDelim, leftDelim),
	)

	tmpl, err := template.New(name).Funcs(renderFuncs).Delims(leftDelim, rightDelim).Parse(tmplVars + inputStr)
	if err != nil {
		return bytes.Buffer{}, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return bytes.Buffer{}, err
	}
	return buf, nil
}

// varsFromArgs collects the trailing `key=value` strings and maps a template
// passes to `include` or `render` into one variable map. Sigil's rules.
func varsFromArgs(args []interface{}) map[string]interface{} {
	vars := make(map[string]interface{})
	for _, arg := range args {
		if mv, ok := arg.(map[string]interface{}); ok {
			for k, v := range mv {
				vars[k] = v
			}
			continue
		}
		sv, ok := arg.(string)
		if !ok {
			continue
		}
		if parts := strings.SplitN(sv, "=", 2); len(parts) == 2 {
			vars[parts[0]] = parts[1]
		}
	}
	return vars
}

// renderNested is the `render` filter. It exists so a nested render goes
// through this renderer rather than back into sigil.Execute, which would
// reintroduce the environment writes at one remove.
func renderNested(args ...interface{}) (interface{}, error) {
	if len(args) == 0 {
		return "", fmt.Errorf("render cannot be used without arguments")
	}
	input, ok := args[len(args)-1].(string)
	if !ok {
		return "", fmt.Errorf("render must be given a string to render")
	}
	out, err := renderTemplate([]byte(input), varsFromArgs(args[:len(args)-1]), "<render>")
	return out.String(), err
}

// includeTemplate is the `include` filter, for the same reason as
// renderNested. The path stack is still sigil's, so a relative include inside
// an included file resolves the way it always has.
func includeTemplate(filename string, args ...interface{}) (interface{}, error) {
	path, err := sigil.LookPath(filename)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sigil.PushPath(filepath.Dir(path))
	defer sigil.PopPath()
	out, err := renderTemplate(data, varsFromArgs(args), filepath.Base(path))
	return out.String(), err
}
