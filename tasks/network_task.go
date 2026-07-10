package tasks

import (
	"encoding/json"
	"errors"
	"sort"

	"github.com/dokku/docket/subprocess"
)

// NetworkTask creates or destroys a Docker network
type NetworkTask struct {
	// Name is the name of the network
	Name string `required:"true" yaml:"name" description:"Name of the network"`

	// State is the state of the network
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"State of the network"`
}

// NetworkTaskExample contains an example of a NetworkTask
type NetworkTaskExample struct {
	// Name is the task name holding the NetworkTask description
	Name string `yaml:"-"`

	// DokkuNetwork is the NetworkTask configuration
	DokkuNetwork NetworkTask `yaml:"dokku_network"`
}

// GetName returns the name of the example
func (e NetworkTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the network task
func (t NetworkTask) Doc() string {
	return "Creates or destroys a Docker network"
}

// ExportSupport reports how docket export handles this task.
func (t NetworkTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Examples returns a list of NetworkTaskExamples as yaml
func (t NetworkTask) Examples() ([]Doc, error) {
	return MarshalExamples([]NetworkTaskExample{
		{
			Name: "Create a network named example-network",
			DokkuNetwork: NetworkTask{
				Name: "example-network",
			},
		},
		{
			Name: "Destroy a network named example-network",
			DokkuNetwork: NetworkTask{
				Name:  "example-network",
				State: "absent",
			},
		},
	})
}

// Execute creates or destroys a Docker network
func (t NetworkTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the NetworkTask would produce.
func (t NetworkTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			exists, err := networkExists(t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if exists {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "network:create", t.Name},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    "network missing",
				Mutations: []string{"create network " + t.Name},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			exists, err := networkExists(t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !exists {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "--force", "network:destroy", t.Name},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    "network present",
				Mutations: []string{"destroy network " + t.Name},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// networkListEntry is the subset of `dokku network:list --format json` the
// exporter reads. dokku serializes its DockerNetwork struct with capitalized
// keys, and DokkuManaged (added in dokku 0.39) is true only for networks
// created by network:create, so it distinguishes them from Docker built-ins
// (bridge/host/none) and networks created by other tooling (e.g. compose
// *_default networks).
type networkListEntry struct {
	Name         string `json:"Name"`
	DokkuManaged bool   `json:"DokkuManaged"`
}

// ExportGlobal reconstructs the server's dokku-created networks from
// `network:list --format json`, emitting a dokku_network task for each network
// reporting DokkuManaged. Networks docket did not create - Docker built-ins and
// networks from other tooling - report DokkuManaged false and are skipped. A
// dokku predating the DokkuManaged field reports it absent (decoding to false)
// for every network, so nothing is exported rather than every network being
// re-emitted.
func (t NetworkTask) ExportGlobal() ([]interface{}, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "network:list", "--format", "json"},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return nil, nil
	}

	var entries []networkListEntry
	if err := json.Unmarshal(result.StdoutBytes(), &entries); err != nil {
		return nil, nil
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	var out []interface{}
	for _, e := range entries {
		if e.Name == "" || !e.DokkuManaged {
			continue
		}
		out = append(out, NetworkTask{Name: e.Name})
	}
	return out, nil
}

// networkExists checks if a Docker network exists. Returns (false,
// *subprocess.SSHError) on transport failure; (false, nil) when dokku
// reports the network absent; (true, nil) when present.
func networkExists(name string) (bool, error) {
	return subprocess.Probe(subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"--quiet",
			"network:exists",
			name,
		},
	})
}

// destroyNetwork is retained as an integration-test helper. It runs the
// destroy-network apply path synchronously.
func destroyNetwork(name string) TaskOutputState {
	state := TaskOutputState{Changed: false, State: StatePresent}
	exists, _ := networkExists(name)
	if !exists {
		state.State = StateAbsent
		return state
	}
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "--force", "network:destroy", name},
	})
	state.Commands = append(state.Commands, result.Command)
	if err != nil {
		return TaskOutputErrorFromExec(state, err, result)
	}
	state = state.WithExecResult(result)
	state.Changed = true
	state.State = StateAbsent
	return state
}

// init registers the NetworkTask with the task registry
func init() {
	RegisterTask(&NetworkTask{})
}
