package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// schedulerK3sNodeSysctlsGlobalScope is the key the node-sysctls report files
// the unprofiled-node scope under, and the flag that addresses it on the set,
// clear and report subcommands.
const schedulerK3sNodeSysctlsGlobalScope = "--global"

// SchedulerK3sNodeSysctlsTask manages the node-level kernel sysctls
// scheduler-k3s applies either to every node without a node profile or to the
// nodes of a single node profile.
//
// The probe reads `scheduler-k3s:node-sysctls:report --stored`, which reports
// the map each scope stores on its own. The plain report renders a profile's
// entry as the resolved set, with the global sysctls merged underneath the
// profile's own, while `--replace` and `:clear` write only the profile's own
// map - so a profile-scoped task probing the plain report would read drift
// forever. `--stored` arrived in dokku 0.38.30 (dokku/dokku#9075).
type SchedulerK3sNodeSysctlsTask struct {
	// Global scopes the sysctls to every node without a node profile.
	Global bool `required:"false" identity:"key" yaml:"global,omitempty" description:"Scope the sysctls to every node without a node profile. Exactly one of 'global' and 'profile' must be set."`

	// Profile scopes the sysctls to the nodes of a single node profile.
	Profile string `required:"false" identity:"key" yaml:"profile,omitempty" description:"Name of the node profile to scope the sysctls to. The profile must already exist, and dokku only scopes sysctls to a lowercase name of at most 26 characters. A profile's nodes still inherit every global sysctl the profile does not set itself; this task manages only the profile's own map. Exactly one of 'global' and 'profile' must be set."`

	// Sysctls is the desired set of sysctl name/value pairs for the scope.
	Sysctls map[string]string `required:"false" identity:"collection" yaml:"sysctls,omitempty" description:"Map of sysctl name to value to apply to the scope; omit for state 'clear'. Removing a sysctl stops it being applied to new nodes but does not revert it on nodes already carrying it until they reboot. Under state 'present' a value must not begin with '-', which dokku's flag parser would read as a flag; state 'set' has no such limit."`

	// State is the desired state of the sysctls. 'present' and 'absent' are
	// additive, naming sysctls to write or clear; 'set' declares the complete
	// map for the scope and 'clear' empties it.
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent,set,clear" description:"Desired state of the sysctls. 'set' declares the complete map for the scope, removing any sysctl the recipe does not name; 'clear' empties it."`
}

// SchedulerK3sNodeSysctlsTaskExample contains an example of a SchedulerK3sNodeSysctlsTask
type SchedulerK3sNodeSysctlsTaskExample struct {
	// Name is the task name holding the SchedulerK3sNodeSysctlsTask description
	Name string `yaml:"-"`

	// SchedulerK3sNodeSysctlsTask is the SchedulerK3sNodeSysctlsTask configuration
	SchedulerK3sNodeSysctlsTask SchedulerK3sNodeSysctlsTask `yaml:"dokku_scheduler_k3s_node_sysctls"`
}

// GetName returns the name of the example
func (e SchedulerK3sNodeSysctlsTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the scheduler-k3s node sysctls task
func (t SchedulerK3sNodeSysctlsTask) Doc() string {
	return "Manages the scheduler-k3s node-level kernel sysctls for unprofiled nodes or a single node profile"
}

// ExportSupport reports how docket export handles this task.
func (t SchedulerK3sNodeSysctlsTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportPartial, Caveat: "the global scope and every node profile storing sysctls are exported; a profile whose name dokku cannot derive a node-sysctls helm release from - longer than 26 characters, or carrying uppercase - is reported as a warning and left out, since dokku refuses to write sysctls for it and emitting it would make the whole recipe fail docket validate"}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t SchedulerK3sNodeSysctlsTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// Examples returns the examples for the scheduler-k3s node sysctls task
func (t SchedulerK3sNodeSysctlsTask) Examples() ([]Doc, error) {
	return MarshalExamples([]SchedulerK3sNodeSysctlsTaskExample{
		{
			Name: "Set node sysctls for every unprofiled node",
			SchedulerK3sNodeSysctlsTask: SchedulerK3sNodeSysctlsTask{
				Global: true,
				Sysctls: map[string]string{
					"vm.max_map_count": "262144",
					"vm.swappiness":    "10",
				},
			},
		},
		{
			Name: "Remove a specific node sysctl",
			SchedulerK3sNodeSysctlsTask: SchedulerK3sNodeSysctlsTask{
				Global: true,
				Sysctls: map[string]string{
					"vm.swappiness": "",
				},
				State: StateAbsent,
			},
		},
		{
			Name: "Replace every node sysctl",
			SchedulerK3sNodeSysctlsTask: SchedulerK3sNodeSysctlsTask{
				Global: true,
				Sysctls: map[string]string{
					"vm.max_map_count": "262144",
				},
				State: StateSet,
			},
		},
		{
			Name: "Clear every node sysctl",
			SchedulerK3sNodeSysctlsTask: SchedulerK3sNodeSysctlsTask{
				Global: true,
				State:  StateClear,
			},
		},
		{
			Name: "Replace the node sysctls of a node profile",
			SchedulerK3sNodeSysctlsTask: SchedulerK3sNodeSysctlsTask{
				Profile: "edge-workers",
				Sysctls: map[string]string{
					"vm.swappiness": "60",
				},
				State: StateSet,
			},
		},
	})
}

// Execute sets or clears the scheduler-k3s node sysctls for the task's scope
func (t SchedulerK3sNodeSysctlsTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// Validate checks the SchedulerK3sNodeSysctlsTask's inputs without contacting the server.
func (t SchedulerK3sNodeSysctlsTask) Validate() error {
	if t.Global && t.Profile != "" {
		return errors.New("'profile' must not be set when 'global' is set to true")
	}
	if !t.Global && t.Profile == "" {
		return errors.New("'profile' is required when 'global' is not set to true")
	}
	if t.Profile != "" {
		// dokku refuses a profile it cannot derive a helm release name from on
		// every node-sysctls write, :clear included, so the release rules hold
		// for every state rather than only the creating one.
		if err := validateSchedulerK3sProfileNameStored("'profile'", t.Profile); err != nil {
			return err
		}
		if err := validateSchedulerK3sProfileNameRelease("'profile'", "", t.Profile); err != nil {
			return err
		}
	}

	effective := t.State
	if effective == "" {
		effective = StatePresent
	}

	// :clear takes no sysctls, so a map supplied alongside it would be silently
	// discarded rather than removed. Every other state consumes one.
	if effective == StateClear {
		if len(t.Sysctls) > 0 {
			return errors.New("'sysctls' must not be set for state 'clear'")
		}
		return nil
	}
	if len(t.Sysctls) == 0 {
		return fmt.Errorf("'sysctls' must not be empty for state '%s'", effective)
	}

	for _, name := range sortedPairKeys(t.Sysctls) {
		if name == "" {
			return errors.New("sysctl names must not be empty")
		}
		// The per-key form passes the name and the value as separate arguments,
		// so an "=" in a name is harmless there. State 'set' goes through
		// :set --replace, which splits every pair on its first "=", and would
		// store a truncated name and a mangled value instead.
		if effective == StateSet && strings.Contains(name, "=") {
			return fmt.Errorf("sysctl names must not contain '=' for state 'set', got %q", name)
		}
		// dokku interprets an empty value on the :set subcommand as a clear, so
		// a present-state empty value can never be stored and would drift on
		// every run (issue #358). Clearing is expressed with state 'absent'.
		// State 'set' is exempt: :set --replace writes the declared map
		// wholesale, so an empty value is stored and reads back out of the
		// report.
		if effective == StatePresent && t.Sysctls[name] == "" {
			return errors.New("sysctl values must not be empty for state 'present'; use state 'absent' to clear a sysctl")
		}
		// The per-key form passes the value as its own argument, where dokku's
		// flag parser reads a leading "-" as the start of a flag rather than as
		// a value. State 'set' is unaffected, since the pair "name=-1" does not
		// begin with one, and state 'absent' never sends a value at all.
		if effective == StatePresent && strings.HasPrefix(t.Sysctls[name], "-") {
			return fmt.Errorf("sysctl values must not begin with '-' for state 'present'; use state 'set', which passes name=value as one argument, got %q for %q", t.Sysctls[name], name)
		}
	}
	return nil
}

// Plan reports the drift the SchedulerK3sNodeSysctlsTask would produce.
func (t SchedulerK3sNodeSysctlsTask) Plan(ctx context.Context) PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	current := func(ctx context.Context) (map[string]string, error) {
		return getSchedulerK3sNodeSysctls(ctx, t)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			return planPairsSet(ctx, "sysctl", t.Sysctls, current, t.setCommand)
		},
		StateAbsent: func() PlanResult {
			return planPairsUnset(ctx, "sysctl", t.Sysctls, current, t.setCommand)
		},
		StateSet: func() PlanResult {
			return planPairsReplace(ctx, "sysctl", t.Sysctls, current, t.replaceCommand)
		},
		StateClear: func() PlanResult {
			return planPairsClear(ctx, "sysctl", current, t.clearCommand)
		},
	})
}

// scopeArgs returns the flags that address the task's scope on the set, clear
// and report subcommands. dokku's flag parser stops at the first positional
// argument, so callers place these ahead of any sysctl name or pair.
func (t SchedulerK3sNodeSysctlsTask) scopeArgs() []string {
	if t.Profile != "" {
		return []string{"--profile", t.Profile}
	}
	return []string{schedulerK3sNodeSysctlsGlobalScope}
}

// scopeKey returns the key the report files the task's scope under: the
// profile name, or "--global" for the unprofiled-node scope.
func (t SchedulerK3sNodeSysctlsTask) scopeKey() string {
	if t.Profile != "" {
		return t.Profile
	}
	return schedulerK3sNodeSysctlsGlobalScope
}

// setCommand builds one `dokku scheduler-k3s:node-sysctls:set` call. An empty
// value is forwarded verbatim; dokku interprets it as a delete.
func (t SchedulerK3sNodeSysctlsTask) setCommand(name, value string) subprocess.ExecCommandInput {
	args := append([]string{"--quiet", "scheduler-k3s:node-sysctls:set"}, t.scopeArgs()...)
	return subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    append(args, name, value),
	}
}

// replaceCommand builds the single `:set --replace` call that swaps the scope's
// whole map for the declared one. Pairs are sorted so the rendered command does
// not change between runs.
func (t SchedulerK3sNodeSysctlsTask) replaceCommand() subprocess.ExecCommandInput {
	args := append([]string{"--quiet", "scheduler-k3s:node-sysctls:set", "--replace"}, t.scopeArgs()...)
	for _, name := range sortedPairKeys(t.Sysctls) {
		args = append(args, name+"="+t.Sysctls[name])
	}
	return subprocess.ExecCommandInput{Command: "dokku", Args: args}
}

// clearCommand builds the `:clear` call that empties the scope.
func (t SchedulerK3sNodeSysctlsTask) clearCommand() subprocess.ExecCommandInput {
	return subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    append([]string{"--quiet", "scheduler-k3s:node-sysctls:clear"}, t.scopeArgs()...),
	}
}

// getSchedulerK3sNodeSysctls reads the sysctls the task's scope stores on its
// own, narrowing `scheduler-k3s:node-sysctls:report --stored` to that scope.
func getSchedulerK3sNodeSysctls(ctx context.Context, t SchedulerK3sNodeSysctlsTask) (map[string]string, error) {
	payload, err := readSchedulerK3sNodeSysctlsReport(ctx, t.scopeArgs()...)
	if err != nil {
		return nil, err
	}

	sysctls := map[string]string{}
	for name, value := range payload[t.scopeKey()] {
		sysctls[name] = value
	}
	return sysctls, nil
}

// readSchedulerK3sNodeSysctlsReport runs `scheduler-k3s:node-sysctls:report
// --stored --format json`, narrowed by scopeArgs when any are given, and
// returns its one-entry-per-scope payload, keyed by profile name with the
// global scope under "--global".
//
// A transport-level failure (*subprocess.SSHError) is propagated. A dokku older
// than 0.38.30 has no --stored flag; that is an error rather than "no
// sysctls", which would otherwise let state 'clear' report in sync on a scope
// that still stores sysctls. Any other dokku-level non-zero exit (scheduler-k3s
// not initialized, or a profile that does not exist) is treated as "no
// sysctls". Malformed JSON surfaces as an error.
func readSchedulerK3sNodeSysctlsReport(ctx context.Context, scopeArgs ...string) (map[string]map[string]string, error) {
	args := append([]string{"--quiet", "scheduler-k3s:node-sysctls:report", "--stored"}, scopeArgs...)
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    append(args, "--format", "json"),
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		var execErr *subprocess.ExecError
		if errors.As(err, &execErr) && strings.Contains(execErr.Response.Stderr, "flag provided but not defined: -stored") {
			return nil, errors.New("scheduler-k3s:node-sysctls:report has no --stored flag; dokku_scheduler_k3s_node_sysctls requires dokku 0.38.30 or newer")
		}
		return map[string]map[string]string{}, nil
	}

	payload := map[string]map[string]string{}
	if err := json.Unmarshal(result.StdoutBytes(), &payload); err != nil {
		return nil, fmt.Errorf("parse scheduler-k3s:node-sysctls:report json: %w", err)
	}
	return payload, nil
}

// ExportGlobal satisfies GlobalExporter by delegating to exportGlobal with a
// no-op warn callback. The export engine prefers ExportGlobalReport when
// present, so the left-out-profile diagnostic reaches ExportReport.Warnings
// rather than being discarded here.
func (t SchedulerK3sNodeSysctlsTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	return t.exportGlobal(ctx, func(string) {})
}

// ExportGlobalReport is the diagnostics-aware form of ExportGlobal (the
// globalExportReporter interface).
func (t SchedulerK3sNodeSysctlsTask) ExportGlobalReport(ctx context.Context, warn func(msg string)) ([]interface{}, error) {
	return t.exportGlobal(ctx, warn)
}

// exportGlobal reconstructs every scope that stores sysctls: the global scope
// first, then each node profile by name. Scopes storing nothing are skipped,
// since state:set refuses an empty map. state:set replaces the whole map, so a
// re-applied export reproduces the exact set rather than merging into whatever
// the target already carries.
//
// dokku reports a profile whose name it cannot derive a helm release name
// from, but refuses to write sysctls for one, so such a body would fail
// Validate() and with it the whole recipe. It is reported through warn and left
// out, matching how dokku_scheduler_k3s_profile leaves the profile itself out.
func (t SchedulerK3sNodeSysctlsTask) exportGlobal(ctx context.Context, warn func(msg string)) ([]interface{}, error) {
	payload, err := readSchedulerK3sNodeSysctlsReport(ctx)
	if err != nil {
		return nil, err
	}

	var out []interface{}
	if global := payload[schedulerK3sNodeSysctlsGlobalScope]; len(global) > 0 {
		out = append(out, SchedulerK3sNodeSysctlsTask{Global: true, Sysctls: global, State: StateSet})
	}

	profiles := make([]string, 0, len(payload))
	for key, sysctls := range payload {
		if key != schedulerK3sNodeSysctlsGlobalScope && len(sysctls) > 0 {
			profiles = append(profiles, key)
		}
	}
	sort.Strings(profiles)
	for _, profile := range profiles {
		body := SchedulerK3sNodeSysctlsTask{Profile: profile, Sysctls: payload[profile], State: StateSet}
		if err := body.Validate(); err != nil {
			warn(fmt.Sprintf("node sysctls for profile %q are left out of the recipe because the task would not validate: %v", profile, err))
			continue
		}
		out = append(out, body)
	}
	return out, nil
}

// init registers the SchedulerK3sNodeSysctlsTask with the task registry
func init() {
	RegisterTask(&SchedulerK3sNodeSysctlsTask{})
}
