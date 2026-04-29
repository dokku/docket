package commands

import (
	"errors"
	"fmt"
	"os"
	"strings"
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
	tasksFormat       string
	verbose           bool
	json              bool
	host              string
	sudo              bool
	acceptNewHostKeys bool
	tags              []string
	skipTags          []string
	varsFiles         []string
	play              string
	failFast          bool
	listTasks         bool
	startAtTask       string
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
		"Apply tasks from a specific YAML file":  fmt.Sprintf("%s %s --tasks path/to/task.yml", appName, c.Name()),
		"Apply tasks from a JSON5 file":          fmt.Sprintf("%s %s --tasks path/to/tasks.json", appName, c.Name()),
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
	f.StringVar(&c.tasksFile, "tasks", "", "task file (YAML or JSON5) containing a task list. When omitted, docket probes tasks.yml -> tasks.yaml -> tasks.json in the current directory.")
	f.BoolVar(&c.verbose, "verbose", false, "echo the resolved dokku command for each task as a continuation line. Values from inputs declared `sensitive: true` and from task struct fields tagged `sensitive:\"true\"` are masked as `***`. Ignored when --json is set; the JSON output already includes the resolved commands.")
	f.BoolVar(&c.json, "json", false, "emit one JSON-lines event per play/task/summary instead of human-readable output. Schema is keyed by `version: 1`; sensitive values mask to `***`.")
	f.StringVar(&c.host, "host", "", "remote dokku host as [user@]host[:port]; equivalent to DOKKU_HOST. Routes every dokku invocation through ssh.")
	f.BoolVar(&c.sudo, "sudo", false, "wrap remote dokku invocations with `sudo -n`; equivalent to DOKKU_SUDO=1")
	f.BoolVar(&c.acceptNewHostKeys, "accept-new-host-keys", false, "for SSH transport, accept new host keys on first connection (`-o StrictHostKeyChecking=accept-new`). MITM risk on first connect.")
	f.StringSliceVar(&c.tags, "tags", nil, "comma-separated tag list; only tasks whose `tags:` set intersects this list run")
	f.StringSliceVar(&c.skipTags, "skip-tags", nil, "comma-separated tag list; tasks whose `tags:` set intersects this list are skipped")
	f.StringArrayVar(&c.varsFiles, "vars-file", nil, "load input values from a YAML or JSON file (repeatable; later files override earlier; CLI --name=value flags always win). A .json extension parses as JSON; otherwise YAML.")
	f.StringVar(&c.play, "play", "", "run only the play with this name (matches the play's `name:` field; auto-named plays use `play #N`)")
	f.BoolVar(&c.failFast, "fail-fast", false, "abort the entire run on the first task error. By default, an error aborts only the current play and the next play still runs.")
	f.BoolVar(&c.listTasks, "list-tasks", false, "print the resolved task plan and exit without running. Honors --play / --tags / --skip-tags and shows expanded loop iterations and [skipped] markers for when:-skipped tasks.")
	f.StringVar(&c.startAtTask, "start-at-task", "", "skip every task before the matched name; the matched task and successors run normally. Filter order: --start-at-task -> --tags/--skip-tags -> per-task when: at execution. The name search walks every play in source order, narrowed by --play.")

	taskFile, format := resolveTaskFileFromArgs(os.Args)
	data, err := os.ReadFile(taskFile)
	if err != nil {
		return f
	}

	arguments, err := registerInputFlags(f, data, format)
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
			"--tasks":                complete.PredictFiles(taskFileAutocompleteGlob),
			"--verbose":              complete.PredictNothing,
			"--json":                 complete.PredictNothing,
			"--host":                 complete.PredictAnything,
			"--sudo":                 complete.PredictNothing,
			"--accept-new-host-keys": complete.PredictNothing,
			"--tags":                 complete.PredictAnything,
			"--skip-tags":            complete.PredictAnything,
			"--vars-file":            complete.PredictFiles("*"),
			"--play":                 complete.PredictAnything,
			"--fail-fast":            complete.PredictNothing,
			"--list-tasks":           complete.PredictNothing,
			"--start-at-task":        complete.PredictAnything,
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

	varsFileKeys, err := applyVarsFiles(c.arguments, flags, c.varsFiles)
	if err != nil {
		c.Ui.Error(err.Error())
		return 1
	}

	resolvedHost := resolveSshFlags(c.host, c.sudo, c.acceptNewHostKeys)

	resolvedPath, resolvedFormat, err := resolveTaskFilePath(c.tasksFile)
	if err != nil {
		c.Ui.Error(fmt.Sprintf("read error: %v", err))
		return 1
	}
	c.tasksFile = resolvedPath
	c.tasksFormat = resolvedFormat

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

	plays, err := tasks.GetPlaysWithFormat(data, c.tasksFormat, context, userSet)
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

	if c.startAtTask != "" {
		if !startAtTaskMatches(plays, c.startAtTask) {
			c.Ui.Error(fmt.Sprintf(
				"--start-at-task %q: no task matched name; available names: %s",
				c.startAtTask, formatStartAtTaskNames(plays),
			))
			return 1
		}
	}

	if c.listTasks {
		return renderListTasks(c.Ui, listTasksOptions{
			plays:         plays,
			includes:      c.tags,
			skips:         c.skipTags,
			fileLevelKeys: fileLevelKeys,
			userSet:       userSet,
			context:       context,
			jsonOut:       c.json,
		})
	}

	sensitiveValues = append(sensitiveValues, tasks.CollectPlaySensitiveValues(plays)...)
	subprocess.SetGlobalSensitive(sensitiveValues)
	defer subprocess.SetGlobalSensitive(nil)

	if resolvedHost != "" {
		defer subprocess.CloseSshControlMaster(resolvedHost)
	}

	emitter := c.newEmitter()
	start := time.Now()
	counts := ApplyCounts{}
	playWhenExprCtx := buildEnvelopeExprContext(buildPlayWhenContext(context, fileLevelKeys, userSet))
	hasError := false
	// startedAtTask gates --start-at-task: false until an envelope name
	// matches c.startAtTask, at which point every subsequent task runs
	// normally. Shared by reference across plays so the search spans the
	// whole filtered play list.
	startedAtTask := c.startAtTask == ""
	// registered is the run-wide map predicates reach via
	// `.registered.<name>`. loopAccum buffers per-iteration states for
	// loop+register expansions so the running aggregate is exposed to
	// predicates in subsequent iterations and finalized once the loop
	// finishes. Both maps are scoped to a single docket apply
	// invocation.
	registered := map[string]tasks.RegisteredValue{}
	loopAccum := loopRegisterAccumulator{}

playLoop:
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
				if c.failFast {
					break playLoop
				}
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

		ac := &applyContext{
			play:        play,
			playExprCtx: playExprCtx,
			registered:  registered,
			loopAccum:   loopAccum,
			emitter:     emitter,
			counts:      &counts,
			failFast:    c.failFast,
			startAtTask: c.startAtTask,
			started:     &startedAtTask,
		}

		failed := false
		for _, name := range tasks.FilterByTags(play.Tasks, c.tags, c.skipTags) {
			env := play.Tasks.GetEnvelope(name)
			outcome := c.executeTask(env, name, ac, nil, "")
			if outcome.abort {
				emitter.ApplySummary(counts, time.Since(start))
				return 1
			}
			if outcome.failed {
				failed = true
				hasError = true
				// Without --fail-fast, an error in this play aborts the
				// rest of this play but the next play still runs.
				break
			}
		}
		_ = failed
	}

	emitter.ApplySummary(counts, time.Since(start))
	if hasError {
		return 1
	}
	return 0
}

// applyContext bundles the run-wide state apply's per-task helpers
// share so the function signatures stay tractable.
type applyContext struct {
	play        *tasks.Play
	playExprCtx map[string]interface{}
	registered  map[string]tasks.RegisteredValue
	loopAccum   loopRegisterAccumulator
	emitter     EventEmitter
	counts      *ApplyCounts
	failFast    bool
	// startAtTask is the --start-at-task target, or "" when the flag
	// was not set. started is a shared pointer flipped to true the
	// first time an envelope name matches the target; until it flips,
	// executeTask emits a [skipped] event with the "before
	// --start-at-task" reason and skips dispatch.
	startAtTask string
	started     *bool
}

// applyTaskOutcome is the per-task verdict the apply loop reads back
// from executeTask. failed reports whether the task's failure should
// abort the current play (false when ignore_errors swallowed the
// error). abort reports --fail-fast triggered. state carries the
// post-override TaskOutputState so a group walker can propagate the
// failing child's state into rescue's `.failed_task` binding.
type applyTaskOutcome struct {
	state   tasks.TaskOutputState
	failed  bool
	abort   bool
	skipped bool
}

// executeTask runs one envelope - leaf or group. The phase string
// labels child events emitted from a group walk ("block", "rescue",
// "always"); top-level callers pass "". failedTask is non-nil only
// when called from a rescue walker so the rescue child's predicates
// can reference `.failed_task`.
//
// --start-at-task gating sits at the top: until ac.started flips to
// true, an envelope whose name does not match ac.startAtTask (and
// whose group descendants also do not) is reported as `[skipped]`
// with reason "before --start-at-task" and dispatch is skipped. A
// group whose own name does not match but a descendant does is
// entered so the recursive executeTask call lands on the matched
// child.
func (c *ApplyCommand) executeTask(env *tasks.TaskEnvelope, name string, ac *applyContext, failedTask interface{}, phase string) applyTaskOutcome {
	if !*ac.started && ac.startAtTask != "" {
		switch {
		case name == ac.startAtTask:
			*ac.started = true
		case env.IsGroup() && tasks.EnvelopeContainsName(env, ac.startAtTask):
			// Don't skip; descend into the group so the matching
			// child runs. The synthesized group state will reflect
			// only the executed children, not the skipped ones.
		default:
			ac.counts.Tasks++
			ac.counts.Skipped++
			ac.emitter.ApplyTask(ApplyTaskEvent{
				Play:       ac.play.Name,
				Name:       name,
				Phase:      phase,
				Group:      env.IsGroup(),
				Skipped:    true,
				SkipReason: "before --start-at-task",
				Timestamp:  time.Now().UTC(),
			})
			return applyTaskOutcome{skipped: true}
		}
	}
	if env.IsGroup() {
		return c.executeGroup(env, name, ac, failedTask, phase)
	}
	return c.executeLeafTask(env, name, ac, failedTask, phase)
}

// executeLeafTask runs a single non-group task envelope through the
// when -> execute -> overrides -> register -> classify pipeline that
// pre-#211 lived inline inside Run.
func (c *ApplyCommand) executeLeafTask(env *tasks.TaskEnvelope, name string, ac *applyContext, failedTask interface{}, phase string) applyTaskOutcome {
	taskStart := time.Now()

	if env.HasWhen() {
		ok, err := tasks.EvalBool(env.WhenProgram(), envelopeExprContext(ac.playExprCtx, env, nil, ac.registered, failedTask))
		if err != nil {
			ac.counts.Tasks++
			ac.counts.Errors++
			ac.emitter.ApplyTask(ApplyTaskEvent{
				Play:      ac.play.Name,
				Name:      name,
				Phase:     phase,
				WhenError: err,
				Duration:  time.Since(taskStart),
				Timestamp: time.Now().UTC(),
			})
			return applyTaskOutcome{failed: true, abort: c.failFast}
		}
		if !ok {
			ac.counts.Tasks++
			ac.counts.Skipped++
			ac.emitter.ApplyTask(ApplyTaskEvent{
				Play:      ac.play.Name,
				Name:      name,
				Phase:     phase,
				Skipped:   true,
				Duration:  time.Since(taskStart),
				Timestamp: time.Now().UTC(),
			})
			return applyTaskOutcome{skipped: true}
		}
	}

	state := env.Task.Execute()
	ac.counts.Tasks++

	postState, overrideErr := applyEnvelopeOverrides(env, state, ac.playExprCtx, ac.registered, failedTask)
	if overrideErr != nil {
		ac.counts.Errors++
		ac.emitter.ApplyTask(ApplyTaskEvent{
			Play:      ac.play.Name,
			Name:      name,
			Phase:     phase,
			WhenError: overrideErr,
			Duration:  time.Since(taskStart),
			Timestamp: time.Now().UTC(),
		})
		return applyTaskOutcome{failed: true, abort: c.failFast}
	}
	state = postState

	recordRegister(env, state, ac.loopAccum, ac.registered)

	switch {
	case state.Error != nil:
		ignored := env.IgnoreErrors
		if !ignored {
			ac.counts.Errors++
		}
		ac.emitter.ApplyTask(ApplyTaskEvent{
			Play:      ac.play.Name,
			Name:      name,
			Phase:     phase,
			State:     state,
			Ignored:   ignored,
			Duration:  time.Since(taskStart),
			Timestamp: time.Now().UTC(),
		})
		return applyTaskOutcome{state: state, failed: !ignored, abort: c.failFast && !ignored}
	case state.State != state.DesiredState:
		ignored := env.IgnoreErrors
		if !ignored {
			ac.counts.Errors++
		}
		ac.emitter.ApplyTask(ApplyTaskEvent{
			Play:         ac.play.Name,
			Name:         name,
			Phase:        phase,
			State:        state,
			InvalidState: true,
			Ignored:      ignored,
			Duration:     time.Since(taskStart),
			Timestamp:    time.Now().UTC(),
		})
		return applyTaskOutcome{state: state, failed: !ignored, abort: c.failFast && !ignored}
	case state.Changed:
		ac.counts.Changed++
		ac.emitter.ApplyTask(ApplyTaskEvent{
			Play:      ac.play.Name,
			Name:      name,
			Phase:     phase,
			State:     state,
			Duration:  time.Since(taskStart),
			Timestamp: time.Now().UTC(),
		})
		return applyTaskOutcome{state: state}
	default:
		ac.counts.OK++
		ac.emitter.ApplyTask(ApplyTaskEvent{
			Play:      ac.play.Name,
			Name:      name,
			Phase:     phase,
			State:     state,
			Duration:  time.Since(taskStart),
			Timestamp: time.Now().UTC(),
		})
		return applyTaskOutcome{state: state}
	}
}

// executeGroup runs a try/catch/finally group entry (#211): block ->
// (rescue if a block child errored) -> always. Children execute via
// executeTask so nested groups recurse naturally. The synthesized
// group state is passed through the group's own envelope overrides
// (failed_when / changed_when / register / ignore_errors) so the
// group itself participates in the same predicate chain leaf tasks do.
func (c *ApplyCommand) executeGroup(env *tasks.TaskEnvelope, name string, ac *applyContext, failedTask interface{}, phase string) applyTaskOutcome {
	taskStart := time.Now()

	if env.HasWhen() {
		ok, err := tasks.EvalBool(env.WhenProgram(), envelopeExprContext(ac.playExprCtx, env, nil, ac.registered, failedTask))
		if err != nil {
			ac.counts.Tasks++
			ac.counts.Errors++
			ac.emitter.ApplyTask(ApplyTaskEvent{
				Play:      ac.play.Name,
				Name:      name,
				Phase:     phase,
				Group:     true,
				WhenError: err,
				Duration:  time.Since(taskStart),
				Timestamp: time.Now().UTC(),
			})
			return applyTaskOutcome{failed: true, abort: c.failFast}
		}
		if !ok {
			ac.counts.Tasks++
			ac.counts.Skipped++
			ac.emitter.ApplyTask(ApplyTaskEvent{
				Play:      ac.play.Name,
				Name:      name,
				Phase:     phase,
				Group:     true,
				Skipped:   true,
				Duration:  time.Since(taskStart),
				Timestamp: time.Now().UTC(),
			})
			return applyTaskOutcome{skipped: true}
		}
	}

	// Walk block children. Stop at the first child whose post-override
	// outcome is failed AND ignore_errors did not swallow it. A
	// swallowed (ignored) child does NOT trigger rescue per #210 rule:
	// ignore_errors is the "swallow" path; rescue is the "handle" path.
	var (
		anyChanged       bool
		blockFailedState *tasks.TaskOutputState
		lastChildState   tasks.TaskOutputState
	)
	for i, child := range env.Block {
		childName := child.Name
		if childName == "" {
			childName = fmt.Sprintf("%s.block[%d]", name, i)
		}
		outcome := c.executeTask(child, childName, ac, nil, "block")
		if outcome.abort {
			return applyTaskOutcome{abort: true}
		}
		if outcome.state.Changed {
			anyChanged = true
		}
		lastChildState = outcome.state
		if outcome.failed {
			s := outcome.state
			blockFailedState = &s
			break
		}
	}

	// Run rescue children when block failed.
	rescueErr := error(nil)
	if blockFailedState != nil {
		for i, child := range env.Rescue {
			childName := child.Name
			if childName == "" {
				childName = fmt.Sprintf("%s.rescue[%d]", name, i)
			}
			outcome := c.executeTask(child, childName, ac, *blockFailedState, "rescue")
			if outcome.abort {
				return applyTaskOutcome{abort: true}
			}
			if outcome.state.Changed {
				anyChanged = true
			}
			lastChildState = outcome.state
			if outcome.failed && rescueErr == nil {
				if outcome.state.Error != nil {
					rescueErr = outcome.state.Error
				} else {
					rescueErr = errors.New("rescue child failed")
				}
			}
		}
	}

	// Always children run unconditionally.
	alwaysErr := error(nil)
	for i, child := range env.Always {
		childName := child.Name
		if childName == "" {
			childName = fmt.Sprintf("%s.always[%d]", name, i)
		}
		outcome := c.executeTask(child, childName, ac, nil, "always")
		if outcome.abort {
			return applyTaskOutcome{abort: true}
		}
		if outcome.state.Changed {
			anyChanged = true
		}
		lastChildState = outcome.state
		if outcome.failed && alwaysErr == nil {
			if outcome.state.Error != nil {
				alwaysErr = outcome.state.Error
			} else {
				alwaysErr = errors.New("always child failed")
			}
		}
	}

	// Synthesize the group's TaskOutputState. Rescue clearing the
	// block error implies the group succeeded unless always itself
	// errored. always errors take precedence over a cleared block
	// error; if block errored and rescue also errored, the rescue
	// error is the group's verdict (most recent uncaught failure).
	groupState := tasks.TaskOutputState{
		Changed:      anyChanged,
		DesiredState: lastChildState.DesiredState,
		State:        lastChildState.DesiredState,
		Stdout:       lastChildState.Stdout,
		Stderr:       lastChildState.Stderr,
		ExitCode:     lastChildState.ExitCode,
		Commands:     lastChildState.Commands,
		Message:      lastChildState.Message,
	}
	switch {
	case alwaysErr != nil:
		groupState.Error = alwaysErr
		groupState.State = ""
	case rescueErr != nil:
		groupState.Error = rescueErr
		groupState.State = ""
	case blockFailedState != nil && len(env.Rescue) == 0:
		// Block errored and there is no rescue clause; the original
		// error propagates as the group's verdict.
		groupState.Error = blockFailedState.Error
		groupState.State = blockFailedState.State
		groupState.DesiredState = blockFailedState.DesiredState
	}

	postState, overrideErr := applyEnvelopeOverrides(env, groupState, ac.playExprCtx, ac.registered, failedTask)
	if overrideErr != nil {
		ac.counts.Errors++
		ac.emitter.ApplyTask(ApplyTaskEvent{
			Play:      ac.play.Name,
			Name:      name,
			Phase:     phase,
			Group:     true,
			WhenError: overrideErr,
			Duration:  time.Since(taskStart),
			Timestamp: time.Now().UTC(),
		})
		return applyTaskOutcome{failed: true, abort: c.failFast}
	}
	groupState = postState

	recordRegister(env, groupState, ac.loopAccum, ac.registered)
	ac.counts.Tasks++

	switch {
	case groupState.Error != nil:
		ignored := env.IgnoreErrors
		if !ignored {
			ac.counts.Errors++
		}
		ac.emitter.ApplyTask(ApplyTaskEvent{
			Play:      ac.play.Name,
			Name:      name,
			Phase:     phase,
			Group:     true,
			State:     groupState,
			Ignored:   ignored,
			Duration:  time.Since(taskStart),
			Timestamp: time.Now().UTC(),
		})
		return applyTaskOutcome{state: groupState, failed: !ignored, abort: c.failFast && !ignored}
	case groupState.Changed:
		ac.counts.Changed++
		ac.emitter.ApplyTask(ApplyTaskEvent{
			Play:      ac.play.Name,
			Name:      name,
			Phase:     phase,
			Group:     true,
			State:     groupState,
			Duration:  time.Since(taskStart),
			Timestamp: time.Now().UTC(),
		})
		return applyTaskOutcome{state: groupState}
	default:
		ac.counts.OK++
		ac.emitter.ApplyTask(ApplyTaskEvent{
			Play:      ac.play.Name,
			Name:      name,
			Phase:     phase,
			Group:     true,
			State:     groupState,
			Duration:  time.Since(taskStart),
			Timestamp: time.Now().UTC(),
		})
		return applyTaskOutcome{state: groupState}
	}
}

// startAtTaskMatches reports whether target matches some envelope name
// across plays, walking each play's task envelopes plus any block /
// rescue / always children of group entries. Used to validate the
// --start-at-task flag before the executor begins so a typo errors out
// up-front instead of silently skipping the entire run.
func startAtTaskMatches(plays []*tasks.Play, target string) bool {
	for _, play := range plays {
		if play == nil {
			continue
		}
		for _, name := range play.Tasks.Keys() {
			env := play.Tasks.GetEnvelope(name)
			if name == target {
				return true
			}
			if tasks.EnvelopeContainsName(env, target) {
				return true
			}
		}
	}
	return false
}

// formatStartAtTaskNames builds the "available names: ..." hint for an
// unmatched --start-at-task error. Names are deduplicated and rendered
// in source order, quoted so the user can copy a name verbatim back
// onto the CLI.
func formatStartAtTaskNames(plays []*tasks.Play) string {
	seen := map[string]bool{}
	var quoted []string
	for _, play := range plays {
		if play == nil {
			continue
		}
		for _, name := range play.Tasks.Keys() {
			if !seen[name] {
				seen[name] = true
				quoted = append(quoted, fmt.Sprintf("%q", name))
			}
			env := play.Tasks.GetEnvelope(name)
			for _, descendant := range tasks.CollectEnvelopeNames([]*tasks.TaskEnvelope{env}) {
				if descendant == "" || descendant == name {
					continue
				}
				if !seen[descendant] {
					seen[descendant] = true
					quoted = append(quoted, fmt.Sprintf("%q", descendant))
				}
			}
		}
	}
	if len(quoted) == 0 {
		return "(none)"
	}
	return strings.Join(quoted, ", ")
}

// filterPlaysByName narrows plays to the single play whose Name matches
// target. An empty target returns plays unchanged. An unmatched target
// returns an error so the user sees a clear "no such play" diagnostic
// rather than silently doing nothing.
func filterPlaysByName(plays []*tasks.Play, target string) ([]*tasks.Play, error) {
	if target == "" {
		return plays, nil
	}
	for _, play := range plays {
		if play.Name == target {
			return []*tasks.Play{play}, nil
		}
	}
	names := make([]string, 0, len(plays))
	for _, play := range plays {
		names = append(names, fmt.Sprintf("%q", play.Name))
	}
	return nil, fmt.Errorf("--play %q: no play with that name; available plays: %v", target, names)
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
