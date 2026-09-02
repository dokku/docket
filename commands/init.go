package commands

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
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
)

// InitCommand scaffolds a starter tasks.yml from an embedded template.
//
// init is offline by contract: it never opens a subprocess and never
// contacts the Dokku server. All defaults are derived from the working
// directory (cwd basename for --name, ./.git/config for --repo).
type InitCommand struct {
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

	output string
	// formatFlag is the raw --format value; it is normalised by
	// parseRecipeFormatFlag in Run and then overrides whatever the
	// --output extension would have implied.
	formatFlag string
	name       string
	repo       string
	force      bool
	minimal    bool
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
		"Scaffold tasks.yml using cwd defaults":  fmt.Sprintf("%s %s", appName, c.Name()),
		"Scaffold a JSON5 tasks.json instead":    fmt.Sprintf("%s %s --format json5", appName, c.Name()),
		"Write a minimal one-task scaffold":      fmt.Sprintf("%s %s --minimal", appName, c.Name()),
		"Override the play and app name":         fmt.Sprintf("%s %s --name web", appName, c.Name()),
		"Override the git repository URL":        fmt.Sprintf("%s %s --repo git@example.com:owner/repo.git", appName, c.Name()),
		"Write to a specific path":               fmt.Sprintf("%s %s --output path/to/tasks.yml", appName, c.Name()),
		"Stream the rendered scaffold to stdout": fmt.Sprintf("%s %s --output -", appName, c.Name()),
		"Stream a JSON5 scaffold to stdout":      fmt.Sprintf("%s %s --output - --format json5", appName, c.Name()),
		"Overwrite an existing file":             fmt.Sprintf("%s %s --force", appName, c.Name()),
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
	f.StringVar(&c.output, "output", defaultRecipeOutput, "path to write the scaffold to; pass - to write to stdout")
	f.StringVar(&c.formatFlag, "format", "", "write the scaffold as this format ("+recipeFormatList()+") instead of inferring it from the --output extension. Without an explicit --output, json5 writes "+defaultRecipeOutputFor(tasks.FormatNameJSON5)+"; this is also the only way to get JSON5 on stdout.")
	f.BoolVar(&c.force, "force", false, "overwrite an existing output file")
	f.BoolVar(&c.minimal, "minimal", false, "emit a minimal one-task scaffold without an inputs block")
	f.StringVar(&c.name, "name", defaultName(c.baseDir()), "play name and default app input value")
	f.StringVar(&c.repo, "repo", defaultRepo(c.baseDir()), "git repository URL used as the default for the repo input")
	return f
}

func (c *InitCommand) AutocompleteFlags() complete.Flags {
	return command.MergeAutocompleteFlags(
		c.Meta.AutocompleteFlags(command.FlagSetClient),
		complete.Flags{
			"--output":  taskFileAutocomplete(),
			"--format":  recipeFormatAutocomplete(),
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
//	1 - flag parse error, --force combined with --output -, output file
//	    already exists without --force, template render error, IO error
func (c *InitCommand) Run(args []string) int {
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

	// Resolve the write target before anything else looks at it. With
	// --format json5 and no explicit --output the default path becomes
	// tasks.json, and the exists / --force check below has to stat that
	// path, not the tasks.yml it would otherwise have defaulted to.
	// flags.Changed is only meaningful after flags.Parse.
	var format string
	c.output, format = resolveRecipeOutput(c.output, formatOverride, flags.Changed("output"))
	if msg := recipeOutputFormatMismatch(c.output, formatOverride); msg != "" {
		c.Ui.Warn(msg)
	}

	// --force only means "replace the file that is there", and the exists
	// check below is skipped entirely when streaming, so the flag would
	// otherwise be read and dropped - the same silence #419 reports on
	// export.
	if err := stdoutInertFlagError(flags, c.output, []stdoutInertFlag{
		{name: "force", reason: "a streamed recipe writes no files"},
	}); err != nil {
		c.Ui.Error(err.Error())
		return 1
	}

	toStdout := c.output == taskFileStdin

	if !toStdout {
		if _, err := os.Stat(inDir(c.baseDir(), c.output)); err == nil {
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
		Format:  format,
	})
	if err != nil {
		c.Ui.Error(err.Error())
		return 1
	}

	// Parse-check the rendered scaffold before writing it anywhere, so a
	// scaffold that fails to parse is never left on disk (or streamed to
	// stdout as a broken file).
	taskCount, playCount, err := countTasks(rendered, format)
	if err != nil {
		c.Ui.Error(fmt.Sprintf("internal error: rendered scaffold did not parse: %v", err))
		return 1
	}

	if toStdout {
		if _, err := c.stdout().Write(rendered); err != nil {
			c.Ui.Error(fmt.Sprintf("write error: %v", err))
			return 1
		}
		return 0
	}

	if err := os.WriteFile(inDir(c.baseDir(), c.output), rendered, 0o644); err != nil {
		c.Ui.Error(fmt.Sprintf("write error: %v", err))
		return 1
	}

	// The commands below run with no --tasks, so they probe
	// defaultTaskFileCandidates and take the first that exists - which
	// describes the scaffold only when it landed on that first candidate.
	// Anywhere else (tasks.json from --format json5, an --output in
	// another directory) the block has to name the file, or it quietly
	// tells the reader to inspect a stale tasks.yml instead (#420).
	//
	// The suffix is spliced in ahead of the padding rather than appended,
	// so it is the same width on all three lines and the comments stay
	// lined up without recomputing the column.
	tasksArg := ""
	if filepath.Clean(c.output) != defaultTaskFileCandidates[0] {
		tasksArg = " --tasks " + shellQuotePath(c.output)
	}

	c.Ui.Output(fmt.Sprintf("==> Created %s (%s, %s)", c.output, pluralize(taskCount, "task"), pluralize(playCount, "play")))
	c.Ui.Output("")
	c.Ui.Output("Next steps:")
	c.Ui.Output(fmt.Sprintf("  $ %s validate%s          # offline check", appName(), tasksArg))
	c.Ui.Output(fmt.Sprintf("  $ %s plan%s              # preview against the server", appName(), tasksArg))
	c.Ui.Output(fmt.Sprintf("  $ %s apply%s             # apply", appName(), tasksArg))
	return 0
}

// initOptions is the substitution data passed to the embedded templates.
type initOptions struct {
	Name    string
	Repo    string
	Minimal bool
	// Format selects the on-disk shape of the rendered scaffold: any
	// canonical codec name. The empty string resolves to the default
	// codec, so existing callers (and tests that drive renderInit
	// directly) keep their behaviour.
	Format string
}

// renderInit reads the right embedded template, parses it with custom
// delimiters so sigil syntax in the body survives untouched, and returns
// the rendered bytes. Anything a format needs at the top of the document -
// the YAML `---` marker, the JSON5 opening bracket - lives in that
// format's own templates rather than in a branch here.
//
// Exposed at package scope so unit tests can drive it directly without
// going through the cli-skeleton UI plumbing.
func renderInit(opts initOptions) ([]byte, error) {
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = "app"
	}

	templateName := selectInitTemplate(opts.Format, opts.Minimal)

	raw, err := templates.FS.ReadFile(templateName)
	if err != nil {
		return nil, fmt.Errorf("read template %s: %w", templateName, err)
	}

	// yamlStr / jsonStr emit a value as a correctly quoted-and-escaped
	// scalar so a name with YAML- or JSON-special characters (@web,
	// "foo: bar", an embedded quote) produces a valid scaffold instead of
	// a broken one. yamlStr leaves simple names unquoted, matching the
	// previous output for ordinary names.
	tmpl, err := template.New(templateName).Delims("<<", ">>").Funcs(template.FuncMap{
		"yamlStr": tasks.YAMLScalar,
		"jsonStr": tasks.JSONScalar,
	}).Parse(string(raw))
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

	return body.Bytes(), nil
}

// selectInitTemplate returns the embedded template name for (format,
// minimal). The name is derived from the codec, so a format is scaffolded
// by dropping default.<name>.tmpl and minimal.<name>.tmpl next to the
// others - there is no branch to extend. An unknown format resolves to the
// default codec and gets its templates.
//
// The embedded FS is not checked here; a missing template surfaces as the
// read error in renderInit. TestEveryCodecHasInitTemplates is what stops a
// codec from shipping without one.
func selectInitTemplate(format string, minimal bool) string {
	base := "default"
	if minimal {
		base = "minimal"
	}
	return fmt.Sprintf("%s.%s.tmpl", base, tasks.CodecFor(format).Name())
}

// defaultName returns the basename of dir, or "app" if it cannot be derived to
// a usable name. An empty dir means the process working directory, which is
// what `docket init` in a project folder gets.
func defaultName(dir string) string {
	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return "app"
		}
		dir = cwd
	}
	base := filepath.Base(dir)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return "app"
	}
	return base
}

// defaultRepo reads dir's .git/config and returns the value of the `url` key
// inside the `[remote "origin"]` section. Returns "" when the file does
// not exist, when there is no origin section, or on any parse error.
func defaultRepo(dir string) string {
	f, err := os.Open(inDir(dir, filepath.Join(".git", "config")))
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

// countTasks parses rendered scaffold bytes and returns the total
// number of tasks across all plays plus the play count. Used for the
// "==> Created tasks.yml (N tasks, M plays)" summary line. format
// selects the parser; the empty string defaults to YAML.
func countTasks(data []byte, format string) (int, int, error) {
	recipe, err := tasks.UnmarshalRecipe(data, format)
	if err != nil {
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

// stdout returns the writer this command streams bytes to.
func (c *InitCommand) stdout() io.Writer { return commandStdout(c.Stdout) }

// baseDir returns the directory this command resolves relative paths against.
func (c *InitCommand) baseDir() string { return c.BaseDir }
