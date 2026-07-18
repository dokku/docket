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

// defaultTaskFileCandidates is the ordered list of filenames probed when
// --tasks is not given. The first one that exists in the working
// directory is used. The order matches the legacy default (tasks.yml)
// so behaviour does not change for existing recipes; .yaml and .json
// fall through to give JSON-native users a no-config setup.
var defaultTaskFileCandidates = []string{"tasks.yml", "tasks.yaml", "tasks.json"}

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

// readTaskFileData returns the bytes of the recipe at path. An http(s) URL
// is fetched over HTTP (apply and plan advertise a remote --tasks URL in
// their help); any other value is read from disk so the familiar
// os.ReadFile "no such file" error still surfaces for a mistyped path.
func readTaskFileData(path string) ([]byte, error) {
	if isTaskFileURL(path) {
		return fetchTaskFileURL(path)
	}
	return os.ReadFile(path)
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

// taskFileAutocomplete is the file-completion predictor shared by the
// --tasks / --output / --vars-output flags and `docket fmt`'s positional
// argument across apply / plan / validate / fmt / init / export.
func taskFileAutocomplete() complete.Predictor {
	return predictFilesByExtension(taskFileExtensions)
}
