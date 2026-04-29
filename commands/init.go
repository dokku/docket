package commands

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/dokku/docket/commands/templates"
	"github.com/dokku/docket/tasks"

	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/posener/complete"
	flag "github.com/spf13/pflag"
	yaml "gopkg.in/yaml.v3"
)

// InitCommand scaffolds a starter tasks.yml from an embedded template.
//
// init is offline by contract: it never opens a subprocess and never
// contacts the Dokku server. All defaults are derived from the working
// directory (cwd basename for --name, ./.git/config for --repo).
type InitCommand struct {
	command.Meta

	output  string
	name    string
	repo    string
	force   bool
	minimal bool
}

func (c *InitCommand) Name() string {
	return "init"
}

func (c *InitCommand) Synopsis() string {
	return "Scaffolds a starter tasks.yml from an embedded template"
}

func (c *InitCommand) Help() string {
	return command.CommandHelp(c)
}

func (c *InitCommand) Examples() map[string]string {
	appName := os.Getenv("CLI_APP_NAME")
	return map[string]string{
		"Scaffold tasks.yml using cwd defaults": fmt.Sprintf("%s %s", appName, c.Name()),
		"Write a minimal one-task scaffold":     fmt.Sprintf("%s %s --minimal", appName, c.Name()),
		"Override the play and app name":        fmt.Sprintf("%s %s --name web", appName, c.Name()),
		"Override the git repository URL":       fmt.Sprintf("%s %s --repo git@example.com:owner/repo.git", appName, c.Name()),
		"Write to a specific path":              fmt.Sprintf("%s %s --output path/to/tasks.yml", appName, c.Name()),
		"Stream the rendered YAML to stdout":    fmt.Sprintf("%s %s --output -", appName, c.Name()),
		"Overwrite an existing file":            fmt.Sprintf("%s %s --force", appName, c.Name()),
	}
}

func (c *InitCommand) Arguments() []command.Argument {
	return []command.Argument{}
}

func (c *InitCommand) AutocompleteArgs() complete.Predictor {
	return complete.PredictNothing
}

func (c *InitCommand) ParsedArguments(args []string) (map[string]command.Argument, error) {
	return command.ParseArguments(args, c.Arguments())
}

func (c *InitCommand) FlagSet() *flag.FlagSet {
	f := c.Meta.FlagSet(c.Name(), command.FlagSetClient)
	f.StringVar(&c.output, "output", "tasks.yml", "path to write the scaffold to; pass - to write to stdout")
	f.BoolVar(&c.force, "force", false, "overwrite an existing output file")
	f.BoolVar(&c.minimal, "minimal", false, "emit a minimal one-task scaffold without an inputs block")
	f.StringVar(&c.name, "name", defaultName(), "play name and default app input value")
	f.StringVar(&c.repo, "repo", defaultRepo(), "git repository URL used as the default for the repo input")
	return f
}

func (c *InitCommand) AutocompleteFlags() complete.Flags {
	return command.MergeAutocompleteFlags(
		c.Meta.AutocompleteFlags(command.FlagSetClient),
		complete.Flags{
			"--output":  complete.PredictFiles("*.yml"),
			"--force":   complete.PredictNothing,
			"--minimal": complete.PredictNothing,
			"--name":    complete.PredictNothing,
			"--repo":    complete.PredictNothing,
		},
	)
}

// Run renders the scaffold and writes it. Exit codes:
//
//	0 - scaffold written
//	1 - flag parse error, output file already exists without --force,
//	    template render error, IO error
func (c *InitCommand) Run(args []string) int {
	flags := c.FlagSet()
	flags.Usage = func() { c.Ui.Output(c.Help()) }
	if err := flags.Parse(args); err != nil {
		c.Ui.Error(err.Error())
		c.Ui.Error(command.CommandErrorText(c))
		return 1
	}

	toStdout := c.output == "-"

	if !toStdout {
		if _, err := os.Stat(c.output); err == nil {
			if !c.force {
				c.Ui.Error(fmt.Sprintf("file %s already exists; pass --force to overwrite", c.output))
				return 1
			}
		} else if !errors.Is(err, fs.ErrNotExist) {
			c.Ui.Error(fmt.Sprintf("stat error: %v", err))
			return 1
		}
	}

	rendered, err := renderInit(initOptions{
		Name:    c.name,
		Repo:    c.repo,
		Minimal: c.minimal,
	})
	if err != nil {
		c.Ui.Error(err.Error())
		return 1
	}

	if toStdout {
		if _, err := os.Stdout.Write(rendered); err != nil {
			c.Ui.Error(fmt.Sprintf("write error: %v", err))
			return 1
		}
		return 0
	}

	if err := os.WriteFile(c.output, rendered, 0o644); err != nil {
		c.Ui.Error(fmt.Sprintf("write error: %v", err))
		return 1
	}

	taskCount, playCount, err := countTasks(rendered)
	if err != nil {
		c.Ui.Error(fmt.Sprintf("internal error: rendered scaffold did not parse: %v", err))
		return 1
	}

	c.Ui.Output(fmt.Sprintf("==> Created %s (%s, %s)", c.output, pluralize(taskCount, "task"), pluralize(playCount, "play")))
	c.Ui.Output("")
	c.Ui.Output("Next steps:")
	c.Ui.Output(fmt.Sprintf("  $ %s validate          # offline check", appName()))
	c.Ui.Output(fmt.Sprintf("  $ %s plan              # preview against the server", appName()))
	c.Ui.Output(fmt.Sprintf("  $ %s apply             # apply", appName()))
	return 0
}

// initOptions is the substitution data passed to the embedded templates.
type initOptions struct {
	Name    string
	Repo    string
	Minimal bool
}

// renderInit reads the right embedded template, parses it with custom
// delimiters so sigil syntax in the body survives untouched, and returns
// the rendered bytes (including the leading `---\n` document marker).
//
// Exposed at package scope so unit tests can drive it directly without
// going through the cli-skeleton UI plumbing.
func renderInit(opts initOptions) ([]byte, error) {
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = "app"
	}

	templateName := "default.yml.tmpl"
	if opts.Minimal {
		templateName = "minimal.yml.tmpl"
	}

	raw, err := templates.FS.ReadFile(templateName)
	if err != nil {
		return nil, fmt.Errorf("read template %s: %w", templateName, err)
	}

	tmpl, err := template.New(templateName).Delims("<<", ">>").Parse(string(raw))
	if err != nil {
		return nil, fmt.Errorf("parse template %s: %w", templateName, err)
	}

	var body bytes.Buffer
	if err := tmpl.Execute(&body, struct {
		Name string
		Repo string
	}{Name: name, Repo: opts.Repo}); err != nil {
		return nil, fmt.Errorf("render template %s: %w", templateName, err)
	}

	var out bytes.Buffer
	out.WriteString("---\n")
	out.Write(body.Bytes())
	return out.Bytes(), nil
}

// defaultName returns the basename of the working directory, or "app" if
// the cwd cannot be derived to a usable name.
func defaultName() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "app"
	}
	base := filepath.Base(cwd)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "app"
	}
	return base
}

// defaultRepo reads ./.git/config and returns the value of the `url` key
// inside the `[remote "origin"]` section. Returns "" when the file does
// not exist, when there is no origin section, or on any parse error.
func defaultRepo() string {
	f, err := os.Open(".git/config")
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	inOrigin := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			inOrigin = strings.EqualFold(line, `[remote "origin"]`)
			continue
		}
		if !inOrigin {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) == "url" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// countTasks parses rendered YAML and returns the total number of tasks
// across all plays plus the play count. Used for the "==> Created
// tasks.yml (N tasks, M plays)" summary line.
func countTasks(data []byte) (int, int, error) {
	var recipe tasks.Recipe
	if err := yaml.Unmarshal(data, &recipe); err != nil {
		return 0, 0, err
	}
	total := 0
	for _, play := range recipe {
		total += len(play.Tasks)
	}
	return total, len(recipe), nil
}

func pluralize(n int, word string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

func appName() string {
	if name := os.Getenv("CLI_APP_NAME"); name != "" {
		return name
	}
	return "docket"
}
