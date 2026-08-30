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
	// formatFlag is the raw --format value; it is normalised by
	// parseRecipeFormatFlag in Run and then governs both the recipe and
	// the companion vars-file, overriding either output's extension.
	formatFlag string
	overwrite  bool
	redact     bool
	apps       []string
	resources  []string

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
		"Stream a JSON5 recipe to stdout":                       fmt.Sprintf("%s %s --output - --format json5", appName, c.Name()),
		"Redact secrets into a fill-in-the-blanks vars-file":    fmt.Sprintf("%s %s --redact", appName, c.Name()),
		"Export only a single app":                              fmt.Sprintf("%s %s --app my-app", appName, c.Name()),
		"Export one resource":                                   fmt.Sprintf("%s %s --resource 'dokku_config[app=my-app]'", appName, c.Name()),
		"Export every app's domains":                            fmt.Sprintf("%s %s --resource dokku_domains", appName, c.Name()),
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
	f.StringVar(&c.output, "output", defaultRecipeOutput, "path to write the recipe to; pass - to stream a self-contained recipe to stdout")
	f.StringVar(&c.formatFlag, "format", "", "write the recipe and vars-file as this format (yaml or json5) instead of inferring it from the --output extension. Without an explicit --output, json5 writes "+defaultRecipeOutputJSON5+"; this is also the only way to get JSON5 on stdout.")
	f.StringVar(&c.varsOutput, "vars-output", "", "path to write the companion vars-file to (defaults to <output-base>.vars.<ext>; --format overrides its format)")
	f.BoolVar(&c.overwrite, "overwrite", false, "overwrite existing output files without prompting")
	f.BoolVar(&c.redact, "redact", false, "write placeholder values into the vars-file instead of real secrets")
	f.StringArrayVar(&c.apps, "app", nil, "restrict the export to the named app (repeatable)")
	f.StringArrayVar(&c.resources, "resource", nil, "restrict the export to the named resource address, e.g. 'dokku_config[app=my-app]' (repeatable); a bare task type exports every resource of that type")
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
			"--format":               recipeFormatAutocomplete(),
			"--vars-output":          taskFileAutocomplete(),
			"--overwrite":            complete.PredictNothing,
			"--redact":               complete.PredictNothing,
			"--app":                  complete.PredictNothing,
			"--resource":             complete.PredictNothing,
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
//	1 - flag parse error, a file-only flag combined with --output -, read
//	    error, an output file exists without --overwrite (and the prompt was
//	    declined or stdin is not interactive), or an IO error
func (c *ExportCommand) Run(args []string) int {
	flags := c.FlagSet()
	flags.Usage = func() { c.Ui.Output(c.Help()) }
	if err := flags.Parse(args); err != nil {
		c.Ui.Error(err.Error())
		c.Ui.Error(command.CommandErrorText(c))
		return 1
	}

	formatOverride, err := parseRecipeFormatFlag("--format", c.formatFlag)
	if err != nil {
		c.Ui.Error(err.Error())
		return 1
	}

	// An address already names the app it belongs to, so combining the two
	// filters can only express a contradiction or a redundancy.
	if len(c.resources) > 0 && len(c.apps) > 0 {
		c.Ui.Error("--resource and --app cannot be combined; a resource address already names its app")
		return 1
	}

	// Addresses are parsed and checked against the registry here, before the
	// SSH control master is opened, so a typo fails instantly rather than
	// after a round trip to the server.
	resources, err := tasks.ParseResourceSelectors(c.resources)
	if err != nil {
		c.Ui.Error(err.Error())
		return 1
	}

	// Resolve the write target up front. --format json5 with no explicit
	// --output moves the default to tasks.json (and, through
	// deriveVarsOutput, tasks.vars.json), and every later use of c.output
	// - the overwrite prompt, the write, the summary, the Next steps line
	// - has to agree on one path. flags.Changed is only meaningful after
	// flags.Parse. Validating here also means a typo'd --format fails
	// before an SSH control master is opened or the server is read.
	var recipeFormat string
	c.output, recipeFormat = resolveRecipeOutput(c.output, formatOverride, flags.Changed("output"))
	if msg := recipeOutputFormatMismatch(c.output, formatOverride); msg != "" {
		c.Ui.Warn(msg)
	}

	// A streamed recipe has no vars-file to place and no file on disk to
	// replace, so both flags below would be silently dropped by the stdout
	// branch further down (#419). Rejected here, before the SSH control
	// master is opened and before the server is enumerated, so the failure
	// is instant.
	if err := stdoutInertFlagError(flags, c.output, []stdoutInertFlag{
		{name: "vars-output", reason: "a streamed recipe inlines its values"},
		{name: "overwrite", reason: "a streamed recipe writes no files"},
	}); err != nil {
		c.Ui.Error(err.Error())
		return 1
	}

	resolvedHost := resolveSshFlags(c.host, c.sudo, c.acceptNewHostKeys)
	if resolvedHost != "" {
		defer subprocess.CloseSshControlMaster(resolvedHost)
	}

	toStdout := c.output == taskFileStdin

	res, err := tasks.ExportRecipe(tasks.ExportOptions{
		Apps:      c.apps,
		Resources: resources,
		Redact:    c.redact,
		Inline:    toStdout,
	})
	// Export is the one server-reading command with no recipe to collect a
	// sensitive set from before the run: the values it must mask are the ones
	// its own exporters just read back. Registered here, ahead of the failure
	// below as well as the warnings, because the global play is exported
	// before the app list is read - so a run that dies on apps:list can
	// already be holding a secret (#488).
	//
	// What masks from here on is every diagnostic built from what the server
	// returned: the warnings, this failure, and the marshal errors further
	// down. What deliberately does not is the text built from the user's own
	// arguments - the --app names and --resource addresses reported missing,
	// and the output paths - because a name masked down to *** would hide the
	// typo the message exists to report.
	subprocess.SetGlobalSensitive(res.SensitiveValues())
	defer subprocess.SetGlobalSensitive(nil)

	if err != nil {
		c.Ui.Error(fmt.Sprintf("export failed: %v", subprocess.MaskString(err.Error())))
		return 1
	}
	for _, w := range res.Report.Warnings {
		c.Ui.Warn(fmt.Sprintf("warning: %s", subprocess.MaskString(w)))
	}

	// A nonexistent --app must not silently produce an empty recipe (which the
	// loader then rejects). When nothing was collected, abort without writing;
	// otherwise the existing apps are exported and the missing names are reported
	// with a non-zero exit at the end (#346). The names print unmasked here for
	// the reason exitForMissingApps gives.
	if res.PlayCount() == 0 {
		switch {
		case len(res.Report.MissingApps) > 0:
			c.Ui.Error(fmt.Sprintf("error: %s not found on server; nothing to export", strings.Join(res.Report.MissingApps, ", ")))
		case len(res.Report.MissingResources) > 0:
			c.Ui.Error(fmt.Sprintf("error: %s not found on server; nothing to export", strings.Join(res.Report.MissingResources, ", ")))
		default:
			c.Ui.Error("error: nothing to export")
		}
		return 1
	}

	recipeBytes, err := res.MarshalRecipe(recipeFormat)
	if err != nil {
		c.Ui.Error(fmt.Sprintf("marshal recipe: %v", subprocess.MaskString(err.Error())))
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
	// --format governs the pair: when it is given, the vars-file matches
	// the recipe even if --vars-output names another extension. Without
	// it the vars-file keeps following its own extension, so
	// `--output tasks.yml --vars-output vars.json` still writes JSON.
	varsFormat := formatOverride
	if varsFormat == "" {
		varsFormat = detectTaskFileFormat(varsOutput)
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
		varsBytes, err := res.MarshalVars(varsFormat)
		if err != nil {
			c.Ui.Error(fmt.Sprintf("marshal vars: %v", subprocess.MaskString(err.Error())))
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
	} else if flags.Changed("vars-output") {
		// Asking for a vars-file at a named path and getting no file is the
		// other half of #419. Here the flag was legitimate - there was just
		// nothing sensitive to lift - so this is a note rather than an
		// error. The derived default stays quiet: a path the user did not
		// choose going unwritten is not news.
		c.Ui.Output(fmt.Sprintf("    no sensitive values to export; %s not written", varsOutput))
	}
	c.Ui.Output("")
	c.Ui.Output("Next steps:")
	// Quoted so a path with a space survives the copy-paste this line
	// exists for.
	if writeVars {
		c.Ui.Output(fmt.Sprintf("  $ %s apply --tasks %s --vars-file %s", appName(), shellQuotePath(c.output), shellQuotePath(varsOutput)))
	} else {
		c.Ui.Output(fmt.Sprintf("  $ %s apply --tasks %s", appName(), shellQuotePath(c.output)))
	}
	return c.exitForMissingApps(res)
}

// exitForMissingApps reports any --app names or --resource addresses that were
// not found on the server and returns a non-zero exit code, so a typo is
// surfaced even though what does exist was still exported (#346).
//
// The names print unmasked, unlike the warnings Run masks above. They are the
// user's own arguments echoed back rather than anything the server returned,
// and the whole message is "you asked for a name that is not there" - which a
// name masked down to *** would not say (#488).
func (c *ExportCommand) exitForMissingApps(res *tasks.ExportResult) int {
	missing := append(append([]string(nil), res.Report.MissingApps...), res.Report.MissingResources...)
	if len(missing) == 0 {
		return 0
	}
	c.Ui.Error(fmt.Sprintf("error: %s not found on server", strings.Join(missing, ", ")))
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
