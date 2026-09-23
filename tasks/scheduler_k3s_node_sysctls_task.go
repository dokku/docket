package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// schedulerK3sNodeSysctlsGlobalScope is the key the node-sysctls report files
// the unprofiled-node scope under, and the flag that addresses it on the set
// and clear subcommands.
const schedulerK3sNodeSysctlsGlobalScope = "--global"

// SchedulerK3sNodeSysctlsTask manages the node-level kernel sysctls
// scheduler-k3s applies to every node without a node profile.
//
// Dokku also scopes sysctls to a node profile, but the task deliberately does
// not: `scheduler-k3s:node-sysctls:report` renders a profile's entry as the
// effective set, with the global sysctls merged underneath the profile's own,
// while `--replace` and `:clear` write only the profile's own map. A
// profile-scoped task could therefore never converge - after a clear the report
// still lists every inherited global sysctl, so the task would read drift and
// clear again forever. The global scope's report entry is exactly its stored
// map, which is why it alone is safe to manage. See dokku/dokku#9073 and #555.
type SchedulerK3sNodeSysctlsTask struct {
	// Global scopes the sysctls to every node without a node profile. It is
	// the only scope this task manages, so it must be true; a key field rather
	// than a required one, matching the app/global pair the rest of the
	// scheduler-k3s family keys on, and the slot `profile` joins once a
	// profile's stored map can be read back.
	Global bool `required:"false" identity:"key" yaml:"global" description:"Scope the sysctls to every node without a node profile. Must be true: node profile scopes are not manageable until dokku exposes a profile's stored sysctl map."`

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
	return "Manages the scheduler-k3s node-level kernel sysctls applied to every node without a node profile"
}

// ExportSupport reports how docket export handles this task.
func (t SchedulerK3sNodeSysctlsTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
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
	})
}

// Execute sets or clears the scheduler-k3s node sysctls for the global scope
func (t SchedulerK3sNodeSysctlsTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// Validate checks the SchedulerK3sNodeSysctlsTask's inputs without contacting the server.
func (t SchedulerK3sNodeSysctlsTask) Validate() error {
	if !t.Global {
		return errors.New("'global' must be set to true; scheduler-k3s node profile scopes are not manageable until dokku exposes a profile's stored sysctl map")
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
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			return planPairsSet(ctx, "sysctl", t.Sysctls, getSchedulerK3sNodeSysctls, schedulerK3sNodeSysctlsCommand)
		},
		StateAbsent: func() PlanResult {
			return planPairsUnset(ctx, "sysctl", t.Sysctls, getSchedulerK3sNodeSysctls, schedulerK3sNodeSysctlsCommand)
		},
		StateSet: func() PlanResult {
			return planPairsReplace(ctx, "sysctl", t.Sysctls, getSchedulerK3sNodeSysctls, func() subprocess.ExecCommandInput {
				return schedulerK3sNodeSysctlsReplaceCommand(t.Sysctls)
			})
		},
		StateClear: func() PlanResult {
			return planPairsClear(ctx, "sysctl", getSchedulerK3sNodeSysctls, schedulerK3sNodeSysctlsClearCommand)
		},
	})
}

// schedulerK3sNodeSysctlsCommand builds one `dokku
// scheduler-k3s:node-sysctls:set` call. An empty value is forwarded verbatim;
// dokku interprets it as a delete.
func schedulerK3sNodeSysctlsCommand(name, value string) subprocess.ExecCommandInput {
	return subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "scheduler-k3s:node-sysctls:set", schedulerK3sNodeSysctlsGlobalScope, name, value},
	}
}

// schedulerK3sNodeSysctlsReplaceCommand builds the single `:set --replace` call
// that swaps the scope's whole map for the declared one. Pairs are sorted so
// the rendered command does not change between runs.
func schedulerK3sNodeSysctlsReplaceCommand(sysctls map[string]string) subprocess.ExecCommandInput {
	args := []string{"--quiet", "scheduler-k3s:node-sysctls:set", "--replace", schedulerK3sNodeSysctlsGlobalScope}
	for _, name := range sortedPairKeys(sysctls) {
		args = append(args, name+"="+sysctls[name])
	}
	return subprocess.ExecCommandInput{Command: "dokku", Args: args}
}

// schedulerK3sNodeSysctlsClearCommand builds the `:clear` call that empties the
// scope.
func schedulerK3sNodeSysctlsClearCommand() subprocess.ExecCommandInput {
	return subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "scheduler-k3s:node-sysctls:clear", schedulerK3sNodeSysctlsGlobalScope},
	}
}

// getSchedulerK3sNodeSysctls reads the sysctls currently stored for the global
// scope. `scheduler-k3s:node-sysctls:report --format json` emits one entry per
// scope, keyed by profile name with the global scope under "--global", so the
// report carries every scope and this keeps the one it owns. A transport-level
// failure (*subprocess.SSHError) is propagated; a dokku-level non-zero exit
// (scheduler-k3s not initialized, say) is treated as "no sysctls"; malformed
// JSON surfaces as an error.
func getSchedulerK3sNodeSysctls(ctx context.Context) (map[string]string, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "scheduler-k3s:node-sysctls:report", "--format", "json"},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return map[string]string{}, nil
	}

	payload := map[string]map[string]string{}
	if err := json.Unmarshal(result.StdoutBytes(), &payload); err != nil {
		return nil, fmt.Errorf("parse scheduler-k3s:node-sysctls:report json: %w", err)
	}

	sysctls := map[string]string{}
	for name, value := range payload[schedulerK3sNodeSysctlsGlobalScope] {
		sysctls[name] = value
	}
	return sysctls, nil
}

// ExportGlobal reconstructs the global-scope node sysctls, or nil when none are
// stored. state:set replaces the whole map, so a re-applied export reproduces
// the exact set rather than merging into whatever the target already carries.
func (t SchedulerK3sNodeSysctlsTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	sysctls, err := getSchedulerK3sNodeSysctls(ctx)
	if err != nil {
		return nil, err
	}
	if len(sysctls) == 0 {
		return nil, nil
	}
	return []interface{}{SchedulerK3sNodeSysctlsTask{Global: true, Sysctls: sysctls, State: StateSet}}, nil
}

// init registers the SchedulerK3sNodeSysctlsTask with the task registry
func init() {
	RegisterTask(&SchedulerK3sNodeSysctlsTask{})
}
