package commands

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mattn/go-isatty"
	"github.com/posener/complete"
	flag "github.com/spf13/pflag"
)

// Task file format identifiers used throughout the commands package and
// passed through to tasks.GetPlaysWithFormat / tasks.Validate via
// ValidateOptions.Format. Only these two values are valid; other strings
// are treated as YAML by the dispatchers.
const (
	taskFileFormatYAML  = "yaml"
	taskFileFormatJSON5 = "json5"
)

// taskFileStdin is the conventional path meaning "read the recipe from
// standard input". `docket fmt -` has always accepted it; apply, plan,
// and validate accept it both as a --tasks value and as a bare
// positional argument.
const taskFileStdin = "-"

// defaultTaskFileCandidates is the ordered list of filenames probed when
// --tasks is not given. The first one that exists in the working
// directory is used. The order matches the legacy default (tasks.yml)
// so behaviour does not change for existing recipes; .yaml and .json
// fall through to give JSON-native users a no-config setup.
var defaultTaskFileCandidates = []string{"tasks.yml", "tasks.yaml", "tasks.json"}

// parseRecipeFormatFlag normalises a recipe-format flag value to one of
// the two canonical format identifiers. An empty value means "not set"
// and leaves the decision to the caller. Anything else is rejected
// naming the accepted values, the way an invalid --color is.
//
// flagName is the spelling to blame in that rejection: the input side
// (apply / plan / validate / fmt) passes "--tasks-format", the output
// side (init / export, #410) passes "--format". One normaliser keeps the
// two flags accepting exactly the same spellings.
func parseRecipeFormatFlag(flagName, value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", nil
	case "yaml", "yml":
		return taskFileFormatYAML, nil
	case "json", "json5":
		return taskFileFormatJSON5, nil
	}
	return "", fmt.Errorf("invalid %s %q: must be one of yaml, json5", flagName, value)
}

// taskFileFormatFor resolves the format of a recipe from the three
// signals available, in precedence order:
//
//  1. override - an explicit --tasks-format, already normalised by
//     parseRecipeFormatFlag
//  2. detected - the extension of the path or URL, from
//     detectTaskFileFormat; empty when the source is stdin, which has no
//     name to key off
//  3. data - a content sniff, the same one `docket fmt -` has always
//     used for stdin
//
// The return value is never empty. That matters: tasks.IsJSON5Format("")
// is false, so an empty format silently means YAML downstream, and a
// caller that skipped the sniff would parse a JSON5 recipe with the YAML
// parser and report a confusing error instead of an obvious one.
func taskFileFormatFor(detected, override string, data []byte) string {
	if override != "" {
		return override
	}
	if detected != "" {
		return detected
	}
	return sniffStdinFormat(data)
}

// taskFileDisplayName renders a recipe source for human output. stdin
// has no path, so it prints as <stdin> - the same spelling `docket fmt`
// already uses in its diff headers.
func taskFileDisplayName(path string) string {
	if path == taskFileStdin {
		return "<stdin>"
	}
	return path
}

// shellQuotePath renders path so a printed command survives a paste into
// a POSIX shell. A path made only of characters the shell leaves alone
// comes back untouched; anything else is single-quoted, with an embedded
// single quote spelled '\''.
//
// The safe set is an allowlist rather than a list of metacharacters to
// escape, the same rule as Python's shlex.quote: a character nobody
// thought about ends up quoted instead of ending up in the command.
func shellQuotePath(path string) string {
	if path != "" && !strings.ContainsFunc(path, shellUnsafeRune) {
		return path
	}
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

// shellUnsafeRune reports whether r needs the surrounding path quoted.
func shellUnsafeRune(r rune) bool {
	switch {
	case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		return false
	case strings.ContainsRune("@%_+=:,./-", r):
		return false
	}
	return true
}

// detectTaskFileFormat returns "json5" when path's extension is .json or
// .json5 (case-insensitive), and "yaml" otherwise. Unknown extensions
// default to YAML so explicit paths like `--tasks recipe.txt` keep the
// pre-#218 behaviour. For an http(s) URL the format is taken from the URL
// path component so a trailing query string or fragment
// (`tasks.json?ref=main`) does not get glued onto the extension.
func detectTaskFileFormat(path string) string {
	ext := filepath.Ext(path)
	if isTaskFileURL(path) {
		if u, err := url.Parse(path); err == nil {
			ext = filepath.Ext(u.Path)
		}
	}
	switch strings.ToLower(ext) {
	case ".json", ".json5":
		return taskFileFormatJSON5
	default:
		return taskFileFormatYAML
	}
}

// defaultRecipeOutput and defaultRecipeOutputJSON5 are the --output
// defaults for the two commands that write a recipe (init, export).
// resolveRecipeOutput swaps in the JSON5 spelling when --format json5 is
// given without an explicit --output, so the file name matches its
// contents and a later bare `docket validate` still finds it - both are
// in defaultTaskFileCandidates.
const (
	defaultRecipeOutput      = "tasks.yml"
	defaultRecipeOutputJSON5 = "tasks.json"
)

// resolveRecipeOutput reconciles an explicit --format with the --output
// path a recipe-writing command is about to use, returning the path to
// write and the format to write it in.
//
// Precedence, per #410:
//
//  1. override - an explicit --format, already normalised by
//     parseRecipeFormatFlag. It always wins, including over an --output
//     extension that says otherwise.
//  2. the --output extension, via detectTaskFileFormat.
//  3. YAML - all that is left for stdout, which has no extension. This is
//     the gap #410 exists to close: before --format, `--output -` could
//     only ever emit YAML.
//
// outputChanged is flags.Changed("output"), which is only meaningful
// after flags.Parse. When --format asks for JSON5 and --output was left
// at its default, the default path moves to defaultRecipeOutputJSON5
// rather than dropping a JSON5 document into a .yml file. A path the user
// typed - including "-" - is never rewritten.
//
// Callers must run this before their --force / --overwrite existence
// checks: those have to test the path that will actually be written.
func resolveRecipeOutput(output, override string, outputChanged bool) (string, string) {
	if override == "" {
		if output == taskFileStdin {
			return output, taskFileFormatYAML
		}
		return output, detectTaskFileFormat(output)
	}
	if output != taskFileStdin && !outputChanged && override == taskFileFormatJSON5 {
		output = defaultRecipeOutputJSON5
	}
	return output, override
}

// recipeOutputFormatMismatch returns the warning to print when --format
// disagrees with what path's extension implies, or "" when there is
// nothing to say. Writing a JSON5 recipe to tasks.yml is legal - --format
// always wins - but nothing downstream can tell: the JSON5 formatter
// emits unquoted keys, comments, and trailing commas, none of which parse
// as YAML, and a later `docket validate --tasks tasks.yml` picks its
// parser from the extension. Say it once, on stderr, instead of letting
// the user find out from a parse error.
//
// The companion vars-file deliberately gets no warning: MarshalVars emits
// plain JSON, which is valid YAML, so a .yml vars-file holding JSON still
// loads.
func recipeOutputFormatMismatch(path, override string) string {
	if override == "" || path == taskFileStdin {
		return ""
	}
	if detectTaskFileFormat(path) == override {
		return ""
	}
	return fmt.Sprintf("warning: --format %s does not match the %s extension; reading %s back needs --tasks-format %s",
		override, path, path, override)
}

// stdoutInertFlag names a flag on a recipe-writing command that only means
// something once there is a file to write, together with the clause
// explaining why a stream cannot honour it.
type stdoutInertFlag struct {
	name   string
	reason string
}

// stdoutInertFlagError rejects those flags when --output - has turned the
// write into a stream. Both init and export return from their stdout
// branch before the file-only flags are ever consulted, so without this
// the flag is read off the flag set and dropped: `docket export --output -
// --vars-output secrets.yml` exited 0, wrote nothing to that path, and said
// nothing about it (#419). Erroring beats warning because there is no
// reading of such a command line under which being ignored is what was
// wanted.
//
// flags.Changed - not the parsed value - is the test, so this fires on the
// flag having been typed rather than on what it was set to, and
// `--overwrite=false --output -` is rejected too: the objection is to the
// combination, not to the value. Changed is only meaningful after
// flags.Parse, the same ordering constraint resolveRecipeOutput documents.
//
// Callers must run this after resolveRecipeOutput so output is the path
// that will actually be written.
func stdoutInertFlagError(flags *flag.FlagSet, output string, inert []stdoutInertFlag) error {
	if output != taskFileStdin {
		return nil
	}
	for _, f := range inert {
		if flags.Changed(f.name) {
			return fmt.Errorf("--%s cannot be used with --output -; %s", f.name, f.reason)
		}
	}
	return nil
}

// taskFileFetchTimeout bounds a remote recipe fetch so a hung server does
// not stall the whole command.
const taskFileFetchTimeout = 30 * time.Second

// maxTaskFileBytes caps the size of a fetched recipe so a runaway or
// hostile response cannot exhaust memory. Recipes are small; 16 MiB is far
// above any realistic task file.
const maxTaskFileBytes = 16 << 20

// isTaskFileURL reports whether path is an http(s) URL docket should fetch
// over HTTP rather than read from the local filesystem.
func isTaskFileURL(path string) bool {
	return strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://")
}

// readTaskFileData returns the bytes of the recipe at path, allowing an
// http(s) URL. Kept as the name apply and plan already call, now a thin
// wrapper so there is only one read path to reason about.
func readTaskFileData(path string, stdin *stdinRecipeSource) ([]byte, error) {
	return readRecipeBytes(path, true, stdin)
}

// readRecipeBytes returns the bytes of the recipe at path.
//
// "-" reads standard input. An http(s) URL is fetched over HTTP, but
// only when allowURL is set: apply and plan advertise a remote --tasks
// URL in their help, validate deliberately does not, and passing the
// permission explicitly keeps that contract visible at the call site
// rather than hidden in a function name. Any other value is read from
// disk so the familiar os.ReadFile "no such file" error still surfaces
// for a mistyped path.
func readRecipeBytes(path string, allowURL bool, stdin *stdinRecipeSource) ([]byte, error) {
	if path == taskFileStdin {
		return stdin.recipe()
	}
	if allowURL && isTaskFileURL(path) {
		return fetchTaskFileURL(path)
	}
	return os.ReadFile(path)
}

// stdinRecipeSource memoizes the one read of standard input a command gets.
// Each of apply, plan, and validate touches the recipe at least twice - once in
// FlagSet() to pre-register the recipe's own inputs as flags, once in Run() -
// and a flag-parse error adds two more reads via Help(). Only the first read
// can return data, so every later caller is served from here.
//
// The error is memoized alongside the data on purpose. Without it a second call
// would see an empty, successful read and turn a real stdin failure into a
// misleading "no recipe found in tasks file".
//
// One source belongs to one command. It used to be a package variable with a
// test-only reset hook, which meant a test exercising a piped recipe had to
// swap os.Stdin and put it back - process state no two tests can hold at once.
type stdinRecipeSource struct {
	in   io.Reader
	read bool
	data []byte
	err  error
}

// newStdinRecipeSource returns a source reading from in, or from the process's
// standard input when in is nil.
func newStdinRecipeSource(in io.Reader) *stdinRecipeSource {
	if in == nil {
		in = os.Stdin
	}
	return &stdinRecipeSource{in: in}
}

// recipe returns the recipe piped in, reading it at most once.
func (s *stdinRecipeSource) recipe() ([]byte, error) {
	if !s.read {
		s.read = true
		s.data, s.err = io.ReadAll(s.in)
	}
	return s.data, s.err
}


// fetchTaskFileURL GETs a recipe from an http(s) URL. A transport error, a
// non-2xx response, or a body larger than maxTaskFileBytes is reported as
// an error naming the URL so the read-error message stays actionable.
func fetchTaskFileURL(rawURL string) ([]byte, error) {
	client := &http.Client{Timeout: taskFileFetchTimeout}
	resp, err := client.Get(rawURL)
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("fetch %s: unexpected status %s", rawURL, resp.Status)
	}

	// Read one byte past the cap so an over-limit body is detected rather
	// than silently truncated.
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxTaskFileBytes+1))
	if err != nil {
		return nil, fmt.Errorf("fetch %s: %w", rawURL, err)
	}
	if len(data) > maxTaskFileBytes {
		return nil, fmt.Errorf("fetch %s: recipe exceeds %d bytes", rawURL, maxTaskFileBytes)
	}
	return data, nil
}

// taskFileExtensions lists the recipe file extensions docket recognises,
// the single source of truth for hasTaskFileExtension and taskFileAutocomplete.
var taskFileExtensions = []string{"yml", "yaml", "json", "json5"}

// hasTaskFileExtension reports whether path carries one of the recipe
// file extensions. Used to spot a positional recipe path in an argv the
// flag parser has not yet processed.
func hasTaskFileExtension(path string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	for _, candidate := range taskFileExtensions {
		if ext == candidate {
			return true
		}
	}
	return false
}

// probeDefaultTaskFile stats defaultTaskFileCandidates in order and
// returns the first one that exists (chosen) together with every later
// candidate that also exists (others). An empty chosen and a nil error
// means none of them exist; whether that is fatal is the caller's call -
// resolveTaskFilePath turns it into an error, expandPaths and
// resolveTaskFileFromArgs fall back to defaultTaskFileCandidates[0].
//
// A stat error other than "does not exist" is reported only when it
// happens before any candidate has matched. That is what the probe has
// always done by returning on its first hit: an unreadable tasks.json
// sitting next to a perfectly good tasks.yml has never been fatal, and
// collecting others must not make it so.
func probeDefaultTaskFile() (string, []string, error) {
	var existing []string
	for _, candidate := range defaultTaskFileCandidates {
		_, err := os.Stat(candidate)
		if err == nil {
			existing = append(existing, candidate)
			continue
		}
		if len(existing) == 0 && !errors.Is(err, os.ErrNotExist) {
			return "", nil, fmt.Errorf("stat %s: %w", candidate, err)
		}
	}
	if len(existing) == 0 {
		return "", nil, nil
	}
	return existing[0], existing[1:], nil
}

// ambiguousTaskFileWarning returns the warning to print when the default
// probe had more than one candidate to choose from, or "" when it did
// not. A directory holding both tasks.yml and tasks.json is ambiguous for
// every command that probes, and the choice is invisible in the output:
// `docket validate` reporting "tasks.yml is valid" reads as confirmation
// that the tasks.json next to it is fine (#420). Say which file was read
// and how to pick the other one.
func ambiguousTaskFileWarning(chosen string, others []string) string {
	if chosen == "" || len(others) == 0 {
		return ""
	}
	// "both" for a pair, "all" once there are three; naming every file
	// keeps the message useful when the directory is worse than expected.
	quantifier := "both"
	if len(others) > 1 {
		quantifier = "all"
	}
	names := append([]string{chosen}, others...)
	return fmt.Sprintf("warning: %s %s exist; using %s (pass --tasks to choose)",
		strings.Join(names, ", "), quantifier, chosen)
}

// resolveTaskFilePath returns the path to use as the task file plus its
// detected format. When explicit is non-empty it is used as-is and the
// format is inferred from its extension; the file's existence is not
// checked here so the caller's os.ReadFile produces the canonical "no
// such file" error message. When explicit is empty the function probes
// defaultTaskFileCandidates in order and returns the first one that
// exists. If none exist the returned error names every candidate so the
// user can see which paths were tried.
//
// The third return value carries the other candidates the probe found,
// for ambiguousTaskFileWarning; it is nil whenever the probe did not run.
func resolveTaskFilePath(explicit string) (string, string, []string, error) {
	if explicit == taskFileStdin {
		// stdin has no name to detect a format from; the empty format
		// tells taskFileFormatFor to sniff the bytes instead. Probing
		// the default candidates here would silently prefer ./tasks.yml
		// over the recipe the user actually piped in.
		return taskFileStdin, "", nil, nil
	}
	if explicit != "" {
		return explicit, detectTaskFileFormat(explicit), nil, nil
	}
	chosen, others, err := probeDefaultTaskFile()
	if err != nil {
		return "", "", nil, err
	}
	if chosen == "" {
		return "", "", nil, fmt.Errorf("no task file found; looked for %s", strings.Join(defaultTaskFileCandidates, ", "))
	}
	return chosen, detectTaskFileFormat(chosen), others, nil
}

// resolveTaskFileArg reconciles the --tasks flag value with any positional
// file arguments left after flag parsing. A positional recipe path (e.g.
// `docket validate staging/tasks.yml`) is honored the way `docket fmt`
// honors one, so a CI lint that names the file checks that file rather
// than silently falling back to ./tasks.yml. Passing both --tasks and a
// positional, or more than one positional, is rejected. An empty return
// with a nil error means "use the default probe" (neither was given).
func resolveTaskFileArg(explicit string, positional []string) (string, error) {
	if len(positional) == 0 {
		return explicit, nil
	}
	if len(positional) > 1 {
		return "", fmt.Errorf("only one task file may be specified, got %d", len(positional))
	}
	if explicit != "" {
		return "", fmt.Errorf("cannot specify both --tasks and a positional task file argument")
	}
	return positional[0], nil
}

// recipeSource is a fully resolved recipe: where it came from, how to
// name it in output, its bytes, and the format to parse it with.
type recipeSource struct {
	// Path is the resolved source: a file path, an http(s) URL, or "-".
	Path string
	// Display is Path rendered for humans; "<stdin>" when Path is "-".
	Display string
	// Data is the recipe bytes.
	Data []byte
	// Format is always taskFileFormatYAML or taskFileFormatJSON5, never
	// empty. See taskFileFormatFor.
	Format string
	// Ambiguous holds the default candidates the probe passed over
	// because Path won. It is empty unless the probe actually ran, so an
	// explicit --tasks, a positional path, a URL, and stdin never
	// populate it. Feed it to ambiguousTaskFileWarning.
	Ambiguous []string
}

// loadRecipe resolves, reads, and format-detects the recipe for a
// command's Run. taskFile is the already-reconciled --tasks / positional
// value from resolveTaskFileArg (empty means "probe the defaults"); that
// reconciliation stays with the caller so an argument error and a read
// error remain distinguishable to a --json consumer.
//
// cached / cachedSource carry bytes an earlier phase already read for
// the same source, so a --tasks URL is not fetched over HTTP twice; pass
// nil / "" when there is nothing to reuse. stdin needs no help here -
// stdinRecipeSource memoizes it - but a URL fetch is not memoized
// and would otherwise hit the network again.
//
// allowURL is threaded through to readRecipeBytes; see its doc comment.
func loadRecipe(taskFile, formatOverride string, allowURL bool, cached []byte, cachedSource string, stdin *stdinRecipeSource) (recipeSource, error) {
	path, detected, ambiguous, err := resolveTaskFilePath(taskFile)
	if err != nil {
		return recipeSource{}, err
	}

	data := cached
	if data == nil || cachedSource != path {
		data, err = readRecipeBytes(path, allowURL, stdin)
		if err != nil {
			return recipeSource{}, err
		}
	}

	if path == taskFileStdin && len(data) == 0 {
		// "no recipe found in tasks file" is what the parser would say,
		// and it is actively misleading when there was no file. A
		// generator upstream in the pipe produced nothing; say so.
		return recipeSource{}, fmt.Errorf("recipe on stdin is empty")
	}

	return recipeSource{
		Path:      path,
		Display:   taskFileDisplayName(path),
		Data:      data,
		Format:    taskFileFormatFor(detected, formatOverride, data),
		Ambiguous: ambiguous,
	}, nil
}

// preloadRecipeForFlags reads the recipe during FlagSet construction,
// before pflag has parsed anything, so each recipe input can be
// registered as a real --<name> flag. It works off raw argv because
// flags.Args() does not exist yet.
//
// It returns the bytes, the format to parse them with, and the source
// they came from - apply and plan record the source so Run can reuse
// the bytes rather than fetching the same --tasks URL twice.
//
// It is best-effort: on any failure it returns nil data and lets the
// caller skip input pre-registration, because Run re-resolves the recipe
// and reports the real error there.
//
// It never blocks. `docket - apply` and `docket apply - --help` both
// reach FlagSet() from a pure help/error path (mitchellh/cli's
// commandHelp, which calls FlagSet twice), so reading an interactive
// terminal here would hang instead of printing the message the user
// asked for. Only Run may block on stdin.
func preloadRecipeForFlags(argv []string, allowURL bool, stdin *stdinRecipeSource) (data []byte, format string, source string) {
	path, detected := resolveTaskFileFromArgs(argv)
	if path == taskFileStdin && isatty.IsTerminal(os.Stdin.Fd()) {
		return nil, "", path
	}
	data, err := readRecipeBytes(path, allowURL, stdin)
	if err != nil {
		return nil, "", path
	}
	// The override is read straight from argv: pflag has not run, so
	// the command's flag field is still empty. An unrecognised value is
	// ignored here and rejected properly by parseRecipeFormatFlag in Run.
	override, _ := parseRecipeFormatFlag("--tasks-format", tasksFormatFromArgs(argv))
	return data, taskFileFormatFor(detected, override, data), path
}

// predictFilesByExtension returns a completion predictor offering files whose
// name ends in one of the given extensions (each without a leading dot, e.g.
// "yml"), plus directories for navigation, each offered exactly once.
//
// complete.PredictFiles feeds its pattern to filepath.Glob, whose
// filepath.Match engine has no brace expansion, so a single "*.{yml,yaml}"
// glob matches nothing (#340). Unioning one PredictFiles per extension
// restores per-extension matching; the dedupe stops a directory (which every
// sub-predictor lists) from being offered once per extension -- the library
// prints every option without deduping (posener/complete complete.go output).
func predictFilesByExtension(extensions []string) complete.Predictor {
	predictors := make([]complete.Predictor, 0, len(extensions))
	for _, ext := range extensions {
		predictors = append(predictors, complete.PredictFiles("*."+ext))
	}
	return complete.PredictFunc(func(a complete.Args) []string {
		seen := make(map[string]bool)
		var matches []string
		for _, p := range predictors {
			for _, match := range p.Predict(a) {
				if seen[match] {
					continue
				}
				seen[match] = true
				matches = append(matches, match)
			}
		}
		return matches
	})
}

// recipeFormatAutocomplete offers the two canonical values shared by
// --tasks-format on the reading side and --format on the writing side.
// The yml / json aliases parseRecipeFormatFlag also accepts are
// deliberately left out so completion suggests one spelling per format.
func recipeFormatAutocomplete() complete.Predictor {
	return complete.PredictSet(taskFileFormatYAML, taskFileFormatJSON5)
}

// taskFileAutocomplete is the file-completion predictor shared by the
// --tasks / --output / --vars-output flags and `docket fmt`'s positional
// argument across apply / plan / validate / fmt / init / export.
func taskFileAutocomplete() complete.Predictor {
	return predictFilesByExtension(taskFileExtensions)
}

// commandArgv returns the argv a command should resolve its pre-parse flags
// from: the one it was given, or the process's when it was given none.
//
// Reading os.Args directly is what forced every test that exercises FlagSet to
// assign to the process global and restore it afterwards, which is a thing
// t.Parallel() makes unsafe.
func commandArgv(argv []string) []string {
	if argv != nil {
		return argv
	}
	return os.Args
}
