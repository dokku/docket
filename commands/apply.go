package commands

import (
	"fmt"
	"os"
	"time"

	"github.com/dokku/docket/subprocess"
	"github.com/dokku/docket/tasks"

	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/posener/complete"
	flag "github.com/spf13/pflag"
)

type ApplyCommand struct {
	command.Meta

	tasksFile         string
	verbose           bool
	json              bool
	host              string
	sudo              bool
	acceptNewHostKeys bool
	tags              []string
	skipTags          []string
	arguments         map[string]*Argument
}

func (c *ApplyCommand) Name() string {
	return "apply"
}

func (c *ApplyCommand) Synopsis() string {
	return "Applies a docket task file"
}

func (c *ApplyCommand) Help() string {
	return command.CommandHelp(c)
}

func (c *ApplyCommand) Examples() map[string]string {
	appName := os.Getenv("CLI_APP_NAME")
	return map[string]string{
		"Apply tasks from the default tasks.yml": fmt.Sprintf("%s %s", appName, c.Name()),
		"Apply tasks from a specific file":       fmt.Sprintf("%s %s --tasks path/to/task.yml", appName, c.Name()),
		"Apply tasks from a remote URL":          fmt.Sprintf("%s %s --tasks http://dokku.com/docket/example.yml", appName, c.Name()),
		"Override a task input":                  fmt.Sprintf("%s %s --name lollipop", appName, c.Name()),
	}
}

func (c *ApplyCommand) Arguments() []command.Argument {
	return []command.Argument{}
}

func (c *ApplyCommand) AutocompleteArgs() complete.Predictor {
	return complete.PredictNothing
}

func (c *ApplyCommand) ParsedArguments(args []string) (map[string]command.Argument, error) {
	return command.ParseArguments(args, c.Arguments())
}

func (c *ApplyCommand) FlagSet() *flag.FlagSet {
	f := c.Meta.FlagSet(c.Name(), command.FlagSetClient)
	f.StringVar(&c.tasksFile, "tasks", "tasks.yml", "a yaml file containing a task list")
	f.BoolVar(&c.verbose, "verbose", false, "echo the resolved dokku command for each task as a continuation line. Values from inputs declared `sensitive: true` and from task struct fields tagged `sensitive:\"true\"` are masked as `***`. Ignored when --json is set; the JSON output already includes the resolved commands.")
	f.BoolVar(&c.json, "json", false, "emit one JSON-lines event per play/task/summary instead of human-readable output. Schema is keyed by `version: 1`; sensitive values mask to `***`.")
	f.StringVar(&c.host, "host", "", "remote dokku host as [user@]host[:port]; equivalent to DOKKU_HOST. Routes every dokku invocation through ssh.")
	f.BoolVar(&c.sudo, "sudo", false, "wrap remote dokku invocations with `sudo -n`; equivalent to DOKKU_SUDO=1")
	f.BoolVar(&c.acceptNewHostKeys, "accept-new-host-keys", false, "for SSH transport, accept new host keys on first connection (`-o StrictHostKeyChecking=accept-new`). MITM risk on first connect.")
	f.StringSliceVar(&c.tags, "tags", nil, "comma-separated tag list; only tasks whose `tags:` set intersects this list run")
	f.StringSliceVar(&c.skipTags, "skip-tags", nil, "comma-separated tag list; tasks whose `tags:` set intersects this list are skipped")

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

func (c *ApplyCommand) AutocompleteFlags() complete.Flags {
	return command.MergeAutocompleteFlags(
		c.Meta.AutocompleteFlags(command.FlagSetClient),
		complete.Flags{
			"--tasks":                complete.PredictFiles("*.yml"),
			"--verbose":              complete.PredictNothing,
			"--json":                 complete.PredictNothing,
			"--host":                 complete.PredictAnything,
			"--sudo":                 complete.PredictNothing,
			"--accept-new-host-keys": complete.PredictNothing,
			"--tags":                 complete.PredictAnything,
			"--skip-tags":            complete.PredictAnything,
		},
	)
}

func (c *ApplyCommand) Run(args []string) int {
	flags := c.FlagSet()
	flags.Usage = func() { c.Ui.Output(c.Help()) }
	if err := flags.Parse(args); err != nil {
		c.Ui.Error(err.Error())
		c.Ui.Error(command.CommandErrorText(c))
		return 1
	}

	resolvedHost := resolveSshFlags(c.host, c.sudo, c.acceptNewHostKeys)

	data, err := os.ReadFile(c.tasksFile)
	if err != nil {
		c.Ui.Error(fmt.Sprintf("read error: %v", err))
		return 1
	}

	context := make(map[string]interface{})
	var sensitiveValues []string
	for name, argument := range c.arguments {
		if argument.Required && !argument.HasValue() {
			c.Ui.Error(fmt.Sprintf("Missing flag '--%s'", name))
			return 1
		}
		context[name] = argument.GetValue()
		if argument.Sensitive {
			if v := argument.StringValue(); v != "" {
				sensitiveValues = append(sensitiveValues, v)
			}
		}
	}

	taskList, err := tasks.GetTasks(data, context)
	if err != nil {
		c.Ui.Error(fmt.Sprintf("task error: %v", err))
		return 1
	}

	sensitiveValues = append(sensitiveValues, tasks.CollectSensitiveValues(taskList)...)
	subprocess.SetGlobalSensitive(sensitiveValues)
	defer subprocess.SetGlobalSensitive(nil)

	if resolvedHost != "" {
		defer subprocess.CloseSshControlMaster(resolvedHost)
	}

	emitter := c.newEmitter()
	emitter.PlayStart("tasks", resolvedHost)

	start := time.Now()
	counts := ApplyCounts{}
	playName := "tasks"

	keys := tasks.FilterByTags(taskList, c.tags, c.skipTags)
	exprBaseCtx := buildEnvelopeExprContext(context)

	for _, name := range keys {
		env := taskList.GetEnvelope(name)
		taskStart := time.Now()

		if env.HasWhen() {
			ok, err := tasks.EvalBool(env.WhenProgram(), envelopeExprContext(exprBaseCtx, env))
			if err != nil {
				counts.Tasks++
				counts.Errors++
				emitter.ApplyTask(ApplyTaskEvent{
					Play:      playName,
					Name:      name,
					WhenError: err,
					Duration:  time.Since(taskStart),
					Timestamp: time.Now().UTC(),
				})
				emitter.ApplySummary(counts, time.Since(start))
				return 1
			}
			if !ok {
				counts.Tasks++
				counts.Skipped++
				emitter.ApplyTask(ApplyTaskEvent{
					Play:      playName,
					Name:      name,
					Skipped:   true,
					Duration:  time.Since(taskStart),
					Timestamp: time.Now().UTC(),
				})
				continue
			}
		}

		state := env.Task.Execute()
		counts.Tasks++

		switch {
		case state.Error != nil:
			counts.Errors++
			emitter.ApplyTask(ApplyTaskEvent{
				Play:      playName,
				Name:      name,
				State:     state,
				Duration:  time.Since(taskStart),
				Timestamp: time.Now().UTC(),
			})
			emitter.ApplySummary(counts, time.Since(start))
			return 1
		case state.State != state.DesiredState:
			counts.Errors++
			emitter.ApplyTask(ApplyTaskEvent{
				Play:         playName,
				Name:         name,
				State:        state,
				InvalidState: true,
				Duration:     time.Since(taskStart),
				Timestamp:    time.Now().UTC(),
			})
			emitter.ApplySummary(counts, time.Since(start))
			return 1
		case state.Changed:
			counts.Changed++
			emitter.ApplyTask(ApplyTaskEvent{
				Play:      playName,
				Name:      name,
				State:     state,
				Duration:  time.Since(taskStart),
				Timestamp: time.Now().UTC(),
			})
		default:
			counts.OK++
			emitter.ApplyTask(ApplyTaskEvent{
				Play:      playName,
				Name:      name,
				State:     state,
				Duration:  time.Since(taskStart),
				Timestamp: time.Now().UTC(),
			})
		}
	}

	emitter.ApplySummary(counts, time.Since(start))
	return 0
}

// newEmitter constructs the EventEmitter for this run. --json builds a
// JSONEmitter; otherwise the human Formatter is used. The verbose flag is
// only meaningful for the human path - JSON output already includes the
// resolved commands in each task event's `commands` array.
func (c *ApplyCommand) newEmitter() EventEmitter {
	if c.json {
		return NewJSONEmitter(c.Ui)
	}
	return NewFormatter(c.Ui, c.verbose)
}
