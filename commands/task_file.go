package commands

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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
// pre-#218 behaviour. HTTP URLs and other non-filesystem paths flow
// through filepath.Ext just fine because they still carry an extension.
func detectTaskFileFormat(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".json", ".json5":
		return taskFileFormatJSON5
	default:
		return taskFileFormatYAML
	}
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

// taskFileAutocompleteGlob is the shared file glob for the --tasks flag
// completion across apply / plan / validate / fmt / init.
const taskFileAutocompleteGlob = "*.{yml,yaml,json,json5}"
