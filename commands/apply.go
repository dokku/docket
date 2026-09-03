package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
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

	// BaseDir is the directory relative paths resolve against - the recipe
	// probed when --tasks is absent, and any output written to a relative
	// path. Populated from main.go; empty means the process working
	// directory, which is what it always was.
	//
	// It exists so a test can point a command at a temp directory instead of
	// chdir'ing the whole process, which no test can do while another runs
	// beside it.
	BaseDir string

	// Stdin is where a `--tasks -` recipe is read from. Populated from
	// main.go; nil reads the process's standard input. A test hands over its
	// own pipe instead of swapping os.Stdin, which no two tests can do at
	// once.
	Stdin io.Reader

	stdin *stdinRecipeSource

	// Argv is the process argv this command resolves its --tasks and
	// --tasks-format from, before pflag has parsed anything. Populated from
	// main.go; nil falls back to os.Args, which is what a command built
	// directly gets. It exists so a test can hand the command its own argv
	// instead of assigning to the process global and putting it back.
	Argv []string

	// Ctx is the run context, populated from main.go with the process signal
	// context. It carries cancellation down through every task's Plan and
	// Execute. Nil when the command was constructed directly (tests do this),
	// in which case Run falls back to context.Background().
	Ctx context.Context

	tasksFile string
	// tasksFormatFlag is the raw --tasks-format value; tasksFormat is
	// the format actually used, after override / extension / sniff.
	tasksFormatFlag   string
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
	detailedExitCode  bool
	arguments         map[string]*Argument

	// tasksData caches the recipe bytes read while building the FlagSet (to
	// pre-register input flags). Run reuses them instead of reading the
	// source a second time, so a --tasks URL is fetched once per
	// invocation. tasksDataSource records where those bytes came from; the
	// cache is honored only when Run resolves the same source.
	tasksData       []byte
	tasksDataSource string
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
		"Apply a recipe piped in on stdin":       fmt.Sprintf("%s export --output - | %s %s -", appName, appName, c.Name()),
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
	f.StringVar(&c.tasksFile, "tasks", "", "task file (YAML or JSON5) containing a task list. Pass - to read the recipe from stdin. When omitted, docket probes tasks.yml -> tasks.yaml -> tasks.json in the current directory. An http(s):// URL is fetched over HTTP.")
	f.StringVar(&c.tasksFormatFlag, "tasks-format", "", "parse the recipe as this format ("+recipeFormatList()+") instead of detecting it from the file extension. Required only when the extension is absent or wrong; stdin is otherwise sniffed from its first byte.")
	f.BoolVar(&c.verbose, "verbose", false, "echo the resolved dokku command for each task as a continuation line. Values from inputs declared `sensitive: true` and from task struct fields tagged `sensitive:\"true\"` are masked as `***`. Ignored when --json is set; the JSON output already includes the resolved commands.")
	f.BoolVar(&c.json, "json", false, "emit one JSON-lines event per play/task/summary instead of human-readable output. Schema is keyed by `version: 1`; sensitive values mask to `***`.")
	f.StringVar(&c.host, "host", "", "remote dokku host as [user@]host[:port]; equivalent to DOKKU_HOST. Routes every dokku invocation through ssh.")
	f.BoolVar(&c.sudo, "sudo", false, "run dokku as root via `sudo -n` - remotely with --host, locally without. Covers dokku only, not the local helper commands some tasks run. Equivalent to DOKKU_SUDO=1.")
	f.BoolVar(&c.acceptNewHostKeys, "accept-new-host-keys", false, "for SSH transport, accept new host keys on first connection (`-o StrictHostKeyChecking=accept-new`). MITM risk on first connect.")
	f.StringSliceVar(&c.tags, "tags", nil, "comma-separated tag list; only tasks whose `tags:` set intersects this list run")
	f.StringSliceVar(&c.skipTags, "skip-tags", nil, "comma-separated tag list; tasks whose `tags:` set intersects this list are skipped")
	f.StringArrayVar(&c.varsFiles, "vars-file", nil, "load input values from a YAML or JSON file (repeatable; later files override earlier; CLI --name=value flags always win). A .json extension parses as JSON; otherwise YAML.")
	f.StringVar(&c.play, "play", "", "run only the play with this name (matches the play's `name:` field; auto-named plays use `play #N`)")
	f.BoolVar(&c.failFast, "fail-fast", false, "abort the entire run on the first task error. By default, an error aborts only the current play and the next play still runs.")
	f.BoolVar(&c.listTasks, "list-tasks", false, "print the resolved task plan and exit without running. Honors --play / --tags / --skip-tags and shows expanded loop iterations and [skipped] markers for when:-skipped tasks.")
	f.StringVar(&c.startAtTask, "start-at-task", "", "skip every task before the matched name; the matched task and successors run normally. Filter order: --start-at-task -> --tags/--skip-tags -> per-task when: at execution. The name search walks every play in source order, narrowed by --play.")
	f.BoolVar(&c.detailedExitCode, "detailed-exitcode", false, "exit 0 when nothing changed, 2 when at least one task changed, 1 on error. Without this flag apply exits 0 whether or not anything changed.")

	data, format, source := preloadRecipeForFlags(c.baseDir(), c.argv(), true, c.stdinSource())
	if data == nil {
		return f
	}
	c.tasksData = data
	c.tasksDataSource = source

	// The only error registerInputFlags still returns is an unreadable or
	// unparseable recipe, which leaves nothing to register. Run re-resolves the
	// recipe through loadRecipe and reports that properly, with the diagnostics
	// the raw bytes deserve, so swallowing it here costs nothing - the same
	// contract preloadRecipeForFlags already relies on. A malformed *input* no
	// longer reaches this line: it is registered anyway, and rejected by the
	// loader with a positioned diagnostic (#493).
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
			"--tasks":                taskFileAutocomplete(),
			"--tasks-format":         recipeFormatAutocomplete(),
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
			"--detailed-exitcode":    complete.PredictNothing,
		},
	)
}

// Run executes every task in the parsed recipe against the live server,
// printing a one-line summary per task plus a final summary line.
//
// Exit codes (default):
//
//	0 - the run completed without errors, whether or not anything changed
//	1 - read error, parse error, or at least one task errored
//
// Exit codes (--detailed-exitcode):
//
//	0 - the run completed cleanly; nothing changed
//	1 - read error, parse error, or at least one task errored (errors win)
//	2 - the run completed; at least one task changed server state
//
// --list-tasks returns before any task runs, so it is unaffected by
// --detailed-exitcode and still exits 0 or 1.
func (c *ApplyCommand) Run(args []string) int {
	flags := c.FlagSet()
	flags.Usage = func() { c.Ui.Output(c.Help()) }
	if err := flags.Parse(args); err != nil {
		c.Ui.Error(err.Error())
		c.Ui.Error(command.CommandErrorText(c))
		return 1
	}

	varsFileKeys, varsWarnings, err := applyVarsFiles(c.arguments, flags, c.varsFiles)
	if err != nil {
		c.Ui.Error(err.Error())
		return 1
	}
	// Warned about on stderr in every mode, --json included: the JSON stream
	// goes to stdout through the emitter, so this cannot land in it (#489).
	for _, w := range varsWarnings {
		c.Ui.Warn(w)
	}

	ctx := runContext(c.Ctx)
	// The target rides on the run context, so every task planned or executed
	// below routes to the same server without any of them holding a reference
	// to it - and a second run in the same process can carry a different one.
	target := resolveSshFlags(os.Getenv, c.host, c.sudo, c.acceptNewHostKeys)
	ctx = subprocess.ContextWithTarget(ctx, target)

	formatOverride, err := parseRecipeFormatFlag("--tasks-format", c.tasksFormatFlag)
	if err != nil {
		c.Ui.Error(err.Error())
		return 1
	}

	taskFile, err := resolveTaskFileArg(c.tasksFile, flags.Args())
	if err != nil {
		c.Ui.Error(err.Error())
		return 1
	}
	// The cached bytes are the ones FlagSet already read for this source
	// (see tasksData); for a --tasks URL this avoids a second HTTP fetch
	// of the same recipe.
	recipe, err := loadRecipe(c.baseDir(), taskFile, formatOverride, true, c.tasksData, c.tasksDataSource, c.stdinSource())
	if err != nil {
		c.Ui.Error(fmt.Sprintf("read error: %v", err))
		return 1
	}
	if msg := ambiguousTaskFileWarning(recipe.Path, recipe.Ambiguous); msg != "" {
		c.Ui.Warn(msg)
	}
	c.tasksFile = recipe.Path
	c.tasksFormat = recipe.Format
	data := recipe.Data

	userSet := userSetKeys(flags, varsFileKeys, c.arguments)

	inputCtx, sensitiveValues, err := buildInputContext(c.arguments, userSet)
	if err != nil {
		c.Ui.Error(err.Error())
		return 1
	}

	// Register the sensitive CLI/vars-file input values before the recipe is
	// parsed and rendered, so a template or parse error that interpolated one
	// of them is masked. Task-declared sensitive values are added once the
	// recipe parses (below).
	// The masker belongs to this run and goes out of scope with it, so there
	// is no teardown: the deferred clear this replaces is exactly what made a
	// second run in the same process lose its secrets.
	masker := subprocess.NewMasker(sensitiveValues...)
	ctx = subprocess.ContextWithMasker(ctx, masker)

	plays, err := tasks.GetPlaysWithFormat(data, c.tasksFormat, inputCtx, userSet)
	if err != nil {
		c.Ui.Error(masker.String(fmt.Sprintf("task error: %v", err)))
		return 1
	}

	// Compute file-level input names from the *unfiltered* play list so
	// --play does not accidentally hide an inputs-only play whose
	// declared inputs the surviving play's when: depends on.
	fileLevelKeys := tasks.FileLevelInputNames(plays)

	selected, err := filterPlaysByName(masker, plays, c.play)
	if err != nil {
		// The hint names every play in the file, so a value any of their tasks
		// declares sensitive is in scope for this message - unlike the filtered
		// collection below, which deliberately leaves out a play --play
		// excluded. Registering the whole file costs nothing here: this branch
		// prints one line and returns.
		masker.Add(tasks.CollectPlaySensitiveValues(plays)...)
		c.Ui.Error(err.Error())
		return 1
	}
	plays = selected

	// Task-declared sensitive values join the input-level ones registered
	// above, ahead of anything that renders a task name. Both branches below
	// echo names and return without reaching the run loop: the
	// --start-at-task hint lists every available name, and --list-tasks
	// renders the whole resolved plan. Collecting from the filtered play
	// list rather than the whole file keeps a value that is only secret in a
	// play --play excluded from masking output it never appears in. The
	// unmatched --play branch above is the one place that collects from the
	// whole file, and says why.
	masker.Add(tasks.CollectPlaySensitiveValues(plays)...)

	if c.startAtTask != "" {
		if !startAtTaskMatches(plays, c.startAtTask) {
			c.Ui.Error(masker.String(fmt.Sprintf(
				"--start-at-task %q: no task matched name; available names: %s",
				c.startAtTask, formatStartAtTaskNames(masker, plays),
			)))
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
			context:       inputCtx,
			jsonOut:       c.json,
			masker:        masker,
			target:        target,
		})
	}

	// Every distinct host the run will touch, so a recipe whose plays span
	// servers tears down one ControlMaster per server rather than only the
	// run-wide one. controlPath already keys on the host, so the sockets do
	// not collide; nothing was closing the extra ones.
	defer closeControlMasters(target, plays)

	emitter := c.newEmitter(masker)
	start := time.Now()
	counts := ApplyCounts{}
	playWhenExprCtx := buildEnvelopeExprContext(buildPlayWhenContext(inputCtx, fileLevelKeys, userSet))
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
		// Checked per play as well as per task so a cancelled run does not
		// print the header of a play whose tasks will never run.
		if ctx.Err() != nil {
			break playLoop
		}
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

		// A play may send its tasks somewhere other than the run-wide
		// target. Resolving here and deriving a child context is the whole
		// of the per-play routing: the tasks below read the target off the
		// context they are handed and need to know nothing about plays.
		playTarget := play.ResolveTarget(target)
		playCtx := subprocess.ContextWithTarget(ctx, playTarget)

		emitter.PlayStart(play.Name, playTarget.Host)

		playExprCtx := buildEnvelopeExprContext(tasks.BuildPerPlayContext(inputCtx, play.Inputs, userSet))

		ac := &applyContext{
			ctx:         playCtx,
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
		// Iterate the full task list in source order. Tag filtering
		// (--tags/--skip-tags) is applied inside executeTask, *after*
		// the --start-at-task gate, so the documented filter order holds
		// (start-at-task selects the resume point first, then tags
		// narrow). Pre-filtering here would drop the --start-at-task
		// target before the gate could flip `started`, silently no-oping
		// the run.
		for _, name := range play.Tasks.Keys() {
			if ctx.Err() != nil {
				break playLoop
			}
			env := play.Tasks.GetEnvelope(name)
			outcome := c.executeTask(env, name, ac, nil, "")
			if outcome.abort {
				emitter.ApplySummary(counts, time.Since(start))
				return c.runExit(ctx, true, 0)
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

	return c.runExit(ctx, hasError, counts.Changed)
}

// runExit turns the run's verdict into an exit code, and is the one place that
// asks whether the run was cancelled. The question has to be asked here rather
// than tracked by the loop: an interrupt that lands mid-task also makes that
// task fail, so the loop leaves through the error path and never reaches its
// own cancellation check. Reporting the interrupt separately from a task
// failure is what lets an operator tell "I pressed Ctrl-C" from "something
// broke", and a cancelled run never returns the --detailed-exitcode "changed"
// code: a wrapper reads 2 as "the run completed and changed something", and an
// interrupted run completed nothing.
func (c *ApplyCommand) runExit(ctx context.Context, hasError bool, changed int) int {
	if ctx.Err() != nil {
		c.Ui.Error("run cancelled")
		return 1
	}
	if hasError {
		return 1
	}
	if c.detailedExitCode && changed > 0 {
		return 2
	}
	return 0
}

// applyContext bundles the run-wide state apply's per-task helpers
// share so the function signatures stay tractable.
type applyContext struct {
	// ctx is the run context. Every task this play executes is run under it,
	// so cancelling it stops the run rather than only the child process of
	// whichever task happens to be in flight.
	ctx         context.Context
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
//
// Tag filtering (--tags/--skip-tags) runs *after* the gate so the
// documented order holds - start-at-task selects the resume point,
// then tags narrow the survivors. It applies only to top-level
// entries (phase == ""); group children are never tag-filtered. A
// resume-point group (one entered to reach a nested target) bypasses
// the tag filter so the named child stays reachable.
func (c *ApplyCommand) executeTask(env *tasks.TaskEnvelope, name string, ac *applyContext, failedTask interface{}, phase string) applyTaskOutcome {
	// resumeGroup marks a group we descend into to reach a nested
	// --start-at-task target; such a group skips tag filtering below so
	// the descent can land on the matched child regardless of the
	// group's own tags.
	resumeGroup := false
	if !*ac.started && ac.startAtTask != "" {
		switch {
		case name == ac.startAtTask:
			*ac.started = true
		case env.IsGroup() && tasks.EnvelopeContainsName(env, ac.startAtTask):
			// Don't skip; descend into the group so the matching
			// child runs. The synthesized group state will reflect
			// only the executed children, not the skipped ones.
			resumeGroup = true
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
	// Drop a top-level entry the tag filter excludes. This runs after the
	// gate above so the --start-at-task target still flips `started` even
	// when its own tags exclude it. Excluded tasks produce no event and no
	// counts, matching the prior FilterByTags pre-pass.
	if phase == "" && !resumeGroup && !tasks.EnvelopePassesTags(env, c.tags, c.skipTags) {
		return applyTaskOutcome{skipped: true}
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

	if msg := tasks.TaskDeprecation(env.Task); msg != "" {
		ac.emitter.TaskWarning(ac.play.Name, name, "deprecated", msg)
	}

	state := env.Task.Execute(ac.ctx)
	ac.counts.Tasks++

	// Probe diagnostics raised during planning (carried out on the state by
	// ExecutePlan) surface as informational warning lines/events above the
	// task's own result line.
	for _, w := range state.Warnings {
		ac.emitter.TaskWarning(ac.play.Name, name, w.Reason, w.Message)
	}

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
	// outcome is failed AND ignore_errors did not swallow it, or as soon as
	// the run context is cancelled. A
	// swallowed (ignored) child does NOT trigger rescue per #210 rule:
	// ignore_errors is the "swallow" path; rescue is the "handle" path.
	var (
		anyChanged       bool
		blockFailedState *tasks.TaskOutputState
		lastChildState   tasks.TaskOutputState
	)
	for i, child := range env.Block {
		if ac.ctx.Err() != nil {
			break
		}
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
			if ac.ctx.Err() != nil {
				break
			}
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

	// Always children run unconditionally - except under a cancelled run,
	// where every one of them would fail on the dead context the moment it
	// reached a subprocess. Skipping them reports the interrupt once instead
	// of once per cleanup task.
	alwaysErr := error(nil)
	for i, child := range env.Always {
		if ac.ctx.Err() != nil {
			break
		}
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
		// Block failed and there is no rescue clause; the failing
		// child's verdict becomes the group's. A child can fail three
		// ways: with an error, via a state mismatch (nil error), or via
		// a zero-value failed state (a runtime-erroring when/failed_when
		// predicate). Propagate the error and/or state mismatch as-is.
		groupState.Error = blockFailedState.Error
		groupState.State = blockFailedState.State
		groupState.DesiredState = blockFailedState.DesiredState
		if groupState.Error == nil && groupState.State == groupState.DesiredState {
			// The child failed with neither an error nor a state
			// mismatch; synthesize an error so the failure is not
			// silently classified as ok/changed.
			groupState.Error = errors.New("block child failed")
		}
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
	case groupState.State != groupState.DesiredState:
		ignored := env.IgnoreErrors
		if !ignored {
			ac.counts.Errors++
		}
		ac.emitter.ApplyTask(ApplyTaskEvent{
			Play:         ac.play.Name,
			Name:         name,
			Phase:        phase,
			Group:        true,
			State:        groupState,
			InvalidState: true,
			Ignored:      ignored,
			Duration:     time.Since(taskStart),
			Timestamp:    time.Now().UTC(),
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
//
// Each name is masked before it is quoted, not after. `%q` escapes what it
// wraps, and a generated address is already escaped where it quotes a key
// value, so masking the finished message would be matching a literal against
// text that carries the secret escaped twice over - and would miss it (#475).
// Deduplication still keys on the real name: two tasks that mask alike are two
// tasks.
func formatStartAtTaskNames(masker *subprocess.Masker, plays []*tasks.Play) string {
	seen := map[string]bool{}
	var quoted []string
	for _, play := range plays {
		if play == nil {
			continue
		}
		for _, name := range play.Tasks.Keys() {
			if !seen[name] {
				seen[name] = true
				quoted = append(quoted, fmt.Sprintf("%q", masker.String(name)))
			}
			env := play.Tasks.GetEnvelope(name)
			for _, descendant := range tasks.CollectEnvelopeNames([]*tasks.TaskEnvelope{env}) {
				if descendant == "" || descendant == name {
					continue
				}
				if !seen[descendant] {
					seen[descendant] = true
					quoted = append(quoted, fmt.Sprintf("%q", masker.String(descendant)))
				}
			}
		}
	}
	if len(quoted) == 0 {
		return "(none)"
	}
	return strings.Join(quoted, ", ")
}

// formatAvailablePlayNames builds the "available plays: ..." hint for an
// unmatched --play error. Names render in source order, quoted so the user can
// copy one verbatim back onto the CLI.
//
// Each name is masked before it is quoted, for the reason
// formatStartAtTaskNames gives: `%q` escapes what it wraps, so masking the
// finished message would be matching a registered literal against text that
// carries the secret escaped (#477).
func formatAvailablePlayNames(masker *subprocess.Masker, plays []*tasks.Play) string {
	quoted := make([]string, 0, len(plays))
	for _, play := range plays {
		if play == nil {
			continue
		}
		quoted = append(quoted, fmt.Sprintf("%q", masker.String(play.Name)))
	}
	if len(quoted) == 0 {
		return "(none)"
	}
	return strings.Join(quoted, ", ")
}

// unknownPlayError is what filterPlaysByName returns for an unmatched --play.
// It formats lazily rather than at construction so the caller can finish
// populating the mask registry with the recipe's task-declared sensitive
// values first: those are collected from the play list only after the filter
// runs, and a message built eagerly would already have missed them (#477).
type unknownPlayError struct {
	target string
	plays  []*tasks.Play
	// masker is carried on the error because Error() is called from places
	// that have neither a context nor an emitter, and the message names both
	// the requested play and every available one.
	masker *subprocess.Masker
}

func (e *unknownPlayError) Error() string {
	return fmt.Sprintf("--play %q: no play with that name; available plays: %s",
		e.masker.String(e.target), formatAvailablePlayNames(e.masker, e.plays))
}

// filterPlaysByName narrows plays to the single play whose Name matches
// target. An empty target returns plays unchanged. An unmatched target
// returns an error so the user sees a clear "no such play" diagnostic
// rather than silently doing nothing.
func filterPlaysByName(masker *subprocess.Masker, plays []*tasks.Play, target string) ([]*tasks.Play, error) {
	if target == "" {
		return plays, nil
	}
	for _, play := range plays {
		if play != nil && play.Name == target {
			return []*tasks.Play{play}, nil
		}
	}
	return nil, &unknownPlayError{target: target, plays: plays, masker: masker}
}

// newEmitter constructs the EventEmitter for this run. --json builds a
// JSONEmitter; otherwise the human Formatter is used. The verbose flag is
// only meaningful for the human path - JSON output already includes the
// resolved commands in each task event's `commands` array.
func (c *ApplyCommand) newEmitter(masker *subprocess.Masker) EventEmitter {
	if c.json {
		return NewJSONEmitter(c.Ui, masker)
	}
	return NewFormatter(c.Ui, c.verbose, masker)
}

// argv returns the argv this command resolves its pre-parse flags from.
func (c *ApplyCommand) argv() []string { return commandArgv(c.Argv) }

// stdinSource returns this command's memoized standard-input reader, creating
// it on first use. One per command: the recipe is read more than once per
// invocation (FlagSet, Run, and Help on a flag error) but standard input only
// yields its bytes once.
func (c *ApplyCommand) stdinSource() *stdinRecipeSource {
	if c.stdin == nil {
		c.stdin = newStdinRecipeSource(c.Stdin)
	}
	return c.stdin
}

// baseDir returns the directory this command resolves relative paths against.
func (c *ApplyCommand) baseDir() string { return c.BaseDir }
