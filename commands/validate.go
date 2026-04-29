package commands

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/dokku/docket/tasks"

	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/posener/complete"
	flag "github.com/spf13/pflag"
)

// ValidateCommand performs offline schema and template checks against a
// docket recipe without contacting a Dokku server.
type ValidateCommand struct {
	command.Meta

	tasksFile string
	json      bool
	strict    bool
	varsFiles []string
	arguments map[string]*Argument
}

func (c *ValidateCommand) Name() string {
	return "validate"
}

func (c *ValidateCommand) Synopsis() string {
	return "Performs offline schema and template checks on a docket task file"
}

func (c *ValidateCommand) Help() string {
	return command.CommandHelp(c)
}

func (c *ValidateCommand) Examples() map[string]string {
	appName := os.Getenv("CLI_APP_NAME")
	return map[string]string{
		"Validate the default tasks.yml":           fmt.Sprintf("%s %s", appName, c.Name()),
		"Validate a specific file":                 fmt.Sprintf("%s %s --tasks path/to/task.yml", appName, c.Name()),
		"Emit JSON-lines problem events":           fmt.Sprintf("%s %s --json", appName, c.Name()),
		"Flag required inputs without an override": fmt.Sprintf("%s %s --strict", appName, c.Name()),
	}
}

func (c *ValidateCommand) Arguments() []command.Argument {
	return []command.Argument{}
}

func (c *ValidateCommand) AutocompleteArgs() complete.Predictor {
	return complete.PredictNothing
}

func (c *ValidateCommand) ParsedArguments(args []string) (map[string]command.Argument, error) {
	return command.ParseArguments(args, c.Arguments())
}

func (c *ValidateCommand) FlagSet() *flag.FlagSet {
	f := c.Meta.FlagSet(c.Name(), command.FlagSetClient)
	f.StringVar(&c.tasksFile, "tasks", "tasks.yml", "a yaml file containing a task list")
	f.BoolVar(&c.json, "json", false, "emit one JSON-lines problem event per finding")
	f.BoolVar(&c.strict, "strict", false, "additionally flag required inputs that have no default and no CLI override")
	f.StringArrayVar(&c.varsFiles, "vars-file", nil, "load input values from a YAML or JSON file (repeatable; later files override earlier; CLI --name=value flags always win). A .json extension parses as JSON; otherwise YAML.")

	taskFile := getTaskYamlFilename(os.Args)
	data, err := os.ReadFile(taskFile)
	if err != nil {
		return f
	}

	arguments, err := registerInputFlags(f, data)
	if err != nil {
		return f
	}
	c.arguments = arguments

	return f
}

func (c *ValidateCommand) AutocompleteFlags() complete.Flags {
	return command.MergeAutocompleteFlags(
		c.Meta.AutocompleteFlags(command.FlagSetClient),
		complete.Flags{
			"--tasks":     complete.PredictFiles("*.yml"),
			"--json":      complete.PredictNothing,
			"--strict":    complete.PredictNothing,
			"--vars-file": complete.PredictFiles("*"),
		},
	)
}

// Run loads the tasks file and reports every problem the validator finds.
//
// Exit codes:
//
//	0 - no problems found
//	1 - file read failed, or the validator returned at least one problem
func (c *ValidateCommand) Run(args []string) int {
	flags := c.FlagSet()
	flags.Usage = func() { c.Ui.Output(c.Help()) }
	if err := flags.Parse(args); err != nil {
		c.Ui.Error(err.Error())
		c.Ui.Error(command.CommandErrorText(c))
		return 1
	}

	if _, err := applyVarsFiles(c.arguments, flags, c.varsFiles); err != nil {
		if c.json {
			c.emitJSONProblem(tasks.Problem{
				Code:    "vars_file_error",
				Message: err.Error(),
			})
		} else {
			c.Ui.Error(err.Error())
		}
		return 1
	}

	data, err := os.ReadFile(c.tasksFile)
	if err != nil {
		if c.json {
			c.emitJSONProblem(tasks.Problem{
				Code:    "read_error",
				Message: err.Error(),
			})
		} else {
			c.Ui.Error(fmt.Sprintf("read error: %v", err))
		}
		return 1
	}

	overrides := map[string]bool{}
	for name, argument := range c.arguments {
		if argument.HasValue() {
			overrides[name] = true
		}
	}

	problems := tasks.Validate(data, tasks.ValidateOptions{
		Strict:         c.strict,
		InputOverrides: overrides,
	})

	if c.json {
		for _, p := range problems {
			c.emitJSONProblem(p)
		}
		if len(problems) > 0 {
			return 1
		}
		return 0
	}

	if len(problems) == 0 {
		c.Ui.Info(fmt.Sprintf("==> Validating %s", c.tasksFile))
		c.Ui.Info("")
		c.Ui.Info(fmt.Sprintf("[ok]      %s is valid", c.tasksFile))
		return 0
	}

	c.renderHumanProblems(problems)
	return 1
}

// emitJSONProblem prints a single JSON-lines event. The version field is
// pinned at 1 so consumers can branch on schema changes.
func (c *ValidateCommand) emitJSONProblem(p tasks.Problem) {
	event := map[string]interface{}{
		"version": 1,
		"type":    "validate_problem",
		"code":    p.Code,
		"message": p.Message,
	}
	if p.Play != "" {
		event["play"] = p.Play
	}
	if p.Task != "" {
		event["task"] = p.Task
	}
	if p.Line > 0 {
		event["line"] = p.Line
	}
	if p.Column > 0 {
		event["column"] = p.Column
	}
	if p.Hint != "" {
		event["hint"] = p.Hint
	}
	b, err := json.Marshal(event)
	if err != nil {
		c.Ui.Error(fmt.Sprintf("json marshal error: %v", err))
		return
	}
	c.Ui.Output(string(b))
}

// renderHumanProblems prints problems grouped by play, mirroring the issue's
// example output. Play and task headers are emitted only when they change so
// the output stays compact.
func (c *ValidateCommand) renderHumanProblems(problems []tasks.Problem) {
	c.Ui.Info(fmt.Sprintf("==> Validating %s", c.tasksFile))
	c.Ui.Info("")
	c.Ui.Info(fmt.Sprintf("[error]   %d problem(s):", len(problems)))
	c.Ui.Info("")

	grouped := map[string][]tasks.Problem{}
	playOrder := []string{}
	for _, p := range problems {
		key := p.Play
		if _, ok := grouped[key]; !ok {
			playOrder = append(playOrder, key)
		}
		grouped[key] = append(grouped[key], p)
	}
	sort.SliceStable(playOrder, func(i, j int) bool {
		return playOrder[i] < playOrder[j]
	})

	for _, play := range playOrder {
		if play != "" {
			c.Ui.Info(fmt.Sprintf("  %s", play))
		}
		for _, p := range grouped[play] {
			c.Ui.Info(fmt.Sprintf("    ! %s", formatProblem(p)))
		}
		c.Ui.Info("")
	}
}

func formatProblem(p tasks.Problem) string {
	var b strings.Builder
	if p.Task != "" {
		b.WriteString(p.Task)
		if p.Line > 0 {
			fmt.Fprintf(&b, " (line %d", p.Line)
			if p.Column > 0 {
				fmt.Fprintf(&b, ":%d", p.Column)
			}
			b.WriteString(")")
		}
		b.WriteString(": ")
	} else if p.Line > 0 {
		fmt.Fprintf(&b, "line %d", p.Line)
		if p.Column > 0 {
			fmt.Fprintf(&b, ":%d", p.Column)
		}
		b.WriteString(": ")
	}
	b.WriteString(p.Message)
	if p.Hint != "" {
		fmt.Fprintf(&b, " - %s", p.Hint)
	}
	return b.String()
}
