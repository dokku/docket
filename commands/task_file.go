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

// parseTasksFormatFlag normalises a --tasks-format value to one of the
// two canonical format identifiers. An empty value means "not set" and
// leaves detection to taskFileFormatFor. Anything else is rejected
// naming the accepted values, the way an invalid --color is.
func parseTasksFormatFlag(value string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "":
		return "", nil
	case "yaml", "yml":
		return taskFileFormatYAML, nil
	case "json", "json5":
		return taskFileFormatJSON5, nil
	}
	return "", fmt.Errorf("invalid --tasks-format %q: must be one of yaml, json5", value)
}

// taskFileFormatFor resolves the format of a recipe from the three
// signals available, in precedence order:
//
//  1. override - an explicit --tasks-format, already normalised by
//     parseTasksFormatFlag
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
func readTaskFileData(path string) ([]byte, error) {
	return readRecipeBytes(path, true)
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
func readRecipeBytes(path string, allowURL bool) ([]byte, error) {
	if path == taskFileStdin {
		return readStdinRecipe()
	}
	if allowURL && isTaskFileURL(path) {
		return fetchTaskFileURL(path)
	}
	return os.ReadFile(path)
}

// stdinRecipe memoizes the one read of os.Stdin a process gets. Each of
// apply, plan, and validate touches the recipe at least twice - once in
// FlagSet() to pre-register the recipe's own inputs as flags, once in
// Run() - and a flag-parse error adds two more reads via Help(). Only
// the first read can return data, so every later caller is served from
// here.
//
// The error is memoized alongside the data on purpose. Without it a
// second call would see an empty, successful read and turn a real stdin
// failure into a misleading "no recipe found in tasks file".
//
// A plain sentinel rather than sync.Once: the command path is
// single-goroutine, and a Once paired with a test reset hook reads like
// a concurrency bug that isn't there.
var stdinRecipe struct {
	read bool
	data []byte
	err  error
}

// readStdinRecipe returns the recipe piped into standard input, reading
// it at most once per process.
func readStdinRecipe() ([]byte, error) {
	if !stdinRecipe.read {
		stdinRecipe.read = true
		stdinRecipe.data, stdinRecipe.err = io.ReadAll(os.Stdin)
	}
	return stdinRecipe.data, stdinRecipe.err
}

// resetStdinRecipe clears the memo so a test can swap os.Stdin and read
// again. Production code never calls it - a process only ever has one
// stdin.
func resetStdinRecipe() {
	stdinRecipe.read = false
	stdinRecipe.data = nil
	stdinRecipe.err = nil
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

// resolveTaskFilePath returns the path to use as the task file plus its
// detected format. When explicit is non-empty it is used as-is and the
// format is inferred from its extension; the file's existence is not
// checked here so the caller's os.ReadFile produces the canonical "no
// such file" error message. When explicit is empty the function probes
// defaultTaskFileCandidates in order and returns the first one that
// exists. If none exist the returned error names every candidate so the
// user can see which paths were tried.
func resolveTaskFilePath(explicit string) (string, string, error) {
	if explicit == taskFileStdin {
		// stdin has no name to detect a format from; the empty format
		// tells taskFileFormatFor to sniff the bytes instead. Probing
		// the default candidates here would silently prefer ./tasks.yml
		// over the recipe the user actually piped in.
		return taskFileStdin, "", nil
	}
	if explicit != "" {
		return explicit, detectTaskFileFormat(explicit), nil
	}
	for _, candidate := range defaultTaskFileCandidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, detectTaskFileFormat(candidate), nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", "", fmt.Errorf("stat %s: %w", candidate, err)
		}
	}
	return "", "", fmt.Errorf("no task file found; looked for %s", strings.Join(defaultTaskFileCandidates, ", "))
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
// readStdinRecipe memoizes it globally - but a URL fetch is not memoized
// and would otherwise hit the network again.
//
// allowURL is threaded through to readRecipeBytes; see its doc comment.
func loadRecipe(taskFile, formatOverride string, allowURL bool, cached []byte, cachedSource string) (recipeSource, error) {
	path, detected, err := resolveTaskFilePath(taskFile)
	if err != nil {
		return recipeSource{}, err
	}

	data := cached
	if data == nil || cachedSource != path {
		data, err = readRecipeBytes(path, allowURL)
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
		Path:    path,
		Display: taskFileDisplayName(path),
		Data:    data,
		Format:  taskFileFormatFor(detected, formatOverride, data),
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
func preloadRecipeForFlags(argv []string, allowURL bool) (data []byte, format string, source string) {
	path, detected := resolveTaskFileFromArgs(argv)
	if path == taskFileStdin && isatty.IsTerminal(os.Stdin.Fd()) {
		return nil, "", path
	}
	data, err := readRecipeBytes(path, allowURL)
	if err != nil {
		return nil, "", path
	}
	// The override is read straight from argv: pflag has not run, so
	// the command's flag field is still empty. An unrecognised value is
	// ignored here and rejected properly by parseTasksFormatFlag in Run.
	override, _ := parseTasksFormatFlag(tasksFormatFromArgs(argv))
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

// tasksFormatAutocomplete offers the two canonical --tasks-format
// values. The yml / json aliases parseTasksFormatFlag also accepts are
// deliberately left out so completion suggests one spelling per format.
func tasksFormatAutocomplete() complete.Predictor {
	return complete.PredictSet(taskFileFormatYAML, taskFileFormatJSON5)
}

// taskFileAutocomplete is the file-completion predictor shared by the
// --tasks / --output / --vars-output flags and `docket fmt`'s positional
// argument across apply / plan / validate / fmt / init / export.
func taskFileAutocomplete() complete.Predictor {
	return predictFilesByExtension(taskFileExtensions)
}
