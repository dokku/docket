package commands

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dokku/docket/tasks"

	udiff "github.com/aymanbagabas/go-udiff"
	"github.com/fatih/color"
	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/mattn/go-isatty"
	"github.com/posener/complete"
	flag "github.com/spf13/pflag"
)

// FmtCommand canonicalises tasks.yml files in place. CLI semantics are
// modeled after black / ruff format: --check controls exit-code-on-
// mismatch, --diff controls whether the unified diff is printed, the
// two flags compose. Default (no flags) writes each file in place.
//
// fmt is offline by contract: it never opens a subprocess and never
// contacts the Dokku server.
type FmtCommand struct {
	command.Meta

	// BaseDir is the directory relative paths resolve against - the recipe
	// probed when --tasks is absent, and any output written to a relative
	// path. Populated from main.go; empty means the process working
	// directory, which is what it always was.
	//
	// It exists so a test can point a command at a temp directory instead of
	// chdir'ing the whole process, which no test can do while another runs
	// beside it.
	BaseDir string

	// Stdout is where a streamed recipe, diff or catalog is written. Populated
	// from main.go; nil writes to the process's standard output. These writes
	// bypass Ui on purpose - it is wrapped in a log formatter - so a test that
	// asserts on them needs somewhere of its own to capture.
	Stdout io.Writer

	// Stdin is where a `--tasks -` recipe is read from. Populated from
	// main.go; nil reads the process's standard input. A test hands over its
	// own pipe instead of swapping os.Stdin, which no two tests can do at
	// once.
	Stdin io.Reader

	stdin *stdinRecipeSource

	// useColor is this run's answer to --color, resolved in Run. A value
	// rather than fatih/color's process-wide flag, so one run's setting
	// cannot reach another's output - or, as it once did, anything that is
	// not about colour at all.
	useColor bool

	check bool
	diff  bool
	color string
	// tasksFormatFlag is the raw --tasks-format value, overriding both
	// the per-file extension detection and the stdin content sniff.
	tasksFormatFlag string
}

func (c *FmtCommand) Name() string {
	return "fmt"
}

func (c *FmtCommand) Synopsis() string {
	return "Formats a tasks file canonically (YAML or JSON5)"
}

func (c *FmtCommand) Help() string {
	return command.CommandHelp(c)
}

func (c *FmtCommand) Examples() map[string]string {
	appName := os.Getenv("CLI_APP_NAME")
	return map[string]string{
		"Format ./tasks.yml in place":             fmt.Sprintf("%s %s", appName, c.Name()),
		"Format a JSON5 task file in place":       fmt.Sprintf("%s %s tasks.json", appName, c.Name()),
		"Check whether files are canonical":       fmt.Sprintf("%s %s --check", appName, c.Name()),
		"Print the diff without writing":          fmt.Sprintf("%s %s --diff", appName, c.Name()),
		"CI gate: print diff and fail on bad":     fmt.Sprintf("%s %s --check --diff", appName, c.Name()),
		"Read from stdin, write to stdout":        fmt.Sprintf("cat tasks.yml | %s %s -", appName, c.Name()),
		"Force the format of a flow-style recipe": fmt.Sprintf("cat tasks.yml | %s %s --tasks-format yaml -", appName, c.Name()),
		"Format every yaml under recipes/":        fmt.Sprintf("%s %s 'recipes/*.yml'", appName, c.Name()),
		"Format every JSON5 file under recipes/":  fmt.Sprintf("%s %s 'recipes/*.json5'", appName, c.Name()),
		"Force colorized diff in a pipe":          fmt.Sprintf("%s %s --diff --color always", appName, c.Name()),
	}
}

func (c *FmtCommand) Arguments() []command.Argument {
	return []command.Argument{}
}

func (c *FmtCommand) AutocompleteArgs() complete.Predictor {
	return taskFileAutocomplete()
}

func (c *FmtCommand) ParsedArguments(args []string) (map[string]command.Argument, error) {
	return command.ParseArguments(args, c.Arguments())
}

func (c *FmtCommand) FlagSet() *flag.FlagSet {
	f := c.Meta.FlagSet(c.Name(), command.FlagSetClient)
	f.BoolVar(&c.check, "check", false, "exit non-zero if any file is not canonically formatted; do not write")
	f.BoolVar(&c.diff, "diff", false, "print a unified diff for any file that is not canonically formatted; do not write")
	f.StringVar(&c.color, "color", "auto", "when to colorize diff output: auto, always, never")
	f.StringVar(&c.tasksFormatFlag, "tasks-format", "", "format the recipe as this format (yaml or json5) instead of detecting it from the file extension, or from the first byte when reading stdin. Needed for a flow-style YAML recipe, which starts with [ and would otherwise sniff as JSON5.")
	return f
}

func (c *FmtCommand) AutocompleteFlags() complete.Flags {
	return command.MergeAutocompleteFlags(
		c.Meta.AutocompleteFlags(command.FlagSetClient),
		complete.Flags{
			"--check":        complete.PredictNothing,
			"--diff":         complete.PredictNothing,
			"--color":        complete.PredictSet("auto", "always", "never"),
			"--tasks-format": recipeFormatAutocomplete(),
		},
	)
}

// Run executes fmt against the resolved file list and reports per-file
// outcomes. Exit codes:
//
//	0 - every file is canonical (or was successfully formatted in place)
//	1 - flag parse error, IO error, parse / round-trip failure, or
//	    --check mismatch on at least one file
func (c *FmtCommand) Run(args []string) int {
	flags := c.FlagSet()
	flags.Usage = func() { c.Ui.Output(c.Help()) }
	if err := flags.Parse(args); err != nil {
		c.Ui.Error(err.Error())
		c.Ui.Error(command.CommandErrorText(c))
		return 1
	}

	useColor, err := resolveColorMode(c.color, c.stdout())
	if err != nil {
		c.Ui.Error(err.Error())
		return 1
	}
	c.useColor = useColor

	formatOverride, err := parseRecipeFormatFlag("--tasks-format", c.tasksFormatFlag)
	if err != nil {
		c.Ui.Error(err.Error())
		return 1
	}

	positional := flags.Args()
	if len(positional) == 1 && positional[0] == taskFileStdin {
		return c.runStdin(formatOverride)
	}
	// stdin produces one stream, so it cannot be combined with named
	// files. Without this the "-" falls through to expandPaths, matches
	// no glob, and dies on a confusing `os.ReadFile("-")`. apply, plan,
	// and validate reject the same mix in resolveTaskFileArg.
	for _, arg := range positional {
		if arg == taskFileStdin {
			c.Ui.Error("cannot mix - with other paths; stdin is a single recipe")
			return 1
		}
	}

	paths, ambiguous, err := expandPaths(c.baseDir(), positional)
	if err != nil {
		c.Ui.Error(err.Error())
		return 1
	}
	// Only the no-argument probe can be ambiguous, and it resolves to a
	// single path, so paths[0] is the candidate the warning is about.
	if len(ambiguous) > 0 {
		c.Ui.Warn(ambiguousTaskFileWarning(paths[0], ambiguous))
	}

	exit := 0
	for _, path := range paths {
		if status := c.formatPath(path, formatOverride); status > exit {
			exit = status
		}
	}
	return exit
}

// runStdin reads stdin, formats it, and writes the result to stdout in
// the default mode. With --diff the diff goes to stdout; with --check
// the exit code reflects whether the input was canonical.
//
// Stdin has no filename to drive format detection, so it sniffs the
// first non-trivia byte: a leading [ or { signals JSON5; anything else
// (including the typical `---` document marker or a leading comment-
// only YAML file) goes through the YAML formatter. formatOverride, from
// --tasks-format, wins over the sniff - a flow-style YAML recipe starts
// with [ and would otherwise be reformatted as JSON5.
func (c *FmtCommand) runStdin(formatOverride string) int {
	src, err := c.stdinSource().recipe()
	if err != nil {
		c.Ui.Error(fmt.Sprintf("read stdin: %v", err))
		return 1
	}
	formatted, err := formatTaskFileBytes(src, taskFileFormatFor("", formatOverride, src))
	if err != nil {
		c.Ui.Error(fmt.Sprintf("format error: %v", err))
		return 1
	}

	changed := !bytesEqual(src, formatted)

	if c.diff && changed {
		_, _ = io.WriteString(c.stdout(), renderDiff("<stdin>", string(src), string(formatted), c.useColor))
	}

	if c.check {
		if changed {
			c.Ui.Error("[error]   stdin is not canonically formatted")
			return 1
		}
		return 0
	}

	if c.diff {
		// --diff alone: never write, even on stdin.
		return 0
	}

	if _, err := c.stdout().Write(formatted); err != nil {
		c.Ui.Error(fmt.Sprintf("write stdout: %v", err))
		return 1
	}
	return 0
}

// formatPath formats a single file. Returns 0 on success, 1 on any
// error or --check mismatch. Errors are reported via c.Ui and the
// caller picks the worst-of exit code across all paths.
func (c *FmtCommand) formatPath(path, formatOverride string) int {
	src, err := os.ReadFile(path)
	if err != nil {
		c.Ui.Error(fmt.Sprintf("read %s: %v", path, err))
		return 1
	}

	formatted, err := formatTaskFileBytes(src, taskFileFormatFor(detectTaskFileFormat(path), formatOverride, src))
	if err != nil {
		c.Ui.Error(fmt.Sprintf("%s: %v", path, err))
		return 1
	}

	changed := !bytesEqual(src, formatted)

	if c.diff && changed {
		_, _ = io.WriteString(c.stdout(), renderDiff(path, string(src), string(formatted), c.useColor))
	}

	if c.check {
		if changed {
			c.Ui.Error(fmt.Sprintf("[error]   %s is not canonically formatted", path))
			c.Ui.Error(fmt.Sprintf("          run: %s fmt %s", appName(), path))
			return 1
		}
		return 0
	}

	if c.diff {
		return 0
	}

	if !changed {
		// no-op preservation: leave the file untouched so mtime
		// stays clean for make / file-watchers.
		return 0
	}

	if err := os.WriteFile(path, formatted, 0o644); err != nil {
		c.Ui.Error(fmt.Sprintf("write %s: %v", path, err))
		return 1
	}
	c.Ui.Output(fmt.Sprintf("==> Formatted %s", path))
	return 0
}

// formatTaskFileBytes dispatches to the YAML or JSON5 formatter based
// on format. Centralised so the in-place and stdin paths stay byte-
// identical for the same logical operation.
func formatTaskFileBytes(src []byte, format string) ([]byte, error) {
	if format == taskFileFormatJSON5 {
		return tasks.FormatJSON5(src)
	}
	return tasks.Format(src)
}

// sniffStdinFormat picks a format for stdin input. JSON5 input always
// starts (after optional whitespace and comments) with `[` or `{`;
// YAML recipes start with `-`, `---`, or a key, none of which collide.
// On ambiguity the function defaults to YAML so existing pipelines
// keep their pre-#218 behaviour.
func sniffStdinFormat(src []byte) string {
	for i := 0; i < len(src); i++ {
		c := src[i]
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			continue
		}
		if c == '/' && i+1 < len(src) && (src[i+1] == '/' || src[i+1] == '*') {
			// Skip a leading line/block comment - JSON5 idiom.
			return taskFileFormatJSON5
		}
		if c == '[' || c == '{' {
			return taskFileFormatJSON5
		}
		return taskFileFormatYAML
	}
	return taskFileFormatYAML
}

// expandPaths resolves the positional arguments to a sorted, deduped
// list of file paths. An empty argument list expands to the first
// existing default candidate (tasks.yml -> tasks.yaml -> tasks.json),
// falling back to "tasks.yml" so the downstream read step produces the
// familiar "no such file" error message when nothing is present.
// Each argument is passed through filepath.Glob; literal paths that
// do not match the glob syntax flow through unchanged.
//
// The second return value carries the other default candidates the probe
// passed over, for ambiguousTaskFileWarning. Named arguments select their
// own files, so it is only ever set for the empty-argument case.
func expandPaths(baseDir string, args []string) ([]string, []string, error) {
	if len(args) == 0 {
		chosen, others, err := probeDefaultTaskFile(baseDir)
		if err != nil {
			return nil, nil, err
		}
		// The probe answers in bare candidate names because the ambiguity
		// warning reads better that way; the paths handed on have to be
		// concrete.
		if chosen == "" {
			return []string{inDir(baseDir, defaultTaskFileCandidates[0])}, nil, nil
		}
		return []string{inDir(baseDir, chosen)}, others, nil
	}

	seen := map[string]bool{}
	var paths []string
	for _, arg := range args {
		matches, err := filepath.Glob(inDir(baseDir, arg))
		if err != nil {
			return nil, nil, fmt.Errorf("invalid glob %q: %w", arg, err)
		}
		if len(matches) == 0 {
			// A literal path with no glob metacharacters that does
			// not exist still flows through to the read step so
			// the user gets a clean "no such file" error.
			matches = []string{inDir(baseDir, arg)}
		}
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				paths = append(paths, m)
			}
		}
	}
	sort.Strings(paths)
	return paths, nil, nil
}

// resolveColorMode turns the --color flag value into this run's answer. auto
// respects TTY and NO_COLOR; always forces colours on; never forces them off.
//
// It returns the decision rather than writing fatih/color's process-wide
// NoColor, which is what it used to do. That global is read by more than the
// diff renderer - subprocess used it to decide whether a child could inherit
// our stdin - so one command's --color setting reached places that had nothing
// to do with colour, and no two runs in a process could disagree.
func resolveColorMode(mode string, out io.Writer) (bool, error) {
	switch mode {
	case "auto":
		return !noColorDefault(out), nil
	case "always":
		return true, nil
	case "never":
		return false, nil
	}
	return false, fmt.Errorf("invalid --color value %q (allowed: auto, always, never)", mode)
}

// noColorDefault matches fatih/color's own default detection: colors on
// when stdout is a terminal, NO_COLOR is unset and TERM is not `dumb`.
//
// Matching it in full is load-bearing now that nothing consults the
// `color.NoColor` global any more. While both switches were in play a color
// needed each to agree, so anything fatih/color suppressed stayed suppressed
// whatever this function said; this is the only answer left.
//
// A writer that is not the process's own stdout has no file descriptor to ask,
// and is not a terminal by definition - it is a buffer a test or a caller is
// collecting bytes in - so it answers "no color" without consulting isatty.
func noColorDefault(w io.Writer) bool {
	if noColorEnv() {
		return true
	}
	f, ok := w.(*os.File)
	if !ok {
		return true
	}
	return !isatty.IsTerminal(f.Fd()) && !isatty.IsCygwinTerminal(f.Fd())
}

// renderDiff produces a colorized GNU unified diff between original
// and formatted with file path on both header lines (no a/ b/ prefix,
// matching gofmt / black). The output round-trips through patch -p0
// once colors are stripped.
func renderDiff(path, original, formatted string, useColor bool) string {
	raw, err := udiff.ToUnified(path, path, original, udiff.Strings(original, formatted), udiff.DefaultContextLines)
	if err != nil {
		return fmt.Sprintf("[error]   diff failed for %s: %v\n", path, err)
	}
	if raw == "" {
		return ""
	}
	return colorizeDiff(raw, useColor)
}

// colorizeDiff walks the unified diff output line by line and applies ANSI
// colors when useColor says to. Passing the decision in rather than consulting
// fatih/color's global is what lets two runs in one process disagree about it.
func colorizeDiff(raw string, useColor bool) string {
	if !useColor {
		return raw
	}
	// Built here and forced on rather than shared package values, because a
	// color.Color with no setting of its own falls back to fatih/color's
	// process-wide flag - which is the global this stopped consulting.
	var (
		removeLine = color.New(color.FgRed)
		addLine    = color.New(color.FgGreen)
		hunk       = color.New(color.FgCyan)
		fileHeader = color.New(color.Bold)
	)
	for _, c := range []*color.Color{removeLine, addLine, hunk, fileHeader} {
		c.EnableColor()
	}
	var b strings.Builder
	for _, line := range strings.SplitAfter(raw, "\n") {
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ "):
			b.WriteString(fileHeader.Sprint(line))
		case strings.HasPrefix(line, "@@"):
			b.WriteString(hunk.Sprint(line))
		case strings.HasPrefix(line, "-"):
			b.WriteString(removeLine.Sprint(line))
		case strings.HasPrefix(line, "+"):
			b.WriteString(addLine.Sprint(line))
		default:
			b.WriteString(line)
		}
	}
	return b.String()
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// stdinSource returns this command's memoized standard-input reader, creating
// it on first use. One per command: the recipe is read more than once per
// invocation (FlagSet, Run, and Help on a flag error) but standard input only
// yields its bytes once.
func (c *FmtCommand) stdinSource() *stdinRecipeSource {
	if c.stdin == nil {
		c.stdin = newStdinRecipeSource(c.Stdin)
	}
	return c.stdin
}

// stdout returns the writer this command streams bytes to.
func (c *FmtCommand) stdout() io.Writer { return commandStdout(c.Stdout) }

// baseDir returns the directory this command resolves relative paths against.
func (c *FmtCommand) baseDir() string { return c.BaseDir }

// noColorEnv is the half of noColorDefault that asks the environment rather
// than the writer. It is split out so a test can assert on it without needing
// a terminal: under `go test` stdout is a pipe, so noColorDefault answers "no
// color" whatever the environment says and would pass either way.
func noColorEnv() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return true
	}
	return os.Getenv("TERM") == "dumb"
}
