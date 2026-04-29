package commands

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dokku/docket/subprocess"
	"github.com/dokku/docket/tasks"
	"github.com/fatih/color"
	"github.com/mitchellh/cli"
)

// EventEmitter is the structured event sink consumed by `apply` and `plan`.
// Both the human Formatter and the JSONEmitter implement it. The executor
// in apply.go / plan.go constructs the right emitter at the top of Run()
// based on the --json flag and funnels each per-task branch through the
// interface so the two output modes stay in lock-step.
type EventEmitter interface {
	// PlayStart announces the beginning of a play. host is the optional
	// remote host annotation, "" for local execution.
	PlayStart(name, host string)
	// ApplyTask emits one event per task in an `apply` run.
	ApplyTask(ev ApplyTaskEvent)
	// PlanTask emits one event per task in a `plan` run.
	PlanTask(ev PlanTaskEvent)
	// ApplySummary emits the end-of-run footer for `apply`.
	ApplySummary(c ApplyCounts, d time.Duration)
	// PlanSummary emits the end-of-run footer for `plan`.
	PlanSummary(c PlanCounts, d time.Duration)
}

// ApplyTaskEvent describes a single task outcome from an apply run. The
// run-loop in commands/apply.go populates this once per task and hands it
// to the active emitter; the emitter decides how to render it.
type ApplyTaskEvent struct {
	// Play is the name of the enclosing play (today always "tasks").
	Play string
	// Name is the task's envelope name.
	Name string
	// State is the post-execute state returned by Task.Execute.
	// Zero-value State is acceptable for the WhenError / Skipped branches.
	State tasks.TaskOutputState
	// WhenError, when non-nil, indicates the `when:` predicate raised an
	// expr error and the task did not run. Mutually exclusive with the
	// other branches.
	WhenError error
	// Skipped indicates the `when:` predicate evaluated to false and the
	// task was filtered out. Mutually exclusive with the other branches.
	Skipped bool
	// InvalidState, when true, indicates Execute reported success but the
	// final State did not match DesiredState; treated as an error in
	// counts and exit logic.
	InvalidState bool
	// Duration is the wall-clock time Execute took (or zero for the
	// when-skipped / when-error branches).
	Duration time.Duration
	// Timestamp is when the event was produced (UTC). The JSON emitter
	// serialises this as the `ts` field; the human emitter ignores it.
	Timestamp time.Time
}

// PlanTaskEvent describes a single task outcome from a plan run.
type PlanTaskEvent struct {
	Play      string
	Name      string
	Result    tasks.PlanResult
	WhenError error
	Skipped   bool
	Duration  time.Duration
	Timestamp time.Time
}

// Marker is the bracketed status marker that prefixes a task line. The
// concrete strings (without brackets) match the conventions documented
// in issue #202: apply uses ok/changed/skipped/error, plan uses
// ok/+/~/-/!.
type Marker string

const (
	// Apply markers
	MarkerOK      Marker = "ok"
	MarkerChanged Marker = "changed"
	MarkerSkipped Marker = "skipped"
	MarkerError   Marker = "error"

	// Plan markers (PlanStatusOK reuses MarkerOK)
	MarkerCreate     Marker = "+"
	MarkerModify     Marker = "~"
	MarkerDestroy    Marker = "-"
	MarkerProbeError Marker = "!"
)

// markerWidth is the fixed column width that every bracketed marker is
// padded to. Wide enough for the longest marker `[changed]` (9 chars)
// plus a trailing space.
const markerWidth = 10

// continuationIndent matches markerWidth so continuation lines align
// under the task name column.
var continuationIndent = strings.Repeat(" ", markerWidth)

// ApplyCounts holds the running totals printed by the apply summary
// line.
type ApplyCounts struct {
	Tasks   int
	Changed int
	OK      int
	Skipped int
	Errors  int
}

// PlanCounts holds the running totals printed by the plan summary line.
type PlanCounts struct {
	Tasks       int
	WouldChange int
	InSync      int
	Skipped     int
	Errors      int
}

// Formatter renders the structured per-task output for `apply` and
// `plan`. It owns marker padding, color decisions, and the play /
// summary line shapes so both subcommands stay visually consistent.
//
// Color is resolved once at construction and honours NO_COLOR plus
// stdout-isatty, matching fatih/color's defaults. When the active Ui
// is not the real terminal Ui (e.g. cli.MockUi in tests), color is
// also forced off so test assertions stay stable.
type Formatter struct {
	ui      cli.Ui
	verbose bool
	color   bool
	paint   map[Marker]*color.Color
}

// NewFormatter constructs a Formatter bound to the given Ui. verbose
// controls whether `→`-prefixed continuation lines are emitted under
// each task line in apply mode.
func NewFormatter(ui cli.Ui, verbose bool) *Formatter {
	useColor := !noColorDefault()
	return &Formatter{
		ui:      ui,
		verbose: verbose,
		color:   useColor,
		paint: map[Marker]*color.Color{
			MarkerOK:         color.New(color.FgGreen),
			MarkerChanged:    color.New(color.FgYellow),
			MarkerSkipped:    color.New(color.Faint),
			MarkerError:      color.New(color.FgRed),
			MarkerCreate:     color.New(color.FgGreen),
			MarkerModify:     color.New(color.FgYellow),
			MarkerDestroy:    color.New(color.FgRed),
			MarkerProbeError: color.New(color.FgRed),
		},
	}
}

// Verbose reports whether the formatter is in verbose mode.
func (f *Formatter) Verbose() bool { return f.verbose }

// PlayHeader emits a `==> Play: <name>` line above the per-task
// listing. Used once per play; until #208 lands, callers emit one
// header per run.
func (f *Formatter) PlayHeader(name string) {
	f.ui.Output(fmt.Sprintf("==> Play: %s", name))
}

// PlayHeaderWithHost is like PlayHeader but appends a `(host: <name>)`
// annotation when the apply/plan run targets a remote dokku host
// (DOKKU_HOST set). When host is empty it delegates to PlayHeader so
// callers can pass `os.Getenv("DOKKU_HOST")` unconditionally.
func (f *Formatter) PlayHeaderWithHost(name, host string) {
	if host == "" {
		f.PlayHeader(name)
		return
	}
	f.ui.Output(fmt.Sprintf("==> Play: %s  (host: %s)", name, host))
}

// PlayStart satisfies EventEmitter; delegates to PlayHeaderWithHost.
func (f *Formatter) PlayStart(name, host string) {
	f.PlayHeaderWithHost(name, host)
}

// ApplyTask renders one apply task line plus optional continuations,
// matching the legacy formatter dispatch in commands/apply.go.
func (f *Formatter) ApplyTask(ev ApplyTaskEvent) {
	switch {
	case ev.WhenError != nil:
		f.TaskLine(MarkerError, ev.Name, "")
		f.Continuation('!', fmt.Sprintf("when expression error: %v", ev.WhenError))
	case ev.Skipped:
		f.TaskLine(MarkerSkipped, ev.Name, "")
	case ev.State.Error != nil:
		f.TaskLine(MarkerError, ev.Name, "")
		f.ErrorContinuation(ev.State.Error)
		if f.verbose {
			for _, cmd := range ev.State.Commands {
				f.Continuation('\u2192', cmd)
			}
		}
	case ev.InvalidState:
		f.TaskLine(MarkerError, ev.Name, "")
		f.Continuation('!', fmt.Sprintf("invalid state: expected=%v actual=%v", ev.State.DesiredState, ev.State.State))
	case ev.State.Changed:
		f.TaskLine(MarkerChanged, ev.Name, "")
		if f.verbose {
			for _, cmd := range ev.State.Commands {
				f.Continuation('\u2192', cmd)
			}
		}
	default:
		f.TaskLine(MarkerOK, ev.Name, "")
		if f.verbose {
			for _, cmd := range ev.State.Commands {
				f.Continuation('\u2192', cmd)
			}
		}
	}
}

// PlanTask renders one plan task line plus optional continuations,
// matching the legacy formatter dispatch in commands/plan.go.
func (f *Formatter) PlanTask(ev PlanTaskEvent) {
	switch {
	case ev.WhenError != nil:
		f.TaskLine(MarkerProbeError, ev.Name, fmt.Sprintf("(when expression error: %v)", ev.WhenError))
	case ev.Skipped:
		f.TaskLine(MarkerSkipped, ev.Name, "(when: false)")
	case ev.Result.Error != nil:
		f.TaskLine(MarkerProbeError, ev.Name, fmt.Sprintf("(%s)", PrefixErrorMessage(ev.Result.Error)))
	case ev.Result.InSync:
		f.TaskLine(MarkerOK, ev.Name, "(in sync)")
	default:
		marker := Marker(ev.Result.Status)
		if marker == "" {
			marker = Marker(tasks.PlanStatusModify)
		}
		suffix := ""
		if ev.Result.Reason != "" {
			suffix = "(" + ev.Result.Reason + ")"
		}
		f.TaskLine(marker, ev.Name, suffix)
		for _, m := range ev.Result.Mutations {
			f.Continuation('-', m)
		}
	}
}

// ErrorContinuation emits an `! <prefix>: <body>` continuation line
// under an errored task. The prefix is `ssh` when err unwraps to a
// *subprocess.SSHError (transport-level failure: connect, auth,
// host-key) and `dokku` otherwise (remote command exit). Multi-line
// bodies are masked and rendered as separate continuation lines under
// the same indent.
func (f *Formatter) ErrorContinuation(err error) {
	if err == nil {
		return
	}
	f.Continuation('!', PrefixErrorMessage(err))
}

// PrefixErrorMessage prepends `ssh:` or `dokku:` to err.Error() based
// on whether err unwraps to a *subprocess.SSHError. Exported so the
// plan command can reuse the same prefixing logic for the probe-error
// suffix that surfaces inline in the TaskLine instead of in a
// continuation line.
func PrefixErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	var sshErr *subprocess.SSHError
	if errors.As(err, &sshErr) {
		return "ssh: " + err.Error()
	}
	return "dokku: " + err.Error()
}

// TaskLine emits one structured task line: a colored, bracketed,
// padded marker followed by the task name. suffix, when non-empty, is
// appended after two spaces (matching the legacy plan-line layout).
//
// Errored task lines are routed through Ui.Error so they land on stderr
// and inherit the cli-skeleton error styling. The suffix is masked
// against the global sensitive value set so error contexts that include
// stderr can't leak secrets.
func (f *Formatter) TaskLine(m Marker, name, suffix string) {
	marker := f.paintMarker(m)
	line := marker + name
	if suffix != "" {
		line = line + "  " + subprocess.MaskString(suffix)
	}
	if m == MarkerError || m == MarkerProbeError {
		f.ui.Error(line)
		return
	}
	f.ui.Output(line)
}

// Continuation emits an indented continuation line under the most
// recent task line. prefix is the leading rune (`!` for errors,
// `→` for the verbose command echo, `-` for plan mutation items).
// Each line of `body` is emitted as its own continuation so multi-line
// stderr renders cleanly under the marker column. The body is masked
// against the global sensitive value set before output.
func (f *Formatter) Continuation(prefix rune, body string) {
	if body == "" {
		return
	}
	body = subprocess.MaskString(body)
	for _, line := range strings.Split(strings.TrimRight(body, "\n"), "\n") {
		out := fmt.Sprintf("%s%c %s", continuationIndent, prefix, line)
		f.ui.Output(out)
	}
}

// ApplySummary emits the blank line and `Summary: ...` footer that
// follows an apply run. elapsed is rendered with one decimal of
// seconds, matching the spec in issue #202.
func (f *Formatter) ApplySummary(c ApplyCounts, elapsed time.Duration) {
	errWord := "errors"
	if c.Errors == 1 {
		errWord = "error"
	}
	f.ui.Output("")
	f.ui.Output(fmt.Sprintf(
		"Summary: %d tasks · %d changed · %d ok · %d skipped · %d %s  (took %.1fs)",
		c.Tasks, c.Changed, c.OK, c.Skipped, c.Errors, errWord, elapsed.Seconds(),
	))
}

// PlanSummary emits the blank line and `Plan: ...` footer that
// follows a plan run. The format intentionally matches the legacy
// shape so existing CI consumers (and the bats partial-match
// assertions in tests/bats/plan.bats) keep working. The skipped
// count is appended only when at least one task was filtered out by
// `when:`, so recipes that do not exercise envelope predicates still
// produce the legacy summary.
//
// The elapsed duration is accepted for parity with ApplySummary and
// EventEmitter, but the human plan summary line does not render it
// (the legacy format omits timing). The JSON emitter consumes it.
func (f *Formatter) PlanSummary(c PlanCounts, _ time.Duration) {
	f.ui.Output("")
	if c.Skipped > 0 {
		f.ui.Output(fmt.Sprintf(
			"Plan: %d task(s); %d would change, %d in sync, %d skipped, %d error(s).",
			c.Tasks, c.WouldChange, c.InSync, c.Skipped, c.Errors,
		))
		return
	}
	f.ui.Output(fmt.Sprintf(
		"Plan: %d task(s); %d would change, %d in sync, %d error(s).",
		c.Tasks, c.WouldChange, c.InSync, c.Errors,
	))
}

// paintMarker pads the bracketed marker text to markerWidth and applies
// color when enabled.
func (f *Formatter) paintMarker(m Marker) string {
	text := fmt.Sprintf("[%s]", string(m))
	pad := markerWidth - len(text)
	if pad < 1 {
		pad = 1
	}
	padded := text + strings.Repeat(" ", pad)
	if !f.color {
		return padded
	}
	if c, ok := f.paint[m]; ok {
		// Only color the bracketed text; keep trailing whitespace
		// uncolored so the indent is unaffected by ANSI resets.
		return c.Sprint(text) + strings.Repeat(" ", pad)
	}
	return padded
}
