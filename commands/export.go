package commands

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/dokku/docket/subprocess"
	"github.com/dokku/docket/tasks"

	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/posener/complete"
	flag "github.com/spf13/pflag"
)

// ExportCommand reads a live Dokku server and writes a recipe describing it -
// the inverse of apply. Sensitive values are lifted into a companion vars-file
// that the emitted recipe references through inputs, so the pair applies with
// `docket apply --vars-file <vars>`.
type ExportCommand struct {
	command.Meta

	output     string
	varsOutput string
	overwrite  bool
	redact     bool
	apps       []string

	host              string
	sudo              bool
	acceptNewHostKeys bool
}

func (c *ExportCommand) Name() string {
	return "export"
}

func (c *ExportCommand) Synopsis() string {
	return "Reads a live server and writes a recipe describing it"
}

func (c *ExportCommand) Help() string {
	return command.CommandHelp(c)
}

func (c *ExportCommand) Examples() map[string]string {
	appName := os.Getenv("CLI_APP_NAME")
	return map[string]string{
		"Export the local server to tasks.yml + tasks.vars.yml": fmt.Sprintf("%s %s", appName, c.Name()),
		"Export a remote server over SSH":                       fmt.Sprintf("%s %s --host deploy@dokku.example.com", appName, c.Name()),
		"Stream a self-contained recipe to stdout":              fmt.Sprintf("%s %s --output -", appName, c.Name()),
		"Redact secrets into a fill-in-the-blanks vars-file":    fmt.Sprintf("%s %s --redact", appName, c.Name()),
		"Export only a single app":                              fmt.Sprintf("%s %s --app my-app", appName, c.Name()),
	}
}

func (c *ExportCommand) Arguments() []command.Argument {
	return []command.Argument{}
}

func (c *ExportCommand) AutocompleteArgs() complete.Predictor {
	return complete.PredictNothing
}

func (c *ExportCommand) ParsedArguments(args []string) (map[string]command.Argument, error) {
	return command.ParseArguments(args, c.Arguments())
}

func (c *ExportCommand) FlagSet() *flag.FlagSet {
	f := c.Meta.FlagSet(c.Name(), command.FlagSetClient)
	f.StringVar(&c.output, "output", "tasks.yml", "path to write the recipe to; pass - to stream a self-contained recipe to stdout")
	f.StringVar(&c.varsOutput, "vars-output", "", "path to write the companion vars-file to (defaults to <output-base>.vars.<ext>)")
	f.BoolVar(&c.overwrite, "overwrite", false, "overwrite existing output files without prompting")
	f.BoolVar(&c.redact, "redact", false, "write placeholder values into the vars-file instead of real secrets")
	f.StringArrayVar(&c.apps, "app", nil, "restrict the export to the named app (repeatable)")
	f.StringVar(&c.host, "host", "", "remote [user@]host[:port] to read over SSH; overrides DOKKU_HOST")
	f.BoolVar(&c.sudo, "sudo", false, "wrap the remote dokku call in sudo -n")
	f.BoolVar(&c.acceptNewHostKeys, "accept-new-host-keys", false, "trust an unknown SSH host key on first connect")
	return f
}

func (c *ExportCommand) AutocompleteFlags() complete.Flags {
	return command.MergeAutocompleteFlags(
		c.Meta.AutocompleteFlags(command.FlagSetClient),
		complete.Flags{
			"--output":               taskFileAutocomplete(),
			"--vars-output":          taskFileAutocomplete(),
			"--overwrite":            complete.PredictNothing,
			"--redact":               complete.PredictNothing,
			"--app":                  complete.PredictNothing,
			"--host":                 complete.PredictNothing,
			"--sudo":                 complete.PredictNothing,
			"--accept-new-host-keys": complete.PredictNothing,
		},
	)
}

// Run reads the server, marshals the recipe (and vars-file), and writes them.
// Exit codes:
//
//	0 - export written (or streamed to stdout)
//	1 - flag parse error, read error, an output file exists without
//	    --overwrite (and the prompt was declined or stdin is not interactive),
//	    or an IO error
func (c *ExportCommand) Run(args []string) int {
	flags := c.FlagSet()
	flags.Usage = func() { c.Ui.Output(c.Help()) }
	if err := flags.Parse(args); err != nil {
		c.Ui.Error(err.Error())
		c.Ui.Error(command.CommandErrorText(c))
		return 1
	}

	resolvedHost := resolveSshFlags(c.host, c.sudo, c.acceptNewHostKeys)
	if resolvedHost != "" {
		defer subprocess.CloseSshControlMaster(resolvedHost)
	}

	toStdout := c.output == "-"

	res, err := tasks.ExportRecipe(tasks.ExportOptions{
		Apps:   c.apps,
		Redact: c.redact,
		Inline: toStdout,
	})
	if err != nil {
		c.Ui.Error(fmt.Sprintf("export failed: %v", err))
		return 1
	}
	for _, w := range res.Report.Warnings {
		c.Ui.Warn(fmt.Sprintf("warning: %s", w))
	}

	// A nonexistent --app must not silently produce an empty recipe (which the
	// loader then rejects). When nothing was collected, abort without writing;
	// otherwise the existing apps are exported and the missing names are reported
	// with a non-zero exit at the end (#346).
	if res.PlayCount() == 0 {
		if len(res.Report.MissingApps) > 0 {
			c.Ui.Error(fmt.Sprintf("error: %s not found on server; nothing to export", strings.Join(res.Report.MissingApps, ", ")))
		} else {
			c.Ui.Error("error: nothing to export")
		}
		return 1
	}

	recipeFormat := taskFileFormatYAML
	if !toStdout {
		recipeFormat = detectTaskFileFormat(c.output)
	}
	recipeBytes, err := res.MarshalRecipe(recipeFormat)
	if err != nil {
		c.Ui.Error(fmt.Sprintf("marshal recipe: %v", err))
		return 1
	}

	if toStdout {
		if _, err := os.Stdout.Write(recipeBytes); err != nil {
			c.Ui.Error(fmt.Sprintf("write error: %v", err))
			return 1
		}
		return c.exitForMissingApps(res)
	}

	varsOutput := c.varsOutput
	if varsOutput == "" {
		varsOutput = deriveVarsOutput(c.output)
	}
	writeVars := res.HasVars()

	// Overwrite check: both files are checked before either is written, so a
	// declined prompt aborts the whole export with nothing written.
	if !c.overwrite {
		targets := []string{c.output}
		if writeVars {
			targets = append(targets, varsOutput)
		}
		for _, path := range targets {
			exists, err := pathExists(path)
			if err != nil {
				c.Ui.Error(fmt.Sprintf("stat error: %v", err))
				return 1
			}
			if !exists {
				continue
			}
			ok, err := c.confirmOverwrite(path)
			if err != nil {
				c.Ui.Error(err.Error())
				return 1
			}
			if !ok {
				c.Ui.Output("aborted; no files written")
				return 1
			}
		}
	}

	if err := os.WriteFile(c.output, recipeBytes, 0o644); err != nil {
		c.Ui.Error(fmt.Sprintf("write error: %v", err))
		return 1
	}
	if writeVars {
		varsBytes, err := res.MarshalVars(detectTaskFileFormat(varsOutput))
		if err != nil {
			c.Ui.Error(fmt.Sprintf("marshal vars: %v", err))
			return 1
		}
		if err := os.WriteFile(varsOutput, varsBytes, 0o644); err != nil {
			c.Ui.Error(fmt.Sprintf("write error: %v", err))
			return 1
		}
	}

	c.Ui.Output(fmt.Sprintf("==> Exported %s (%s)", c.output, pluralize(res.AppCount(), "app")))
	if writeVars {
		c.Ui.Output(fmt.Sprintf("    values written to %s", varsOutput))
		if c.redact {
			c.Ui.Output("    (redacted; fill in the vars-file before applying)")
		}
	}
	c.Ui.Output("")
	c.Ui.Output("Next steps:")
	if writeVars {
		c.Ui.Output(fmt.Sprintf("  $ %s apply --tasks %s --vars-file %s", appName(), c.output, varsOutput))
	} else {
		c.Ui.Output(fmt.Sprintf("  $ %s apply --tasks %s", appName(), c.output))
	}
	return c.exitForMissingApps(res)
}

// exitForMissingApps reports any --app names that were not found on the server
// and returns a non-zero exit code, so a typo is surfaced even though the apps
// that do exist were still exported (#346).
func (c *ExportCommand) exitForMissingApps(res *tasks.ExportResult) int {
	if len(res.Report.MissingApps) == 0 {
		return 0
	}
	c.Ui.Error(fmt.Sprintf("error: %s not found on server", strings.Join(res.Report.MissingApps, ", ")))
	return 1
}

// confirmOverwrite prompts for permission to overwrite an existing file. When
// stdin is not interactive (Ask returns an error, e.g. EOF), it returns an
// error advising --overwrite rather than silently overwriting.
func (c *ExportCommand) confirmOverwrite(path string) (bool, error) {
	answer, err := c.Ui.Ask(fmt.Sprintf("%s already exists; overwrite? [y/N]", path))
	if err != nil {
		return false, fmt.Errorf("%s already exists; pass --overwrite to replace it", path)
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	return answer == "y" || answer == "yes", nil
}

// deriveVarsOutput returns the default companion vars-file path for a recipe
// output path: <base>.vars.<ext> (e.g. tasks.yml -> tasks.vars.yml).
func deriveVarsOutput(output string) string {
	ext := filepath.Ext(output)
	if ext == "" {
		return output + ".vars"
	}
	return strings.TrimSuffix(output, ext) + ".vars" + ext
}

// pathExists reports whether path exists, distinguishing a genuine stat error
// from a not-found.
func pathExists(path string) (bool, error) {
	if _, err := os.Stat(path); err == nil {
		return true, nil
	} else if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	} else {
		return false, err
	}
}
