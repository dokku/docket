package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
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
	Name string `required:"true" yaml:"name" description:"Name of the node profile."`

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
	return ExportSupport{Status: ExportSupported}
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
func (t SchedulerK3sProfileTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the SchedulerK3sProfileTask would produce.
func (t SchedulerK3sProfileTask) Plan() PlanResult {
	if err := validateSchedulerK3sProfile(t); err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}

	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planSchedulerK3sProfileSet(t) },
		StateAbsent:  func() PlanResult { return planSchedulerK3sProfileUnset(t) },
	})
}

// validateSchedulerK3sProfile rejects malformed inputs before any subprocess
// runs. Both states require name + role; absent state ignores the other
// fields entirely but they are still rejected if obviously broken.
func validateSchedulerK3sProfile(t SchedulerK3sProfileTask) error {
	if t.Name == "" {
		return errors.New("name is required")
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
func planSchedulerK3sProfileSet(t SchedulerK3sProfileTask) PlanResult {
	current, found, err := getSchedulerK3sProfile(t.Name)
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
		Commands:  resolveCommands(inputs),
		apply: func() TaskOutputState {
			return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
		},
	}
}

// planSchedulerK3sProfileUnset probes for the named profile and removes it
// only when present. dokku's `profiles:remove` silently succeeds when the
// profile is missing, but skipping the call keeps Changed=false honest.
func planSchedulerK3sProfileUnset(t SchedulerK3sProfileTask) PlanResult {
	_, found, err := getSchedulerK3sProfile(t.Name)
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
		Commands:  resolveCommands(inputs),
		apply: func() TaskOutputState {
			return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
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
func getSchedulerK3sProfile(name string) (schedulerK3sProfileEntry, bool, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
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

// init registers the SchedulerK3sProfileTask with the task registry
func init() {
	RegisterTask(&SchedulerK3sProfileTask{})
}
