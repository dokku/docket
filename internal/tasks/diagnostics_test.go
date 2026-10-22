package tasks

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

func TestDiagnosticsClassifiesErrors(t *testing.T) {
	t.Parallel()
	execResponse := subprocess.ExecCommandResponse{
		Command:  "dokku --quiet apps:create web",
		Stdout:   "creating web",
		Stderr:   "name is reserved",
		ExitCode: 1,
	}
	cases := []struct {
		name    string
		err     error
		reason  string
		summary string
		detail  string
	}{
		{
			name:    "version too old",
			err:     &VersionError{TaskType: "dokku_app", Minimum: "0.38.31", Found: "0.30.0"},
			reason:  DiagnosticReasonVersion,
			summary: "dokku 0.38.31 or later is required",
			detail:  "dokku_app requires Dokku >= 0.38.31, server has 0.30.0",
		},
		{
			name:    "plugin too old",
			err:     &VersionError{TaskType: "dokku_letsencrypt", Plugin: "letsencrypt", Minimum: "0.20.0", Found: "0.19.0"},
			reason:  DiagnosticReasonVersion,
			summary: "letsencrypt 0.20.0 or later is required",
			detail:  "dokku_letsencrypt requires letsencrypt >= 0.20.0, server has 0.19.0",
		},
		{
			name:    "version unparseable",
			err:     &VersionError{TaskType: "dokku_app", Minimum: "0.38.31", Found: "master", Err: errors.New("bad")},
			reason:  DiagnosticReasonVersion,
			summary: "the dokku version could not be parsed",
			detail:  `dokku_app requires Dokku >= 0.38.31, but the server version "master" could not be parsed`,
		},
		{
			name:    "version unreadable",
			err:     &VersionError{TaskType: "dokku_app", Minimum: "0.38.31", Err: errors.New("exit status 1"), readFailed: true},
			reason:  DiagnosticReasonVersion,
			summary: "the dokku version could not be read",
			detail:  "dokku_app: checking Dokku version: exit status 1",
		},
		{
			name:    "version wrapped with a hint",
			err:     fmt.Errorf("%w (pass --ignore-version to run it anyway)", &VersionError{TaskType: "dokku_app", Minimum: "0.38.31", Found: "0.30.0"}),
			reason:  DiagnosticReasonVersion,
			summary: "dokku 0.38.31 or later is required",
			detail:  "dokku_app requires Dokku >= 0.38.31, server has 0.30.0 (pass --ignore-version to run it anyway)",
		},
		{
			name:    "object missing",
			err:     &ObjectMissingError{Object: TaskObject{Kind: ObjectApp, Values: []string{"web"}}},
			reason:  DiagnosticReasonObjectMissing,
			summary: "app web does not exist",
		},
		{
			name:    "service missing",
			err:     &ObjectMissingError{Object: TaskObject{Kind: ObjectService, Values: []string{"postgres", "db"}}},
			reason:  DiagnosticReasonObjectMissing,
			summary: "service postgres db does not exist",
		},
		{
			name:    "state unreadable with a reason",
			err:     fmt.Errorf("%w: storage directories cannot be read", ErrStateUnreadable),
			reason:  DiagnosticReasonStateUnreadable,
			summary: "the task's current state cannot be read",
			detail:  "storage directories cannot be read",
		},
		{
			name:    "state unreadable",
			err:     ErrStateUnreadable,
			reason:  DiagnosticReasonStateUnreadable,
			summary: "the task's current state cannot be read",
		},
		{
			name:    "command failed",
			err:     &subprocess.ExecError{Response: execResponse, Err: errors.New(execResponse.Stderr), Ran: true},
			reason:  DiagnosticReasonCommandFailed,
			summary: "the command failed with exit code 1",
			detail:  "name is reserved\n\nstdout:\ncreating web",
		},
		{
			name:    "command not run",
			err:     &subprocess.ExecError{Err: errors.New(`exec: "dokku": executable file not found in $PATH`)},
			reason:  DiagnosticReasonCommandNotRun,
			summary: "the command could not be run",
			detail:  `exec: "dokku": executable file not found in $PATH`,
		},
		{
			name:    "ssh failure",
			err:     &subprocess.SSHError{Host: "deploy@dokku.example.com", Err: errors.New("ssh exited 255"), Stderr: "Permission denied (publickey).\n"},
			reason:  DiagnosticReasonSSH,
			summary: "ssh to deploy@dokku.example.com failed",
			detail:  "ssh deploy@dokku.example.com: Permission denied (publickey).",
		},
		{
			name:    "version unreadable over ssh",
			err:     &VersionError{TaskType: "dokku_app", Minimum: "0.38.31", Err: &subprocess.SSHError{Host: "dokku.example.com", Err: errors.New("ssh exited 255")}, readFailed: true},
			reason:  DiagnosticReasonSSH,
			summary: "ssh to dokku.example.com failed",
			detail:  "dokku_app: checking Dokku version: ssh dokku.example.com: ssh exited 255",
		},
		{
			name:    "generic",
			err:     errors.New("first line\nsecond line"),
			reason:  DiagnosticReasonError,
			summary: "first line",
			detail:  "first line\nsecond line",
		},
		{
			name:    "generic one line",
			err:     errors.New("boom"),
			reason:  DiagnosticReasonError,
			summary: "boom",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := Diagnostics(context.Background(), tc.err)
			if len(got) != 1 {
				t.Fatalf("Diagnostics returned %d entries, want 1: %+v", len(got), got)
			}
			d := got[0]
			if d.Severity != DiagnosticError {
				t.Errorf("Severity = %q, want %q", d.Severity, DiagnosticError)
			}
			if d.Reason != tc.reason {
				t.Errorf("Reason = %q, want %q", d.Reason, tc.reason)
			}
			if d.Summary != tc.summary {
				t.Errorf("Summary = %q, want %q", d.Summary, tc.summary)
			}
			if d.Detail != tc.detail {
				t.Errorf("Detail = %q, want %q", d.Detail, tc.detail)
			}
			if d.Message != tc.err.Error() {
				t.Errorf("Message = %q, want err.Error() %q", d.Message, tc.err.Error())
			}
		})
	}
}

// A bare error carries its command's output from the *ExecError it wraps,
// so a caller of ReadTask sees the exit code and stderr too.
func TestDiagnosticsErrorCarriesCommandOutput(t *testing.T) {
	t.Parallel()
	res := subprocess.ExecCommandResponse{Stdout: "out", Stderr: "err", ExitCode: 3}
	err := fmt.Errorf("probing: %w", &subprocess.ExecError{Response: res, Err: errors.New("err"), Ran: true})
	d := Diagnostics(context.Background(), err)[0]
	if d.ExitCode != 3 || d.Stdout != "out" || d.Stderr != "err" {
		t.Errorf("output = (%d, %q, %q), want (3, %q, %q)", d.ExitCode, d.Stdout, d.Stderr, "out", "err")
	}
	if d.Summary != "the command failed with exit code 3" {
		t.Errorf("Summary = %q", d.Summary)
	}
}

// A result's own output fields win over the error's: they are what the CLI
// has always printed for a failed task.
func TestDiagnosticsTaskOutputState(t *testing.T) {
	t.Parallel()
	res := subprocess.ExecCommandResponse{Command: "dokku apps:create web", Stdout: "partial\n", Stderr: "name is reserved\n", ExitCode: 1}
	state := TaskOutputErrorFromExec(TaskOutputState{}, &subprocess.ExecError{Response: res, Err: errors.New(res.Stderr), Ran: true}, res)
	state.Warnings = []PlanWarning{{Reason: WarnReasonProbeRejected, Message: "report rejected"}}

	for _, input := range []any{state, &state} {
		got := Diagnostics(context.Background(), input)
		if len(got) != 2 {
			t.Fatalf("Diagnostics(%T) returned %d entries, want 2: %+v", input, len(got), got)
		}
		want := Diagnostic{
			Severity: DiagnosticError,
			Reason:   DiagnosticReasonCommandFailed,
			Summary:  "the command failed with exit code 1",
			Detail:   "name is reserved\n\nstdout:\npartial",
			Message:  "name is reserved\n",
			ExitCode: 1,
			Stdout:   "partial\n",
			Stderr:   "name is reserved\n",
		}
		if !reflect.DeepEqual(got[0], want) {
			t.Errorf("error diagnostic = %+v\nwant %+v", got[0], want)
		}
		if got[1].Severity != DiagnosticWarning || got[1].Reason != WarnReasonProbeRejected {
			t.Errorf("warning diagnostic = %+v", got[1])
		}
	}
}

// A plan's error comes first, then each warning in order with its reason.
func TestDiagnosticsPlanResult(t *testing.T) {
	t.Parallel()
	result := PlanResult{
		Status: PlanStatusError,
		Error:  &ObjectMissingError{Object: TaskObject{Kind: ObjectApp, Values: []string{"web"}}},
		Warnings: []PlanWarning{
			{Reason: WarnReasonUnknownProperty, Message: "no key \"foo\" in the report"},
			{Reason: WarnReasonServiceOptionDrift, Message: "shm_size is \"64m\", recipe declares \"128m\""},
			{Reason: "something_new", Message: "first\nsecond"},
		},
	}
	for _, input := range []any{result, &result} {
		got := Diagnostics(context.Background(), input)
		want := []Diagnostic{
			{Severity: DiagnosticError, Reason: DiagnosticReasonObjectMissing, Summary: "app web does not exist", Message: "app web does not exist"},
			{Severity: DiagnosticWarning, Reason: WarnReasonUnknownProperty, Summary: "a property is missing from the plugin's report", Detail: "no key \"foo\" in the report", Message: "no key \"foo\" in the report"},
			{Severity: DiagnosticWarning, Reason: WarnReasonServiceOptionDrift, Summary: "the service holds a create-time option the task does not declare", Detail: "shm_size is \"64m\", recipe declares \"128m\"", Message: "shm_size is \"64m\", recipe declares \"128m\""},
			{Severity: DiagnosticWarning, Reason: "something_new", Summary: "first", Detail: "first\nsecond", Message: "first\nsecond"},
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Diagnostics(%T) =\n%+v\nwant\n%+v", input, got, want)
		}
	}
}

// Every reason docket raises has a summary of its own.
func TestDiagnosticsEveryWarnReasonHasASummary(t *testing.T) {
	t.Parallel()
	for _, reason := range []string{WarnReasonUnknownProperty, WarnReasonProbeRejected, WarnReasonProbeIndeterminate, WarnReasonServiceImageDrift, WarnReasonServiceOptionDrift} {
		if warningSummaries[reason] == "" {
			t.Errorf("warning reason %q has no summary", reason)
		}
	}
}

func TestDiagnosticsNothingToReport(t *testing.T) {
	t.Parallel()
	var nilPlan *PlanResult
	var nilState *TaskOutputState
	var nilErr error
	for _, input := range []any{nil, nilErr, nilPlan, nilState, PlanResult{InSync: true}, TaskOutputState{Changed: true}} {
		if got := Diagnostics(context.Background(), input); got != nil {
			t.Errorf("Diagnostics(%#v) = %+v, want nil", input, got)
		}
	}
}

func TestDiagnosticsUnsupportedInput(t *testing.T) {
	t.Parallel()
	got := Diagnostics(context.Background(), 42)
	if len(got) != 1 || got[0].Severity != DiagnosticError || got[0].Summary != "Diagnostics cannot read a int" {
		t.Errorf("Diagnostics(42) = %+v", got)
	}
}

// TestDiagnosticsMasksEveryField puts a declared secret and one the server
// returned into every string a result carries, and checks no diagnostic field
// shows either.
func TestDiagnosticsMasksEveryField(t *testing.T) {
	t.Parallel()
	const declared = "declared-s3cr3t"
	const served = "served-t0ken"
	masker := subprocess.NewMasker(declared)
	ctx := subprocess.ContextWithMasker(context.Background(), masker)
	// A probe registers what it reads back as secret during the plan, after
	// the task's own values.
	masker.Add(served)

	both := declared + " " + served
	res := subprocess.ExecCommandResponse{Command: "dokku config:set web K=" + both, Stdout: "out " + both, Stderr: "err " + both, ExitCode: 1}
	warnings := []PlanWarning{
		{Reason: WarnReasonProbeRejected, Message: "rejected " + both},
		{Reason: "unlisted", Message: both + "\n" + both},
	}
	errs := []error{
		&VersionError{TaskType: "dokku_" + both, Plugin: both, Minimum: both, Found: both},
		&VersionError{TaskType: both, Found: both, Err: errors.New(both)},
		&VersionError{TaskType: both, Err: errors.New(both), readFailed: true},
		&ObjectMissingError{Object: TaskObject{Kind: ObjectService, Values: []string{both, both}}},
		fmt.Errorf("%w: %s", ErrStateUnreadable, both),
		&subprocess.ExecError{Response: res, Err: errors.New(res.Stderr), Ran: true},
		&subprocess.ExecError{Response: res, Err: errors.New(both)},
		&subprocess.SSHError{Host: both, Command: []string{both}, Err: errors.New(both), Stderr: both},
		errors.New(both + "\n" + both),
	}
	var inputs []any
	for _, err := range errs {
		inputs = append(inputs,
			err,
			PlanResult{Status: PlanStatusError, Reason: both, Error: err, Stdout: res.Stdout, Stderr: res.Stderr, ExitCode: 1, Warnings: warnings},
			TaskOutputState{Error: err, Message: both, Stdout: res.Stdout, Stderr: res.Stderr, ExitCode: 1, Commands: []string{res.Command}, Warnings: warnings},
		)
	}
	for _, input := range inputs {
		got := Diagnostics(ctx, input)
		if len(got) == 0 {
			t.Fatalf("Diagnostics(%#v) returned nothing", input)
		}
		for _, d := range got {
			v := reflect.ValueOf(d)
			for i := 0; i < v.NumField(); i++ {
				if v.Field(i).Kind() != reflect.String {
					continue
				}
				s := v.Field(i).String()
				if strings.Contains(s, declared) || strings.Contains(s, served) {
					t.Errorf("Diagnostic.%s leaks a secret: %q (input %T)", v.Type().Field(i).Name, s, input)
				}
			}
		}
	}
}
