package tasks

// ProbeStatus classifies how much of a task's current state Plan() can read
// back from the server before it decides whether to mutate.
type ProbeStatus string

const (
	// ProbeSupported means Plan() reads the resource it manages, so a server
	// that already matches the recipe plans as in sync and a second apply is
	// a no-op.
	ProbeSupported ProbeStatus = "supported"

	// ProbePartial means Plan() reads some of the resource but not all of it:
	// a write-only field with no read command, or an idempotency key that is
	// coarser than the task's inputs. The task converges for the dimensions it
	// probes and plans as drift forever for the rest.
	ProbePartial ProbeStatus = "partial"

	// ProbeUnsupported means Plan() cannot read the resource at all. The task
	// plans as drift on every run and never converges, so a recipe containing
	// one never settles to `plan --detailed-exitcode` exit 0.
	ProbeUnsupported ProbeStatus = "unsupported"
)

// ProbeSupport describes a task's read-state behaviour. Status is required;
// Caveat is a human-readable note naming what cannot be read (and may be empty
// for a task that probes everything it manages).
//
// The json tags are for the task catalog, which publishes this verbatim.
type ProbeSupport struct {
	Status ProbeStatus `json:"status"`
	Caveat string      `json:"caveat,omitempty"`
}

// ProbeDocer is the interface a task implements to declare whether Plan() can
// read its current state. Every registered task is expected to implement it - a
// coverage test enforces that so no task ships without a probe decision - but
// it is modelled as an optional interface to match ExportDocer,
// DeprecationDocer and RequirementsDocer. The docs generator renders the result
// in a Probe support section on the task's page, and `--list-tasks` marks the
// tasks that will never converge.
//
// This declares a *structural* inability to read state: dokku exposes no
// command that reports the resource, so the answer is the same on every server
// and every run. It is not the same thing as a probe that fails at runtime - a
// stale property key map, an older plugin that rejects `:report --format json`,
// an unreachable host. Those stay what they are today: a PlanWarning (see
// WarnReasonUnknownProperty / WarnReasonProbeRejected) or a PlanStatusError,
// raised per-run by the probe call site, and they never change a task's
// declared status.
//
// Nothing in the plan or apply path reads this: drift is decided entirely by
// each Plan() implementation. What keeps the declaration honest is
// TestProbeSupportMatchesPlanWiring, which fails when a task claims it can
// converge but one of its DispatchPlan branches has no reachable in-sync result
// (or when it claims it cannot and some branch does). Asking per branch rather
// than per Plan() is what stops a task that probes `present` and not `absent`
// from passing on the strength of its `present` branch alone.
type ProbeDocer interface {
	ProbeSupport() ProbeSupport
}

// TaskProbeSupport returns the probe support for t, and whether t declared it.
// Centralised so docs generation, --list-tasks, and the coverage test share one
// read site, mirroring TaskExportSupport.
func TaskProbeSupport(t Task) (ProbeSupport, bool) {
	if d, ok := t.(ProbeDocer); ok {
		return d.ProbeSupport(), true
	}
	return ProbeSupport{}, false
}
