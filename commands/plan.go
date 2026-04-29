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

// PlanCommand reports the drift each task in a docket recipe would produce
// against the live server, without mutating it. Plan is fully driven by the
// per-task Plan() method; the apply path is never invoked.
type PlanCommand struct {
	command.Meta

	tasksFile         string
	json              bool
	detailedExitCode  bool
	host              string
	sudo              bool
	acceptNewHostKeys bool
	tags              []string
	skipTags          []string
	varsFiles         []string
	play              string
	arguments         map[string]*Argument
}

func (c *PlanCommand) Name() string {
	return "plan"
}

func (c *PlanCommand) Synopsis() string {
	return "Reports the drift a docket task file would produce, without mutating state"
}

func (c *PlanCommand) Help() string {
	return command.CommandHelp(c)
}

func (c *PlanCommand) Examples() map[string]string {
	appName := os.Getenv("CLI_APP_NAME")
	return map[string]string{
		"Plan tasks from the default tasks.yml": fmt.Sprintf("%s %s", appName, c.Name()),
		"Plan tasks from a specific file":       fmt.Sprintf("%s %s --tasks path/to/task.yml", appName, c.Name()),
		"Plan tasks from a remote URL":          fmt.Sprintf("%s %s --tasks http://dokku.com/docket/example.yml", appName, c.Name()),
		"Override a task input":                 fmt.Sprintf("%s %s --name lollipop", appName, c.Name()),
	}
}

func (c *PlanCommand) Arguments() []command.Argument {
	return []command.Argument{}
}

func (c *PlanCommand) AutocompleteArgs() complete.Predictor {
	return complete.PredictNothing
}

func (c *PlanCommand) ParsedArguments(args []string) (map[string]command.Argument, error) {
	return command.ParseArguments(args, c.Arguments())
}

func (c *PlanCommand) FlagSet() *flag.FlagSet {
	f := c.Meta.FlagSet(c.Name(), command.FlagSetClient)
	f.StringVar(&c.tasksFile, "tasks", "tasks.yml", "a yaml file containing a task list")
	f.BoolVar(&c.json, "json", false, "emit one JSON-lines event per play/task/summary instead of human-readable output. Schema is keyed by `version: 1`; sensitive values mask to `***`.")
	f.BoolVar(&c.detailedExitCode, "detailed-exitcode", false, "exit 0 when no drift is detected, 2 when drift is detected, 1 on error. Without this flag plan exits 0 regardless of drift.")
	f.StringVar(&c.host, "host", "", "remote dokku host as [user@]host[:port]; equivalent to DOKKU_HOST. Routes every dokku invocation through ssh.")
	f.BoolVar(&c.sudo, "sudo", false, "wrap remote dokku invocations with `sudo -n`; equivalent to DOKKU_SUDO=1")
	f.BoolVar(&c.acceptNewHostKeys, "accept-new-host-keys", false, "for SSH transport, accept new host keys on first connection (`-o StrictHostKeyChecking=accept-new`). MITM risk on first connect.")
	f.StringSliceVar(&c.tags, "tags", nil, "comma-separated tag list; only tasks whose `tags:` set intersects this list are planned")
	f.StringSliceVar(&c.skipTags, "skip-tags", nil, "comma-separated tag list; tasks whose `tags:` set intersects this list are skipped")
	f.StringArrayVar(&c.varsFiles, "vars-file", nil, "load input values from a YAML or JSON file (repeatable; later files override earlier; CLI --name=value flags always win). A .json extension parses as JSON; otherwise YAML.")
	f.StringVar(&c.play, "play", "", "plan only the play with this name (matches the play's `name:` field; auto-named plays use `play #N`)")

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

func (c *PlanCommand) AutocompleteFlags() complete.Flags {
	return command.MergeAutocompleteFlags(
		c.Meta.AutocompleteFlags(command.FlagSetClient),
		complete.Flags{
			"--tasks":                complete.PredictFiles("*.yml"),
			"--json":                 complete.PredictNothing,
			"--detailed-exitcode":    complete.PredictNothing,
			"--host":                 complete.PredictAnything,
			"--sudo":                 complete.PredictNothing,
			"--accept-new-host-keys": complete.PredictNothing,
			"--tags":                 complete.PredictAnything,
			"--skip-tags":            complete.PredictAnything,
			"--vars-file":            complete.PredictFiles("*"),
			"--play":                 complete.PredictAnything,
		},
	)
}

// Run iterates every task in the parsed recipe, invokes Plan() (read-only
// by contract), and prints a one-line summary per task plus a final
// summary line.
//
// Exit codes (default):
//
//	0 - plan completed successfully (regardless of drift)
//	1 - read error, parse error, or read-state probe error
//
// Exit codes (--detailed-exitcode):
//
//	0 - plan completed cleanly; no drift detected
//	1 - read error, parse error, or read-state probe error (errors win)
//	2 - plan completed; at least one task reported drift
func (c *PlanCommand) Run(args []string) int {
	flags := c.FlagSet()
	flags.Usage = func() { c.Ui.Output(c.Help()) }
	if err := flags.Parse(args); err != nil {
		c.Ui.Error(err.Error())
		c.Ui.Error(command.CommandErrorText(c))
		return 1
	}

	varsFileKeys, err := applyVarsFiles(c.arguments, flags, c.varsFiles)
	if err != nil {
		c.Ui.Error(err.Error())
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

	userSet := userSetKeys(flags, varsFileKeys, c.arguments)

	plays, err := tasks.GetPlays(data, context, userSet)
	if err != nil {
		c.Ui.Error(fmt.Sprintf("task error: %v", err))
		return 1
	}

	// Compute file-level input names from the *unfiltered* play list so
	// --play does not accidentally hide an inputs-only play whose
	// declared inputs the surviving play's when: depends on.
	fileLevelKeys := tasks.FileLevelInputNames(plays)

	plays, err = filterPlaysByName(plays, c.play)
	if err != nil {
		c.Ui.Error(err.Error())
		return 1
	}

	sensitiveValues = append(sensitiveValues, tasks.CollectPlaySensitiveValues(plays)...)
	subprocess.SetGlobalSensitive(sensitiveValues)
	defer subprocess.SetGlobalSensitive(nil)

	if resolvedHost != "" {
		defer subprocess.CloseSshControlMaster(resolvedHost)
	}

	emitter := c.newEmitter()
	start := time.Now()
	counts := PlanCounts{}
	hasError := false
	hasDrift := false
	playWhenExprCtx := buildEnvelopeExprContext(buildPlayWhenContext(context, fileLevelKeys, userSet))
	// registered + loopAccum carry the same role as in apply: predicates
	// in plan mode see `.registered.<name>` based on the
	// post-override synthesized TaskOutputState of prior tasks.
	// `ignore_errors` is a no-op in plan because plan never aborts the
	// run.
	registered := map[string]tasks.RegisteredValue{}
	loopAccum := loopRegisterAccumulator{}

	for _, play := range plays {
		if play.IsFileLevel() {
			// Inputs-only plays carry no tasks; their inputs are
			// already folded into fileLevelKeys above. They produce
			// no output and do not count toward play totals.
			continue
		}
		if play.HasWhen() {
			ok, err := tasks.EvalBool(play.WhenProgram(), playWhenExprCtx)
			if err != nil {
				counts.PlaysSkipped++
				hasError = true
				emitter.PlaySkipped(play.Name, fmt.Sprintf("%s (error: %v)", play.When, err))
				continue
			}
			if !ok {
				counts.PlaysSkipped++
				emitter.PlaySkipped(play.Name, play.When)
				continue
			}
		}

		emitter.PlayStart(play.Name, resolvedHost)

		playExprCtx := buildEnvelopeExprContext(tasks.BuildPerPlayContext(context, play.Inputs, userSet))

		for _, name := range tasks.FilterByTags(play.Tasks, c.tags, c.skipTags) {
			env := play.Tasks.GetEnvelope(name)
			taskStart := time.Now()

			if env.HasWhen() {
				ok, err := tasks.EvalBool(env.WhenProgram(), envelopeExprContext(playExprCtx, env, nil, registered))
				if err != nil {
					counts.Tasks++
					counts.Errors++
					hasError = true
					emitter.PlanTask(PlanTaskEvent{
						Play:      play.Name,
						Name:      name,
						WhenError: err,
						Duration:  time.Since(taskStart),
						Timestamp: time.Now().UTC(),
					})
					continue
				}
				if !ok {
					counts.Tasks++
					counts.Skipped++
					emitter.PlanTask(PlanTaskEvent{
						Play:      play.Name,
						Name:      name,
						Skipped:   true,
						Duration:  time.Since(taskStart),
						Timestamp: time.Now().UTC(),
					})
					continue
				}
			}

			result := env.Task.Plan()
			counts.Tasks++

			// Synthesize a TaskOutputState from the PlanResult so the
			// changed_when / failed_when / register phases see a
			// consistent shape across plan and apply. Plan probes do
			// not run subprocesses on their drift / in-sync paths, so
			// Stdout/Stderr/ExitCode are non-zero only on probe-error
			// paths (#210 plumbed those through PlanResult).
			synth := tasks.TaskOutputState{
				Changed:      !result.InSync,
				Error:        result.Error,
				State:        result.DesiredState,
				DesiredState: result.DesiredState,
				Commands:     result.Commands,
				Stdout:       result.Stdout,
				Stderr:       result.Stderr,
				ExitCode:     result.ExitCode,
			}
			postState, overrideErr := applyEnvelopeOverrides(env, synth, playExprCtx, registered)
			if overrideErr != nil {
				counts.Errors++
				hasError = true
				emitter.PlanTask(PlanTaskEvent{
					Play:      play.Name,
					Name:      name,
					WhenError: overrideErr,
					Duration:  time.Since(taskStart),
					Timestamp: time.Now().UTC(),
				})
				continue
			}
			synth = postState
			recordRegister(env, synth, loopAccum, registered)

			// Reflect the post-override verdict back onto the
			// PlanResult so the existing classifier and the formatter
			// / JSON output reflect the override. Falsy `failed_when`
			// clears Error; truthy installs one. Truthy `changed_when`
			// converts an in-sync probe to drift, while falsy
			// `changed_when` makes drift look in-sync (the plan event
			// continues to render via the same code path).
			result.Error = synth.Error
			result.InSync = !synth.Changed && synth.Error == nil

			switch {
			case result.Error != nil:
				counts.Errors++
				hasError = true
			case result.InSync:
				counts.InSync++
			default:
				counts.WouldChange++
				hasDrift = true
			}

			emitter.PlanTask(PlanTaskEvent{
				Play:      play.Name,
				Name:      name,
				Result:    result,
				Duration:  time.Since(taskStart),
				Timestamp: time.Now().UTC(),
			})
		}
	}

	emitter.PlanSummary(counts, time.Since(start))

	if hasError {
		return 1
	}
	if c.detailedExitCode && hasDrift {
		return 2
	}
	return 0
}

// newEmitter constructs the EventEmitter for this run. --json builds a
// JSONEmitter; otherwise the human Formatter is used.
func (c *PlanCommand) newEmitter() EventEmitter {
	if c.json {
		return NewJSONEmitter(c.Ui)
	}
	return NewFormatter(c.Ui, false)
}
