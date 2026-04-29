package commands

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dokku/docket/subprocess"
	"github.com/fatih/color"
	"github.com/mitchellh/cli"
)

// newTestFormatter returns a formatter wired to a fresh MockUi with
// color forced off. Tests that need colored output set f.color = true
// explicitly.
func newTestFormatter(verbose bool) (*Formatter, *cli.MockUi) {
	ui := cli.NewMockUi()
	f := NewFormatter(ui, verbose)
	f.color = false
	return f, ui
}

func TestFormatterPlayHeader(t *testing.T) {
	f, ui := newTestFormatter(false)
	f.PlayHeader("tasks")
	got := ui.OutputWriter.String()
	want := "==> Play: tasks\n"
	if got != want {
		t.Errorf("PlayHeader output mismatch\nwant: %q\ngot:  %q", want, got)
	}
}

func TestFormatterPlayHeaderWithHost(t *testing.T) {
	f, ui := newTestFormatter(false)
	f.PlayHeaderWithHost("tasks", "alice@host:2222")
	got := ui.OutputWriter.String()
	want := "==> Play: tasks  (host: alice@host:2222)\n"
	if got != want {
		t.Errorf("PlayHeaderWithHost output mismatch\nwant: %q\ngot:  %q", want, got)
	}
}

func TestFormatterPlayHeaderWithHostEmptyDelegates(t *testing.T) {
	f, ui := newTestFormatter(false)
	f.PlayHeaderWithHost("tasks", "")
	got := ui.OutputWriter.String()
	want := "==> Play: tasks\n"
	if got != want {
		t.Errorf("PlayHeaderWithHost empty host\nwant: %q\ngot:  %q", want, got)
	}
}

func TestFormatterErrorContinuationDokkuPrefix(t *testing.T) {
	f, ui := newTestFormatter(false)
	f.ErrorContinuation(errors.New("app foo does not exist"))
	got := ui.OutputWriter.String()
	want := "          ! dokku: app foo does not exist\n"
	if got != want {
		t.Errorf("ErrorContinuation dokku\nwant: %q\ngot:  %q", want, got)
	}
}

func TestFormatterErrorContinuationSshPrefix(t *testing.T) {
	f, ui := newTestFormatter(false)
	f.ErrorContinuation(&subprocess.SSHError{Host: "alice@host", Stderr: "Permission denied (publickey)."})
	got := ui.OutputWriter.String()
	want := "          ! ssh: ssh alice@host: Permission denied (publickey).\n"
	if got != want {
		t.Errorf("ErrorContinuation ssh\nwant: %q\ngot:  %q", want, got)
	}
}

func TestFormatterErrorContinuationSshWrappedPrefix(t *testing.T) {
	wrapped := &subprocess.SSHError{Host: "host", Err: errors.New("connect refused")}
	f, ui := newTestFormatter(false)
	f.ErrorContinuation(wrapped)
	got := ui.OutputWriter.String()
	if !strings.HasPrefix(strings.TrimLeft(got, " "), "! ssh:") {
		t.Errorf("expected ssh: prefix, got %q", got)
	}
}

func TestFormatterErrorContinuationNilNoOp(t *testing.T) {
	f, ui := newTestFormatter(false)
	f.ErrorContinuation(nil)
	if got := ui.OutputWriter.String(); got != "" {
		t.Errorf("nil error should produce no output, got %q", got)
	}
}

func TestPrefixErrorMessage(t *testing.T) {
	if got := PrefixErrorMessage(nil); got != "" {
		t.Errorf("nil should return empty, got %q", got)
	}
	if got := PrefixErrorMessage(errors.New("boom")); got != "dokku: boom" {
		t.Errorf("plain err\ngot:  %q\nwant: %q", got, "dokku: boom")
	}
	sshErr := &subprocess.SSHError{Host: "h", Err: errors.New("boom")}
	if got := PrefixErrorMessage(sshErr); !strings.HasPrefix(got, "ssh:") {
		t.Errorf("ssh err should be prefixed `ssh:`, got %q", got)
	}
}

func TestFormatterTaskLineMarkerPadding(t *testing.T) {
	cases := []struct {
		marker Marker
		want   string
	}{
		{MarkerOK, "[ok]      task-name\n"},
		{MarkerChanged, "[changed] task-name\n"},
		{MarkerSkipped, "[skipped] task-name\n"},
		{MarkerCreate, "[+]       task-name\n"},
		{MarkerModify, "[~]       task-name\n"},
		{MarkerDestroy, "[-]       task-name\n"},
	}
	for _, tc := range cases {
		t.Run(string(tc.marker), func(t *testing.T) {
			f, ui := newTestFormatter(false)
			f.TaskLine(tc.marker, "task-name", "")
			got := ui.OutputWriter.String()
			if got != tc.want {
				t.Errorf("marker %q\nwant: %q\ngot:  %q", tc.marker, tc.want, got)
			}
		})
	}
}

func TestFormatterTaskLineWithSuffix(t *testing.T) {
	f, ui := newTestFormatter(false)
	f.TaskLine(MarkerOK, "configure", "(in sync)")
	got := ui.OutputWriter.String()
	want := "[ok]      configure  (in sync)\n"
	if got != want {
		t.Errorf("TaskLine with suffix\nwant: %q\ngot:  %q", want, got)
	}
}

func TestFormatterTaskLineErrorRoutesToStderr(t *testing.T) {
	f, ui := newTestFormatter(false)
	f.TaskLine(MarkerError, "task-name", "")
	if got := ui.OutputWriter.String(); got != "" {
		t.Errorf("error marker should not write to stdout, got %q", got)
	}
	want := "[error]   task-name\n"
	if got := ui.ErrorWriter.String(); got != want {
		t.Errorf("error marker stderr\nwant: %q\ngot:  %q", want, got)
	}
}

func TestFormatterTaskLineProbeErrorRoutesToStderr(t *testing.T) {
	f, ui := newTestFormatter(false)
	f.TaskLine(MarkerProbeError, "task-name", "(probe failed)")
	if got := ui.OutputWriter.String(); got != "" {
		t.Errorf("probe-error marker should not write to stdout, got %q", got)
	}
	want := "[!]       task-name  (probe failed)\n"
	if got := ui.ErrorWriter.String(); got != want {
		t.Errorf("probe-error stderr\nwant: %q\ngot:  %q", want, got)
	}
}

func TestFormatterContinuationBangPrefix(t *testing.T) {
	f, ui := newTestFormatter(false)
	f.Continuation('!', "app foo does not exist")
	got := ui.OutputWriter.String()
	want := "          ! app foo does not exist\n"
	if got != want {
		t.Errorf("Continuation\nwant: %q\ngot:  %q", want, got)
	}
}

func TestFormatterContinuationVerboseArrow(t *testing.T) {
	f, ui := newTestFormatter(true)
	f.Continuation('\u2192', "dokku --quiet apps:create foo")
	got := ui.OutputWriter.String()
	want := "          \u2192 dokku --quiet apps:create foo\n"
	if got != want {
		t.Errorf("verbose continuation\nwant: %q\ngot:  %q", want, got)
	}
}

func TestFormatterContinuationSkipsEmpty(t *testing.T) {
	f, ui := newTestFormatter(false)
	f.Continuation('!', "")
	if got := ui.OutputWriter.String(); got != "" {
		t.Errorf("empty body should produce no output, got %q", got)
	}
}

func TestFormatterContinuationMultiLine(t *testing.T) {
	f, ui := newTestFormatter(false)
	f.Continuation('!', "line one\nline two")
	got := ui.OutputWriter.String()
	want := "          ! line one\n          ! line two\n"
	if got != want {
		t.Errorf("multi-line continuation\nwant: %q\ngot:  %q", want, got)
	}
}

func TestFormatterApplySummary(t *testing.T) {
	f, ui := newTestFormatter(false)
	f.ApplySummary(ApplyCounts{Tasks: 3, Changed: 1, OK: 2}, 3200*time.Millisecond)
	got := ui.OutputWriter.String()
	wantContains := []string{
		"Summary: 3 tasks · 1 changed · 2 ok · 0 skipped · 0 errors",
		"(took 3.2s)",
	}
	for _, want := range wantContains {
		if !strings.Contains(got, want) {
			t.Errorf("ApplySummary missing %q in:\n%s", want, got)
		}
	}
	if !strings.HasPrefix(got, "\n") {
		t.Errorf("ApplySummary should start with a blank line; got:\n%s", got)
	}
}

func TestFormatterApplySummaryErrorWord(t *testing.T) {
	f, ui := newTestFormatter(false)
	f.ApplySummary(ApplyCounts{Tasks: 1, Errors: 1}, time.Second)
	got := ui.OutputWriter.String()
	if !strings.Contains(got, "1 error  ") {
		t.Errorf("singular `1 error`, got:\n%s", got)
	}

	f2, ui2 := newTestFormatter(false)
	f2.ApplySummary(ApplyCounts{Tasks: 2, Errors: 2}, time.Second)
	got2 := ui2.OutputWriter.String()
	if !strings.Contains(got2, "2 errors") {
		t.Errorf("plural `2 errors`, got:\n%s", got2)
	}
}

func TestFormatterPlanSummary(t *testing.T) {
	f, ui := newTestFormatter(false)
	f.PlanSummary(PlanCounts{Tasks: 3, WouldChange: 2, InSync: 1, Errors: 0}, 0)
	got := ui.OutputWriter.String()
	want := "Plan: 3 task(s); 2 would change, 1 in sync, 0 error(s)."
	if !strings.Contains(got, want) {
		t.Errorf("PlanSummary missing %q in:\n%s", want, got)
	}
}

func TestFormatterPlanSummaryWithSkipped(t *testing.T) {
	// The skipped count is appended only when at least one task was
	// skipped by `when:` so recipes that do not exercise envelope
	// predicates keep the legacy summary shape (covered above).
	f, ui := newTestFormatter(false)
	f.PlanSummary(PlanCounts{Tasks: 3, WouldChange: 1, InSync: 1, Skipped: 1, Errors: 0}, 0)
	got := ui.OutputWriter.String()
	want := "Plan: 3 task(s); 1 would change, 1 in sync, 1 skipped, 0 error(s)."
	if !strings.Contains(got, want) {
		t.Errorf("PlanSummary with skipped missing %q in:\n%s", want, got)
	}
}

func TestFormatterColorOffProducesPlainOutput(t *testing.T) {
	f, ui := newTestFormatter(false) // color forced off
	f.TaskLine(MarkerChanged, "task", "")
	got := ui.OutputWriter.String()
	if strings.Contains(got, "\x1b[") {
		t.Errorf("color off should not emit ANSI escapes, got %q", got)
	}
}

func TestFormatterColorOnEmitsAnsi(t *testing.T) {
	// Force color on regardless of TTY/NO_COLOR detection.
	prev := color.NoColor
	color.NoColor = false
	t.Cleanup(func() { color.NoColor = prev })

	ui := cli.NewMockUi()
	f := NewFormatter(ui, false)
	f.color = true
	f.TaskLine(MarkerChanged, "task", "")
	got := ui.OutputWriter.String()
	if !strings.Contains(got, "\x1b[") {
		t.Errorf("color on should emit ANSI escapes, got %q", got)
	}
	if !strings.Contains(got, "[changed]") {
		t.Errorf("colored output should still contain marker text, got %q", got)
	}
}

func TestFormatterVerboseAccessor(t *testing.T) {
	f, _ := newTestFormatter(true)
	if !f.Verbose() {
		t.Error("Verbose() should be true when constructed with verbose=true")
	}
	f2, _ := newTestFormatter(false)
	if f2.Verbose() {
		t.Error("Verbose() should be false when constructed with verbose=false")
	}
}
