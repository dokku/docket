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

	// formatFlag is the raw --format value: the format to WRITE. When it
	// names a format the recipe was not read as, fmt converts rather than
	// reformats. It is the same flag init and export take, and the output
	// counterpart to --tasks-format.
	formatFlag string

	// output redirects the write to a named path instead of the file that
	// was read; "-" streams to stdout. Empty means format in place.
	output string

	// force permits an --output that would overwrite an existing file.
	// Formatting in place needs no such permission - clobbering the file it
	// just read is what fmt has always done - so this guards only the new
	// ability to write over a file the command did not read.
	force bool
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
		"Convert a recipe to JSON5 on stdout":     fmt.Sprintf("cat tasks.yml | %s %s --format json5 -", appName, c.Name()),
		"Convert a recipe to a new file":          fmt.Sprintf("%s %s --format json5 --output tasks.json5 tasks.yml", appName, c.Name()),
		"Convert a recipe in place":               fmt.Sprintf("%s %s --format json5 tasks.json", appName, c.Name()),
		"Convert a flow-style YAML recipe":        fmt.Sprintf("cat tasks.yml | %s %s --tasks-format yaml --format json5 -", appName, c.Name()),
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
	f.StringVar(&c.tasksFormatFlag, "tasks-format", "", "read the recipe as this format ("+recipeFormatList()+") instead of detecting it from the file extension, or from the first byte when reading stdin. Needed for a flow-style YAML recipe, which starts with [ and would otherwise sniff as JSON5.")
	f.StringVar(&c.formatFlag, "format", "", "write the recipe as this format ("+recipeFormatList()+"), converting it when that is not the format it was read as. Comments are preserved; YAML anchors and merge keys are inlined, since no other format has them.")
	f.StringVar(&c.output, "output", "", "write to this path instead of formatting in place; pass - to write to stdout. Takes a single recipe.")
	f.BoolVar(&c.force, "force", false, "overwrite an existing --output file")
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
			"--format":       recipeFormatAutocomplete(),
			"--output":       taskFileAutocomplete(),
			"--force":        complete.PredictNothing,
		},
	)
}

// Run executes fmt against the resolved file list and reports per-file
// outcomes. Exit codes:
//
//	0 - every file is canonical (or was successfully formatted, converted,
//	    or written to --output)
//	1 - flag parse error, a rejected flag combination, an --output that
//	    would overwrite an existing file without --force, IO error, parse /
//	    round-trip failure, a --check that would have to convert, or a
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
	outputOverride, err := parseRecipeFormatFlag("--format", c.formatFlag)
	if err != nil {
		c.Ui.Error(err.Error())
		return 1
	}

	if err := c.checkFlagCombination(flags); err != nil {
		c.Ui.Error(err.Error())
		return 1
	}

	positional := flags.Args()
	if len(positional) == 1 && positional[0] == taskFileStdin {
		if err := c.checkOutputTarget(""); err != nil {
			c.Ui.Error(err.Error())
			return 1
		}
		return c.runStdin(formatOverride, outputOverride)
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

	if c.output != "" {
		// --output names one destination, so it can only describe one
		// source. Silently writing each file over the last would be worse
		// than saying so.
		if len(paths) != 1 {
			c.Ui.Error(fmt.Sprintf("--output takes a single recipe, but %d paths were given", len(paths)))
			return 1
		}
		if err := c.checkOutputTarget(paths[0]); err != nil {
			c.Ui.Error(err.Error())
			return 1
		}
	}

	exit := 0
	for _, path := range paths {
		if status := c.formatPath(path, formatOverride, outputOverride); status > exit {
			exit = status
		}
	}
	return exit
}

// checkFlagCombination rejects the flag pairs that cannot both be honoured.
//
// Each test is flags.Changed rather than the parsed value, so it fires on
// the flag having been typed rather than on what it was set to - the
// objection is to the combination, which is the same rule
// stdoutInertFlagError applies for init and export (#419). A flag that
// only means something in a mode the command is not in gets an error
// rather than being quietly dropped.
func (c *FmtCommand) checkFlagCombination(flags *flag.FlagSet) error {
	if flags.Changed("force") && !flags.Changed("output") {
		return fmt.Errorf("--force only applies to --output; formatting a recipe in place already overwrites it")
	}
	if flags.Changed("output") {
		for _, name := range []string{"check", "diff"} {
			if flags.Changed(name) {
				return fmt.Errorf("--output cannot be used with --%s; --%s never writes a file", name, name)
			}
		}
		// --force asks to replace a file, and a stream has none.
		return stdoutInertFlagError(flags, c.output, []stdoutInertFlag{
			{name: "force", reason: "a streamed recipe writes no files"},
		})
	}
	return nil
}

// checkOutputTarget refuses an --output that would destroy a file the
// command was not asked to touch.
//
// Formatting in place overwrites the file it read, which is fmt's whole
// job, so an --output naming that same file needs no permission. Any other
// existing path does: `docket fmt --output tasks.yml staging/tasks.yml`
// would otherwise silently replace an unrelated recipe. The wording
// matches init's, which guards the same hazard.
func (c *FmtCommand) checkOutputTarget(sourcePath string) error {
	if c.output == "" || c.output == taskFileStdin {
		return nil
	}
	target := inDir(c.baseDir(), c.output)
	if samePath(target, sourcePath) {
		return nil
	}
	if _, err := os.Stat(target); err == nil && !c.force {
		return fmt.Errorf("file %s already exists; pass --force to overwrite", c.output)
	}
	return nil
}

// runStdin reads stdin and formats or converts it. The result goes to
// stdout, or to --output when one is named.
//
// Stdin has no filename to drive format detection, so it sniffs the
// first non-trivia byte: a leading [ or { signals JSON5; anything else
// (including the typical `---` document marker or a leading comment-
// only YAML file) goes through the YAML formatter. formatOverride, from
// --tasks-format, wins over the sniff - a flow-style YAML recipe starts
// with [ and would otherwise be reformatted as JSON5.
func (c *FmtCommand) runStdin(formatOverride, outputOverride string) int {
	src, err := c.stdinSource().recipe()
	if err != nil {
		c.Ui.Error(fmt.Sprintf("read stdin: %v", err))
		return 1
	}
	inFormat := taskFileFormatFor("", formatOverride, src)
	outFormat := taskFileOutputFormatFor(outputOverride, c.output, inFormat)

	writePath := ""
	if c.output != "" && c.output != taskFileStdin {
		writePath = inDir(c.baseDir(), c.output)
	}
	return c.writeFormatted(taskFileDisplayName(taskFileStdin), src, inFormat, outFormat, writePath, "")
}

// formatPath formats a single file. Returns 0 on success, 1 on any
// error or --check mismatch. Errors are reported via c.Ui and the
// caller picks the worst-of exit code across all paths.
func (c *FmtCommand) formatPath(path, formatOverride, outputOverride string) int {
	src, err := os.ReadFile(path)
	if err != nil {
		c.Ui.Error(fmt.Sprintf("read %s: %v", path, err))
		return 1
	}

	inFormat := taskFileFormatFor(detectTaskFileFormat(path), formatOverride, src)
	outFormat := taskFileOutputFormatFor(outputOverride, c.output, inFormat)

	writePath := path
	if c.output != "" {
		writePath = ""
		if c.output != taskFileStdin {
			writePath = inDir(c.baseDir(), c.output)
		}
	}
	return c.writeFormatted(path, src, inFormat, outFormat, writePath, path)
}

// writeFormatted is the one place a recipe is canonicalised and its result
// disposed of, shared by the stdin and per-file paths so the two cannot
// drift on what --check, --diff and a conversion mean.
//
// writePath is the file to write, or "" to stream to stdout. sourcePath is
// the file the bytes came from, or "" for stdin; the two are equal for an
// ordinary in-place format, which is what lets the mtime be left alone.
func (c *FmtCommand) writeFormatted(display string, src []byte, inFormat, outFormat, writePath, sourcePath string) int {
	converting := outFormat != inFormat

	formatted, err := tasks.Convert(src, tasks.CodecFor(inFormat), tasks.CodecFor(outFormat))
	if err != nil {
		c.Ui.Error(fmt.Sprintf("%s: %v", display, err))
		return 1
	}

	changed := !bytesEqual(src, formatted)

	// The diff is rendered before the --check rejection below, so
	// `--check --diff` still shows what it objected to.
	if c.diff && changed {
		_, _ = io.WriteString(c.stdout(), renderDiff(display, string(src), string(formatted), c.useColor))
	}

	if c.check {
		if converting {
			// --check asks whether a recipe is already canonical, and a
			// conversion is never a no-op, so the answer would be "no" for
			// every file however clean it is. Say that rather than exiting
			// 1 and letting a CI lint job read it as a formatting failure.
			c.Ui.Error(fmt.Sprintf("[error]   %s: --check cannot be combined with --format %s on a %s recipe; a conversion is never a no-op", display, outFormat, inFormat))
			c.Ui.Error(fmt.Sprintf("          to check a recipe whose extension is misleading, use: %s fmt --check --tasks-format %s", appName(), outFormat))
			return 1
		}
		if changed {
			c.Ui.Error(fmt.Sprintf("[error]   %s is not canonically formatted", display))
			c.Ui.Error(fmt.Sprintf("          run: %s fmt %s", appName(), display))
			return 1
		}
		return 0
	}

	if c.diff {
		// --diff alone: never write, even on stdin.
		return 0
	}

	if writePath == "" {
		if _, err := c.stdout().Write(formatted); err != nil {
			c.Ui.Error(fmt.Sprintf("write stdout: %v", err))
			return 1
		}
		return 0
	}

	if samePath(writePath, sourcePath) && !changed {
		// no-op preservation: leave the file untouched so mtime
		// stays clean for make / file-watchers.
		return 0
	}

	// A conversion can leave a file whose extension no longer describes its
	// contents, which nothing downstream can tell: a later `docket validate
	// --tasks tasks.yml` picks its parser from the name. Say it once here
	// rather than letting the user find out from a parse error. A write
	// that is not converting says nothing, so `--tasks-format json5
	// recipe.yml` stays as quiet as it has always been.
	if converting {
		if msg := recipeOutputFormatMismatch(writePath, outFormat); msg != "" {
			c.Ui.Warn(msg)
		}
	}

	if err := os.WriteFile(writePath, formatted, 0o644); err != nil {
		c.Ui.Error(fmt.Sprintf("write %s: %v", writePath, err))
		return 1
	}
	if samePath(writePath, sourcePath) {
		c.Ui.Output(fmt.Sprintf("==> Formatted %s", writePath))
	} else {
		c.Ui.Output(fmt.Sprintf("==> Wrote %s", writePath))
	}
	return 0
}

// samePath reports whether two paths name the same file, so `--output
// ./tasks.yml tasks.yml` is recognised as the in-place write it is - by
// the overwrite guard, which must not demand --force for it, and by the
// write step, which reports it as formatted rather than written and leaves
// the mtime alone when nothing changed. Two empty paths are not a path.
func samePath(a, b string) bool {
	if a == "" || b == "" {
		return false
	}
	return filepath.Clean(a) == filepath.Clean(b)
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
