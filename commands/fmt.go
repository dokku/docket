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

	check bool
	diff  bool
	color string
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
		"Format every yaml under recipes/":        fmt.Sprintf("%s %s 'recipes/*.yml'", appName, c.Name()),
		"Format every JSON5 file under recipes/":  fmt.Sprintf("%s %s 'recipes/*.json5'", appName, c.Name()),
		"Force colorized diff in a pipe":          fmt.Sprintf("%s %s --diff --color always", appName, c.Name()),
	}
}

func (c *FmtCommand) Arguments() []command.Argument {
	return []command.Argument{}
}

func (c *FmtCommand) AutocompleteArgs() complete.Predictor {
	return complete.PredictFiles(taskFileAutocompleteGlob)
}

func (c *FmtCommand) ParsedArguments(args []string) (map[string]command.Argument, error) {
	return command.ParseArguments(args, c.Arguments())
}

func (c *FmtCommand) FlagSet() *flag.FlagSet {
	f := c.Meta.FlagSet(c.Name(), command.FlagSetClient)
	f.BoolVar(&c.check, "check", false, "exit non-zero if any file is not canonically formatted; do not write")
	f.BoolVar(&c.diff, "diff", false, "print a unified diff for any file that is not canonically formatted; do not write")
	f.StringVar(&c.color, "color", "auto", "when to colorize diff output: auto, always, never")
	return f
}

func (c *FmtCommand) AutocompleteFlags() complete.Flags {
	return command.MergeAutocompleteFlags(
		c.Meta.AutocompleteFlags(command.FlagSetClient),
		complete.Flags{
			"--check": complete.PredictNothing,
			"--diff":  complete.PredictNothing,
			"--color": complete.PredictSet("auto", "always", "never"),
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

	if err := applyColorMode(c.color); err != nil {
		c.Ui.Error(err.Error())
		return 1
	}

	positional := flags.Args()
	if len(positional) == 1 && positional[0] == "-" {
		return c.runStdin()
	}

	paths, err := expandPaths(positional)
	if err != nil {
		c.Ui.Error(err.Error())
		return 1
	}

	exit := 0
	for _, path := range paths {
		if status := c.formatPath(path); status > exit {
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
// only YAML file) goes through the YAML formatter.
func (c *FmtCommand) runStdin() int {
	src, err := io.ReadAll(os.Stdin)
	if err != nil {
		c.Ui.Error(fmt.Sprintf("read stdin: %v", err))
		return 1
	}
	formatted, err := formatTaskFileBytes(src, sniffStdinFormat(src))
	if err != nil {
		c.Ui.Error(fmt.Sprintf("format error: %v", err))
		return 1
	}

	changed := !bytesEqual(src, formatted)

	if c.diff && changed {
		_, _ = os.Stdout.WriteString(renderDiff("<stdin>", string(src), string(formatted)))
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

	if _, err := os.Stdout.Write(formatted); err != nil {
		c.Ui.Error(fmt.Sprintf("write stdout: %v", err))
		return 1
	}
	return 0
}

// formatPath formats a single file. Returns 0 on success, 1 on any
// error or --check mismatch. Errors are reported via c.Ui and the
// caller picks the worst-of exit code across all paths.
func (c *FmtCommand) formatPath(path string) int {
	src, err := os.ReadFile(path)
	if err != nil {
		c.Ui.Error(fmt.Sprintf("read %s: %v", path, err))
		return 1
	}

	formatted, err := formatTaskFileBytes(src, detectTaskFileFormat(path))
	if err != nil {
		c.Ui.Error(fmt.Sprintf("%s: %v", path, err))
		return 1
	}

	changed := !bytesEqual(src, formatted)

	if c.diff && changed {
		_, _ = os.Stdout.WriteString(renderDiff(path, string(src), string(formatted)))
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
func expandPaths(args []string) ([]string, error) {
	if len(args) == 0 {
		for _, candidate := range defaultTaskFileCandidates {
			if _, err := os.Stat(candidate); err == nil {
				return []string{candidate}, nil
			}
		}
		return []string{defaultTaskFileCandidates[0]}, nil
	}

	seen := map[string]bool{}
	var paths []string
	for _, arg := range args {
		matches, err := filepath.Glob(arg)
		if err != nil {
			return nil, fmt.Errorf("invalid glob %q: %w", arg, err)
		}
		if len(matches) == 0 {
			// A literal path with no glob metacharacters that does
			// not exist still flows through to the read step so
			// the user gets a clean "no such file" error.
			matches = []string{arg}
		}
		for _, m := range matches {
			if !seen[m] {
				seen[m] = true
				paths = append(paths, m)
			}
		}
	}
	sort.Strings(paths)
	return paths, nil
}

// applyColorMode resolves the --color flag value into the global
// fatih/color state. auto delegates to the library's TTY / NO_COLOR
// detection; always forces colors on; never forces colors off.
func applyColorMode(mode string) error {
	switch mode {
	case "auto":
		// Default behaviour: respect TTY and NO_COLOR. fatih/color
		// already does this when color.NoColor is left at its
		// package-default value. Re-derive the default explicitly
		// so a previous --color always/never invocation in the same
		// process (i.e. tests) does not leak state.
		color.NoColor = noColorDefault()
	case "always":
		color.NoColor = false
	case "never":
		color.NoColor = true
	default:
		return fmt.Errorf("invalid --color value %q (allowed: auto, always, never)", mode)
	}
	return nil
}

// noColorDefault matches fatih/color's own default detection: colors
// on when stdout is a terminal AND NO_COLOR is unset.
func noColorDefault() bool {
	if _, ok := os.LookupEnv("NO_COLOR"); ok {
		return true
	}
	return !isatty.IsTerminal(os.Stdout.Fd()) && !isatty.IsCygwinTerminal(os.Stdout.Fd())
}

// renderDiff produces a colorized GNU unified diff between original
// and formatted with file path on both header lines (no a/ b/ prefix,
// matching gofmt / black). The output round-trips through patch -p0
// once colors are stripped.
func renderDiff(path, original, formatted string) string {
	raw, err := udiff.ToUnified(path, path, original, udiff.Strings(original, formatted), udiff.DefaultContextLines)
	if err != nil {
		return fmt.Sprintf("[error]   diff failed for %s: %v\n", path, err)
	}
	if raw == "" {
		return ""
	}
	return colorizeDiff(raw)
}

var (
	diffRemoveLine = color.New(color.FgRed)
	diffAddLine    = color.New(color.FgGreen)
	diffHunk       = color.New(color.FgCyan)
	diffFileHeader = color.New(color.Bold)
)

// colorizeDiff walks the unified diff output line by line and applies
// ANSI colors. fatih/color is a no-op when color.NoColor is true (set
// by --color never, NO_COLOR, or non-TTY auto), so the same code
// produces plain output in CI / pipes.
func colorizeDiff(raw string) string {
	var b strings.Builder
	for _, line := range strings.SplitAfter(raw, "\n") {
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "--- ") || strings.HasPrefix(line, "+++ "):
			b.WriteString(diffFileHeader.Sprint(line))
		case strings.HasPrefix(line, "@@"):
			b.WriteString(diffHunk.Sprint(line))
		case strings.HasPrefix(line, "-"):
			b.WriteString(diffRemoveLine.Sprint(line))
		case strings.HasPrefix(line, "+"):
			b.WriteString(diffAddLine.Sprint(line))
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
