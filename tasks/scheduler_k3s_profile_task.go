package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// SchedulerK3sProfileTask manages a global scheduler-k3s node profile via the
// dokku `scheduler-k3s:profiles:add` / `:profiles:remove` subcommands. Each
// profile is stored on the dokku host as a single JSON blob
// (`node-profile-<name>.json`); the task represents the complete desired state
// of that blob because dokku's `profiles:add` is a full replace, not a patch.
type SchedulerK3sProfileTask struct {
	// Name is the profile name. It is the lookup key both for the on-disk
	// global property and for `scheduler-k3s:profiles:list --format json`.
	Name string `required:"true" identity:"key" yaml:"name" description:"Name of the node profile. Creating one requires a lowercase name of at most 26 characters: dokku prepends dokku-node-sysctls-profile- to it to name the profile's node-sysctls helm release, and helm caps a release name at 53 characters."`

	// Role is the k3s role nodes joined with this profile take. Required and
	// validated up front; dokku also rejects unknown values but failing in the
	// task gives a clearer message.
	Role string `required:"true" yaml:"role" options:"server,worker" description:"Role for nodes joined with this profile."`

	// KubeletArgs is the list of `key=value` strings forwarded to k3s via
	// `--kubelet-arg`. Drift detection compares this as a multiset; the
	// emitted command preserves the user-declared order so the on-disk JSON
	// tracks the YAML when an apply actually runs.
	KubeletArgs []string `required:"false" yaml:"kubelet_args,omitempty" description:"List of key=value kubelet arguments to forward to k3s."`

	// TaintScheduling toggles the `--taint-scheduling` flag on
	// `scheduler-k3s:profiles:add`. Absent or false is "explicitly cleared".
	TaintScheduling bool `required:"false" yaml:"taint_scheduling,omitempty" description:"Whether to taint the node so only workloads that tolerate the taint schedule on it."`

	// AllowUnknownHosts toggles `--insecure-allow-unknown-hosts`. Absent or
	// false is "explicitly cleared".
	AllowUnknownHosts bool `required:"false" yaml:"allow_unknown_hosts,omitempty" description:"Whether to allow ssh connections to nodes whose host key is not yet trusted."`

	// State is the desired state of the profile.
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the profile."`
}

// SchedulerK3sProfileTaskExample contains an example of a SchedulerK3sProfileTask
type SchedulerK3sProfileTaskExample struct {
	// Name is the task name holding the SchedulerK3sProfileTask description
	Name string `yaml:"-"`

	// SchedulerK3sProfileTask is the SchedulerK3sProfileTask configuration
	SchedulerK3sProfileTask SchedulerK3sProfileTask `yaml:"dokku_scheduler_k3s_profile"`
}

// GetName returns the name of the example
func (e SchedulerK3sProfileTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the scheduler-k3s profile task
func (t SchedulerK3sProfileTask) Doc() string {
	return "Manages a global scheduler-k3s node profile used when joining nodes to a cluster"
}

// ExportSupport reports how docket export handles this task.
func (t SchedulerK3sProfileTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportPartial, Caveat: "every profile dokku reports is exported; a profile whose name dokku accepted but the task refuses for state 'present' - longer than 26 characters, or carrying uppercase, so helm cannot release the derived `dokku-node-sysctls-profile-<name>` - is reported as a warning and left out, since emitting it would make the whole recipe fail docket validate. Remove such a profile with a hand-written task using state 'absent'"}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t SchedulerK3sProfileTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// Examples returns the examples for the scheduler-k3s profile task
func (t SchedulerK3sProfileTask) Examples() ([]Doc, error) {
	return MarshalExamples([]SchedulerK3sProfileTaskExample{
		{
			Name: "Define a worker profile with kubelet args",
			SchedulerK3sProfileTask: SchedulerK3sProfileTask{
				Name: "edge-pool",
				Role: "worker",
				KubeletArgs: []string{
					"max-pods=64",
					"eviction-hard=memory.available<5%",
				},
			},
		},
		{
			Name: "Define a tainted server profile that accepts unknown hosts",
			SchedulerK3sProfileTask: SchedulerK3sProfileTask{
				Name:              "control-plane",
				Role:              "server",
				TaintScheduling:   true,
				AllowUnknownHosts: true,
			},
		},
		{
			Name: "Remove a profile",
			SchedulerK3sProfileTask: SchedulerK3sProfileTask{
				Name:  "edge-pool",
				Role:  "worker",
				State: StateAbsent,
			},
		},
	})
}

// Execute drives the profile to the configured state.
func (t SchedulerK3sProfileTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// Validate checks the SchedulerK3sProfileTask's inputs without contacting the server.
func (t SchedulerK3sProfileTask) Validate() error {
	if err := validateSchedulerK3sProfile(t); err != nil {
		return err
	}
	return nil
}

// Plan reports the drift the SchedulerK3sProfileTask would produce.
func (t SchedulerK3sProfileTask) Plan(ctx context.Context) PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}

	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planSchedulerK3sProfileSet(ctx, t) },
		StateAbsent:  func() PlanResult { return planSchedulerK3sProfileUnset(ctx, t) },
	})
}

// nodeSysctlsReleasePrefix is what dokku prepends to a profile name to derive
// the helm release backing that profile's node-sysctls DaemonSet. Mirrors
// getNodeSysctlsReleaseName in dokku's plugins/scheduler-k3s/node_sysctls.go.
const nodeSysctlsReleasePrefix = "dokku-node-sysctls-profile-"

// helmReleaseNameMaxLength is helm's own ceiling on a release name
// (maxReleaseNameLen in helm's pkg/chartutil/validate_name.go).
const helmReleaseNameMaxLength = 53

// schedulerK3sProfileNameMaxLength is dokku's own cap on a profile name, from
// CommandProfilesAdd and CommandProfilesRemove. dokku's message reads "must be
// less than 32 characters" but the check is `> 32`, so 32 itself is accepted;
// mirror the behaviour rather than the message.
const schedulerK3sProfileNameMaxLength = 32

// schedulerK3sProfileNameHelmMaxLength is the longest profile name whose
// derived node-sysctls release name still fits under helm's ceiling. Derived
// rather than written as a literal so it stays correct if dokku renames the
// prefix, and so the arithmetic is visible to the next reader.
const schedulerK3sProfileNameHelmMaxLength = helmReleaseNameMaxLength - len(nodeSysctlsReleasePrefix)

// schedulerK3sProfileName is the charset dokku accepts for a profile name, in
// both CommandProfilesAdd and CommandProfilesRemove. Mirrors the regexp in
// dokku's plugins/scheduler-k3s/subcommands.go.
var schedulerK3sProfileName = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?$`)

// helmReleaseName is helm's own release-name regexp (validName in helm's
// pkg/chartutil/validate_name.go), which is lowercase-only. dokku's profile
// regexp is not: it allows [a-zA-Z0-9], so a profile name dokku accepts can
// still derive a release name helm refuses.
var helmReleaseName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)

// validateSchedulerK3sProfileName rejects a profile name the server would
// refuse, or would accept and then choke on later.
//
// dokku's own rules - the charset and the 32-character cap - are enforced for
// every state, because `profiles:remove` applies them just as `profiles:add`
// does. The two derived rules are enforced only when creating: dokku builds a
// node-sysctls helm release named `dokku-node-sysctls-profile-<name>`, and helm
// caps a release name at 53 characters and requires it to be lowercase.
// `profiles:add` checks neither, so a name of 27-32 characters, or any
// uppercase name, is stored happily and only fails once something resolves the
// release name: `profiles:remove`, `node-sysctls:set`, or a cluster bootstrap.
// The last two reconcile every scope, so one bad name breaks node sysctls
// server-wide.
//
// Holding the derived rules to state 'present' means docket refuses to create a
// profile it could not later remove, but never refuses to try removing one. A
// profile that predates the rule can still be cleaned up through docket, which
// works on a server with no k3s cluster and fails with dokku's own error on one
// where the removal genuinely cannot succeed. Upstream tracking for having
// dokku reject these at `profiles:add`: dokku/dokku#8971.
func validateSchedulerK3sProfileName(name string, state State) error {
	if name == "" {
		return errors.New("name is required")
	}
	if !schedulerK3sProfileName.MatchString(name) {
		return fmt.Errorf("name must contain only alphanumeric characters and dashes and must not start or end with a dash, got %q", name)
	}
	if len(name) > schedulerK3sProfileNameMaxLength {
		return fmt.Errorf("name must be at most %d characters, got %d", schedulerK3sProfileNameMaxLength, len(name))
	}
	if state == StateAbsent {
		return nil
	}

	release := nodeSysctlsReleasePrefix + name
	if len(name) > schedulerK3sProfileNameHelmMaxLength {
		return fmt.Errorf("name must be at most %d characters for state 'present', got %d: dokku derives the node-sysctls helm release name %q from it, and helm caps a release name at %d characters",
			schedulerK3sProfileNameHelmMaxLength, len(name), release, helmReleaseNameMaxLength)
	}
	// The charset check above has already ruled out everything helm's regexp
	// rejects except case, so this can only fire on an uppercase name - which
	// is why the message names that specifically. Running helm's real regexp
	// rather than a case comparison keeps the check honest if dokku ever
	// widens its own charset.
	if !helmReleaseName.MatchString(release) {
		return fmt.Errorf("name must be lowercase for state 'present', got %q: dokku derives the node-sysctls helm release name %q from it, and helm requires a lowercase release name",
			name, release)
	}
	return nil
}

// validateSchedulerK3sProfile rejects malformed inputs before any subprocess
// runs. Both states require name + role; absent state ignores the other
// fields entirely but they are still rejected if obviously broken.
func validateSchedulerK3sProfile(t SchedulerK3sProfileTask) error {
	if err := validateSchedulerK3sProfileName(t.Name, t.State); err != nil {
		return err
	}
	if t.Role == "" {
		return errors.New("role is required")
	}
	if t.Role != "server" && t.Role != "worker" {
		return fmt.Errorf("role must be 'server' or 'worker', got %q", t.Role)
	}
	for i, arg := range t.KubeletArgs {
		if arg == "" {
			return fmt.Errorf("kubelet_args[%d] must not be empty", i)
		}
		if !strings.Contains(arg, "=") {
			return fmt.Errorf("kubelet_args[%d] %q must be in key=value form", i, arg)
		}
	}
	return nil
}

// planSchedulerK3sProfileSet probes for the named profile and, if its
// captured state differs from the task's desired state, emits a single
// `profiles:add` that carries the complete desired state. `--role` is always
// present (dokku defaults a missing `--role` back to "worker" on each call);
// boolean flags appear only when true; kubelet args are emitted in
// user-declared order so the stored slice tracks the YAML for any apply that
// actually runs.
func planSchedulerK3sProfileSet(ctx context.Context, t SchedulerK3sProfileTask) PlanResult {
	current, found, err := getSchedulerK3sProfile(ctx, t.Name)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	status := PlanStatusCreate
	if found {
		if profileMatches(current, t) {
			return PlanResult{InSync: true, Status: PlanStatusOK}
		}
		status = PlanStatusModify
	}

	inputs := []subprocess.ExecCommandInput{schedulerK3sProfileSetCommand(t)}
	return PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("profile %s drift", t.Name),
		Mutations: []string{formatProfileSetMutation(t, current, found)},
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
		},
	}
}

// planSchedulerK3sProfileUnset probes for the named profile and removes it
// only when present. dokku's `profiles:remove` silently succeeds when the
// profile is missing, but skipping the call keeps Changed=false honest.
func planSchedulerK3sProfileUnset(ctx context.Context, t SchedulerK3sProfileTask) PlanResult {
	_, found, err := getSchedulerK3sProfile(ctx, t.Name)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	if !found {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}

	inputs := []subprocess.ExecCommandInput{{
		Command: "dokku",
		Args:    []string{"--quiet", "scheduler-k3s:profiles:remove", t.Name},
	}}
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("profile %s present", t.Name),
		Mutations: []string{"remove profile " + t.Name},
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateAbsent, inputs)
		},
	}
}

// schedulerK3sProfileSetCommand builds the bulk
// `dokku scheduler-k3s:profiles:add <name> --role <role> [...]` call.
func schedulerK3sProfileSetCommand(t SchedulerK3sProfileTask) subprocess.ExecCommandInput {
	args := []string{"--quiet", "scheduler-k3s:profiles:add", t.Name, "--role", t.Role}
	if t.TaintScheduling {
		args = append(args, "--taint-scheduling")
	}
	if t.AllowUnknownHosts {
		args = append(args, "--insecure-allow-unknown-hosts")
	}
	for _, arg := range t.KubeletArgs {
		args = append(args, "--kubelet-args", arg)
	}
	return subprocess.ExecCommandInput{Command: "dokku", Args: args}
}

// schedulerK3sProfileEntry mirrors the JSON shape dokku writes both to
// `node-profile-<name>.json` and to `scheduler-k3s:profiles:list --format
// json`. Boolean fields use plain bool because dokku omits the keys when
// false, and the Go default is the value we want in that case.
type schedulerK3sProfileEntry struct {
	Name              string   `json:"name"`
	Role              string   `json:"role"`
	KubeletArgs       []string `json:"kubelet_args"`
	TaintScheduling   bool     `json:"taint_scheduling"`
	AllowUnknownHosts bool     `json:"allow_unknown_hosts"`
}

// getSchedulerK3sProfile fetches the live state of the named profile via
// `scheduler-k3s:profiles:list --format json`. There is no `profiles:report`
// subcommand, so the list call (returning every profile) is the only public
// route to a single profile.
func getSchedulerK3sProfile(ctx context.Context, name string) (schedulerK3sProfileEntry, bool, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"--quiet",
			"scheduler-k3s:profiles:list",
			"--format", "json",
		},
	})
	if err != nil {
		return schedulerK3sProfileEntry{}, false, err
	}
	return parseSchedulerK3sProfile(result.StdoutBytes(), name)
}

// parseSchedulerK3sProfile decodes the `:profiles:list --format json`
// payload (a JSON array of profile objects) and returns the entry matching
// name. Kept separate from getSchedulerK3sProfile so the parse path is unit
// testable without a fake subprocess.
func parseSchedulerK3sProfile(raw []byte, name string) (schedulerK3sProfileEntry, bool, error) {
	if len(raw) == 0 {
		return schedulerK3sProfileEntry{}, false, nil
	}
	var entries []schedulerK3sProfileEntry
	if err := json.Unmarshal(raw, &entries); err != nil {
		return schedulerK3sProfileEntry{}, false, fmt.Errorf("parse scheduler-k3s:profiles:list json: %w", err)
	}
	for _, entry := range entries {
		if entry.Name == name {
			return entry, true, nil
		}
	}
	return schedulerK3sProfileEntry{}, false, nil
}

// profileMatches reports whether the captured profile already satisfies the
// task's desired state. KubeletArgs is compared as a multiset so reordering
// alone does not trigger drift; everything else compares verbatim.
func profileMatches(current schedulerK3sProfileEntry, desired SchedulerK3sProfileTask) bool {
	if current.Role != desired.Role {
		return false
	}
	if current.TaintScheduling != desired.TaintScheduling {
		return false
	}
	if current.AllowUnknownHosts != desired.AllowUnknownHosts {
		return false
	}
	return sameKubeletArgs(current.KubeletArgs, desired.KubeletArgs)
}

// sameKubeletArgs returns true when a and b are equal as multisets (same
// elements with the same multiplicities, ignoring order). nil and empty are
// treated identically.
func sameKubeletArgs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	aSorted := append([]string{}, a...)
	bSorted := append([]string{}, b...)
	sort.Strings(aSorted)
	sort.Strings(bSorted)
	for i := range aSorted {
		if aSorted[i] != bSorted[i] {
			return false
		}
	}
	return true
}

// formatProfileSetMutation summarises the create or modify intent for plan
// output. For modifies it shows the desired role and the count of kubelet
// args; for creates it just names the new profile.
func formatProfileSetMutation(t SchedulerK3sProfileTask, current schedulerK3sProfileEntry, found bool) string {
	if !found {
		return fmt.Sprintf("create profile %s (role=%s, kubelet_args=%d)", t.Name, t.Role, len(t.KubeletArgs))
	}
	return fmt.Sprintf("update profile %s (role=%s was %s, kubelet_args=%d was %d, taint_scheduling=%t was %t, allow_unknown_hosts=%t was %t)",
		t.Name,
		t.Role, current.Role,
		len(t.KubeletArgs), len(current.KubeletArgs),
		t.TaintScheduling, current.TaintScheduling,
		t.AllowUnknownHosts, current.AllowUnknownHosts,
	)
}

// ExportGlobal satisfies GlobalExporter by delegating to exportGlobal with a
// no-op warn callback. The export engine prefers ExportGlobalReport when
// present, so the left-out-profile diagnostic reaches ExportReport.Warnings
// rather than being discarded here.
func (t SchedulerK3sProfileTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	return t.exportGlobal(ctx, func(string) {})
}

// ExportGlobalReport is the diagnostics-aware form of ExportGlobal (the
// globalExportReporter interface): it routes the "profile left out" warning
// through the engine's warn callback (wired to ExportReport.Warnings) instead
// of dropping the profile silently.
func (t SchedulerK3sProfileTask) ExportGlobalReport(ctx context.Context, warn func(msg string)) ([]interface{}, error) {
	return t.exportGlobal(ctx, warn)
}

// exportGlobal reconstructs every scheduler-k3s node profile from
// profiles:list, which exposes the full profile definition.
//
// dokku accepts profiles docket cannot express as a valid task: `profiles:add`
// caps a name at 32 characters and allows uppercase, while docket refuses, for
// state 'present', anything helm could not turn into the derived
// `dokku-node-sysctls-profile-<name>` release (#482). A server can therefore be
// carrying a profile whose faithful export is a recipe `docket validate`
// refuses - and it refuses the whole file, not just that task, so one such
// profile would make the entire export unusable (#483). Each candidate body is
// run through its own Validate() - the same method `docket validate` calls -
// and one that would be refused is reported through warn and left out, so the
// recipe that does come back applies. The warning names the profile and the
// remedy, since a left-out profile is also one docket can no longer be asked to
// remove.
func (t SchedulerK3sProfileTask) exportGlobal(ctx context.Context, warn func(msg string)) ([]interface{}, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "scheduler-k3s:profiles:list", "--format", "json"},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return nil, nil
	}

	var entries []schedulerK3sProfileEntry
	if err := json.Unmarshal(result.StdoutBytes(), &entries); err != nil {
		return nil, nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	var out []interface{}
	for _, e := range entries {
		body := SchedulerK3sProfileTask{
			Name:              e.Name,
			Role:              e.Role,
			KubeletArgs:       e.KubeletArgs,
			TaintScheduling:   e.TaintScheduling,
			AllowUnknownHosts: e.AllowUnknownHosts,
			// Explicit rather than left to the loader's default, so the body is
			// plannable straight out of the exporter and Validate() below is
			// asked the same question the loader will ask.
			State: StatePresent,
		}
		if err := body.Validate(); err != nil {
			warn(fmt.Sprintf("profile %q is left out of the recipe because the task would not validate: %v; remove it with a hand-written task using state 'absent'", e.Name, err))
			continue
		}
		out = append(out, body)
	}
	return out, nil
}

// init registers the SchedulerK3sProfileTask with the task registry
func init() {
	RegisterTask(&SchedulerK3sProfileTask{})
}
