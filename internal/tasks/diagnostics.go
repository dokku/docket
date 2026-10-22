package tasks

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dokku/docket/internal/subprocess"
)

// DiagnosticSeverity is how serious a Diagnostic is.
type DiagnosticSeverity string

// Diagnostic severities, as Diagnostic.Severity.
//
//sdk:export
const (
	// DiagnosticError marks a diagnostic for an error: the task could not be
	// planned, applied or read.
	DiagnosticError DiagnosticSeverity = "error"
	// DiagnosticWarning marks a diagnostic for a PlanWarning: the task ran,
	// and something about it needs a look.
	DiagnosticWarning DiagnosticSeverity = "warning"
)

// Diagnostic reasons for an error, as Diagnostic.Reason. A warning's Reason is
// its PlanWarning.Reason, one of the WarnReason* constants.
const (
	// DiagnosticReasonVersion marks an error for a *VersionError: the server,
	// or a plugin the task needs, is older than the task's floor, or its
	// version could not be read or parsed.
	//
	//sdk:export
	DiagnosticReasonVersion = "version"
	// DiagnosticReasonObjectMissing marks an error for an
	// *ObjectMissingError: the app or service the task depends on does not
	// exist.
	//
	//sdk:export
	DiagnosticReasonObjectMissing = "object_missing"
	// DiagnosticReasonStateUnreadable marks an error wrapping
	// ErrStateUnreadable: the task's current state cannot be read.
	//
	//sdk:export
	DiagnosticReasonStateUnreadable = "state_unreadable"
	// DiagnosticReasonSSH marks an error for a failure of the ssh transport
	// itself - connect, auth, host key - rather than of the command it ran.
	//
	//sdk:export
	DiagnosticReasonSSH = "ssh"
	// DiagnosticReasonCommandFailed marks an error for a command that ran and
	// exited non-zero.
	//
	//sdk:export
	DiagnosticReasonCommandFailed = "command_failed"
	// DiagnosticReasonCommandNotRun marks an error for a command that could
	// not be run to completion: it could not start, was cancelled or was
	// killed.
	//
	//sdk:export
	DiagnosticReasonCommandNotRun = "command_not_run"
	// DiagnosticReasonError marks any other error.
	//
	//sdk:export
	DiagnosticReasonError = "error"
)

// Diagnostic is one error or warning from a task's result, ready to show. Every
// string in it is masked with the masker on the context Diagnostics was given.
//
//sdk:export
type Diagnostic struct {
	// Severity is DiagnosticError or DiagnosticWarning.
	Severity DiagnosticSeverity

	// Reason is a stable machine key to branch on. For a warning it is the
	// PlanWarning's Reason, one of the WarnReason* constants. For an error it
	// is one of the DiagnosticReason* constants.
	Reason string

	// Summary is a short sentence that stays the same for the same kind of
	// problem, such as "dokku 0.38.31 or later is required" or "app web does
	// not exist".
	Summary string

	// Detail is the rest of what is known: the full message when it says more
	// than Summary, then the failed command's stdout and stderr when the
	// message does not already carry them. Empty when Summary says it all.
	Detail string

	// Message is the error's own text (err.Error()) or the warning's Message,
	// masked. It is what docket's CLI prints for the diagnostic.
	Message string

	// ExitCode, Stdout and Stderr are the output of the command that failed,
	// masked. For a PlanResult or a TaskOutputState they are the result's own
	// fields; for a bare error they come from the *ExecError or the ssh
	// failure it wraps. They are set on an error only, and are zero or empty
	// when no command output is known.
	ExitCode int
	Stdout   string
	Stderr   string
}

// Diagnostics returns the errors and warnings in result, masked with the
// masker on ctx, so a caller can show them without masking or classifying each
// one itself. result is a PlanResult, a TaskOutputState (or a pointer to
// either), or an error, such as the one ReadTask or ReadAddress returns.
//
// The error, when there is one, comes first, then one warning per entry in
// Warnings, in order, each keeping its Reason. A result with neither, a nil
// error and a nil pointer return nil. Any other type returns a single error
// diagnostic naming it.
//
// An error is classified by what it wraps, first match wins: a failure of the
// ssh transport, a *VersionError, an *ObjectMissingError, ErrStateUnreadable,
// a command that failed (an *ExecError, or a result carrying a non-zero
// ExitCode), and anything else.
//
//sdk:export
func Diagnostics(ctx context.Context, result any) []Diagnostic {
	masker := subprocess.MaskerFromContext(ctx)
	var out []Diagnostic
	switch r := result.(type) {
	case nil:
		return nil
	case PlanResult:
		out = resultDiagnostics(r.Error, commandOutput{r.ExitCode, r.Stdout, r.Stderr}, r.Warnings)
	case *PlanResult:
		if r == nil {
			return nil
		}
		out = resultDiagnostics(r.Error, commandOutput{r.ExitCode, r.Stdout, r.Stderr}, r.Warnings)
	case TaskOutputState:
		out = resultDiagnostics(r.Error, commandOutput{r.ExitCode, r.Stdout, r.Stderr}, r.Warnings)
	case *TaskOutputState:
		if r == nil {
			return nil
		}
		out = resultDiagnostics(r.Error, commandOutput{r.ExitCode, r.Stdout, r.Stderr}, r.Warnings)
	case error:
		out = []Diagnostic{errorDiagnostic(r, errorOutput(r))}
	default:
		msg := fmt.Sprintf("Diagnostics cannot read a %T", result)
		out = []Diagnostic{{Severity: DiagnosticError, Reason: DiagnosticReasonError, Summary: msg, Message: msg}}
	}
	for i := range out {
		out[i] = maskDiagnostic(masker, out[i])
	}
	return out
}

// commandOutput is what a failed command left behind.
type commandOutput struct {
	exitCode int
	stdout   string
	stderr   string
}

// resultDiagnostics is Diagnostics for a PlanResult or a TaskOutputState,
// unmasked.
func resultDiagnostics(err error, output commandOutput, warnings []PlanWarning) []Diagnostic {
	var out []Diagnostic
	if err != nil {
		out = append(out, errorDiagnostic(err, output))
	}
	for _, w := range warnings {
		out = append(out, warningDiagnostic(w))
	}
	return out
}

// errorOutput recovers the output of the command a bare error came from.
func errorOutput(err error) commandOutput {
	var sshErr *subprocess.SSHError
	if errors.As(err, &sshErr) {
		return commandOutput{stderr: sshErr.Stderr}
	}
	var execErr *subprocess.ExecError
	if errors.As(err, &execErr) {
		return commandOutput{
			exitCode: execErr.Response.ExitCode,
			stdout:   execErr.Response.Stdout,
			stderr:   execErr.Response.Stderr,
		}
	}
	return commandOutput{}
}

// errorDiagnostic classifies err, unmasked.
func errorDiagnostic(err error, output commandOutput) Diagnostic {
	d := Diagnostic{
		Severity: DiagnosticError,
		Message:  err.Error(),
		ExitCode: output.exitCode,
		Stdout:   output.stdout,
		Stderr:   output.stderr,
	}

	// The ssh transport is checked first: a version that could not be read
	// because the connection failed is a connection failure, and docket's
	// CLI has always labelled it `ssh:`.
	var sshErr *subprocess.SSHError
	var verr *VersionError
	var missing *ObjectMissingError
	var execErr *subprocess.ExecError
	switch {
	case errors.As(err, &sshErr):
		d.Reason = DiagnosticReasonSSH
		d.Summary = "ssh to " + sshErr.Host + " failed"
	case errors.As(err, &verr):
		d.Reason = DiagnosticReasonVersion
		d.Summary = versionSummary(verr)
	case errors.As(err, &missing):
		d.Reason = DiagnosticReasonObjectMissing
		d.Summary = missing.Error()
	case errors.Is(err, ErrStateUnreadable):
		d.Reason = DiagnosticReasonStateUnreadable
		d.Summary = ErrStateUnreadable.Error()
	case errors.As(err, &execErr) && !execErr.Ran:
		d.Reason = DiagnosticReasonCommandNotRun
		d.Summary = "the command could not be run"
	case errors.As(err, &execErr) || output.exitCode != 0:
		code := output.exitCode
		if code == 0 && execErr != nil {
			code = execErr.Response.ExitCode
		}
		d.Reason = DiagnosticReasonCommandFailed
		d.Summary = fmt.Sprintf("the command failed with exit code %d", code)
	default:
		d.Reason = DiagnosticReasonError
		d.Summary = firstLine(d.Message)
	}
	if d.Summary == "" {
		d.Summary = "the task failed"
	}
	d.Detail = errorDetail(d)
	return d
}

// versionSummary is the Summary for a *VersionError.
func versionSummary(e *VersionError) string {
	component := e.Plugin
	if component == "" {
		component = "dokku"
	}
	switch {
	case e.readFailed && e.Plugin == "":
		return "the dokku version could not be read"
	case e.readFailed:
		return "the plugin versions could not be read"
	case e.Err != nil:
		return "the " + component + " version could not be parsed"
	default:
		return component + " " + e.Minimum + " or later is required"
	}
}

// errorDetail is the Detail for d: its message, unless Summary already says
// it, then the command's output that the message does not already carry.
func errorDetail(d Diagnostic) string {
	var parts []string
	message := strings.TrimSpace(d.Message)
	switch {
	case message == d.Summary:
	case strings.HasPrefix(message, d.Summary+": "):
		parts = append(parts, strings.TrimPrefix(message, d.Summary+": "))
	case message != "":
		parts = append(parts, message)
	}
	for _, out := range []struct{ label, text string }{{"stdout", d.Stdout}, {"stderr", d.Stderr}} {
		text := strings.TrimSpace(out.text)
		if text == "" || strings.Contains(message, text) {
			continue
		}
		parts = append(parts, out.label+":\n"+text)
	}
	return strings.Join(parts, "\n\n")
}

// warningSummaries is the Summary for each warning reason docket raises.
var warningSummaries = map[string]string{
	WarnReasonUnknownProperty:    "a property is missing from the plugin's report",
	WarnReasonProbeRejected:      "the plugin rejected the report probe",
	WarnReasonProbeIndeterminate: "the server does not reveal enough of the state to compare it",
	WarnReasonServiceImageDrift:  "the service is not running the image the task declares",
	WarnReasonServiceOptionDrift: "the service holds a create-time option the task does not declare",
}

// warningDiagnostic is the Diagnostic for w, unmasked.
func warningDiagnostic(w PlanWarning) Diagnostic {
	summary, ok := warningSummaries[w.Reason]
	if !ok {
		summary = firstLine(w.Message)
	}
	d := Diagnostic{
		Severity: DiagnosticWarning,
		Reason:   w.Reason,
		Summary:  summary,
		Message:  w.Message,
	}
	if strings.TrimSpace(w.Message) != summary {
		d.Detail = strings.TrimSpace(w.Message)
	}
	return d
}

// maskDiagnostic masks every string in d.
func maskDiagnostic(m *subprocess.Masker, d Diagnostic) Diagnostic {
	d.Summary = m.String(d.Summary)
	d.Detail = m.String(d.Detail)
	d.Message = m.String(d.Message)
	d.Stdout = m.String(d.Stdout)
	d.Stderr = m.String(d.Stderr)
	return d
}

// firstLine is s up to its first line break, trimmed.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[:i])
	}
	return s
}
