package tasks

import (
	"context"
	"errors"
	"fmt"
	"github.com/dokku/docket/subprocess"
	"sort"
	"strconv"
	"strings"
)

// PortsTask manages the ports for a given dokku application
type PortsTask struct {
	// App is the name of the app
	App string `required:"true" identity:"key" yaml:"app" description:"Name of the app"`

	// PortMappings are the port mappings to set
	PortMappings []PortMapping `required:"false" identity:"collection" yaml:"port_mappings,omitempty" description:"Port mappings to set; omit for state 'clear'; under state 'present' and 'set' no two may share a scheme and host port"`

	// State is the desired state of the ports
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent,set,clear" description:"Desired state of the ports"`
}

// PortsTaskExample contains an example of a PortsTask
type PortsTaskExample struct {
	// Name is the task name holding the PortsTask description
	Name string `yaml:"-"`

	// PortsTask is the PortsTask configuration
	PortsTask PortsTask `yaml:"dokku_ports"`
}

// GetName returns the name of the example
func (e PortsTaskExample) GetName() string {
	return e.Name
}

// PortMapping represents a port mapping
type PortMapping struct {
	// Scheme is the scheme of the port mapping
	Scheme string `required:"true" identity:"key" yaml:"scheme" description:"Scheme of the port mapping"`

	// Host is the host of the port mapping
	Host int `required:"true" identity:"key" yaml:"host" description:"Host of the port mapping"`

	// Container is the container of the port mapping
	Container int `required:"true" yaml:"container" description:"Container of the port mapping"`
}

// String returns the string representation of the port mapping
func (p PortMapping) String() string {
	return fmt.Sprintf("%s:%d:%d", p.Scheme, p.Host, p.Container)
}

// schemeHost returns the scheme:host pair dokku binds a single container port
// to. Two mappings sharing this key cannot coexist, which is what dokku's
// reusesSchemeHostPort refuses in ports:add and ports:set.
func (p PortMapping) schemeHost() string {
	return fmt.Sprintf("%s:%d", p.Scheme, p.Host)
}

// Doc returns the docblock for the ports task
func (t PortsTask) Doc() string {
	return "Manages the ports for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t PortsTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t PortsTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// Examples returns the examples for the ports task
func (t PortsTask) Examples() ([]Doc, error) {
	return MarshalExamples([]PortsTaskExample{
		{
			Name: "Map http port 80 to container port 5000",
			PortsTask: PortsTask{
				App: "node-js-app",
				PortMappings: []PortMapping{
					{Scheme: "http", Host: 80, Container: 5000},
				},
			},
		},
		{
			Name: "Map both http and https ports",
			PortsTask: PortsTask{
				App: "node-js-app",
				PortMappings: []PortMapping{
					{Scheme: "http", Host: 80, Container: 5000},
					{Scheme: "https", Host: 443, Container: 5000},
				},
			},
		},
		{
			Name: "Remove a port mapping",
			PortsTask: PortsTask{
				App: "node-js-app",
				PortMappings: []PortMapping{
					{Scheme: "http", Host: 80, Container: 5000},
				},
				State: StateAbsent,
			},
		},
		{
			Name: "Replace every port mapping on an app",
			PortsTask: PortsTask{
				App: "node-js-app",
				PortMappings: []PortMapping{
					{Scheme: "https", Host: 443, Container: 5000},
				},
				State: StateSet,
			},
		},
		{
			Name: "Clear all port mappings from an app",
			PortsTask: PortsTask{
				App:   "node-js-app",
				State: StateClear,
			},
		},
	})
}

// Execute manages the app's port mappings
func (t PortsTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// Validate checks the PortsTask's inputs without contacting the server.
func (t PortsTask) Validate() error {
	if t.App == "" {
		return errors.New("'app' is required")
	}
	// ports:clear takes no mappings, so a list supplied alongside it would be
	// silently discarded rather than removed. Every other state consumes one.
	if t.State == StateClear {
		if len(t.PortMappings) > 0 {
			return errors.New("'port_mappings' must not be set for state 'clear'")
		}
		return nil
	}
	if len(t.PortMappings) == 0 {
		return fmt.Errorf("'port_mappings' must not be empty for state '%s'", t.State)
	}
	// Each mapping renders as scheme:host:container, so an unknown or
	// misspelled key (silently ignored by the YAML decode) or a missing
	// port would otherwise reach dokku as a malformed argument. Enforce
	// the per-item required fields the docs mark required.
	for i, m := range t.PortMappings {
		if m.Scheme == "" {
			return fmt.Errorf("'scheme' is required for port_mappings[%d]", i)
		}
		if m.Host < 1 || m.Host > 65535 {
			return fmt.Errorf("'host' must be a port between 1 and 65535 for port_mappings[%d]", i)
		}
		if m.Container < 1 || m.Container > 65535 {
			return fmt.Errorf("'container' must be a port between 1 and 65535 for port_mappings[%d]", i)
		}
	}
	// dokku binds one container port per scheme:host pair and refuses a
	// ports:add or ports:set whose mappings reuse one, so a list that does is
	// rejected here rather than at apply. ports:remove has no such check: at
	// most one of the colliding mappings can be bound, and planPortsAbsent
	// already drops the ones the server does not have.
	if t.State != StateAbsent {
		seen := map[string]int{}
		for i, m := range t.PortMappings {
			key := m.schemeHost()
			if first, ok := seen[key]; ok {
				return fmt.Errorf("port_mappings[%d] reuses the scheme and host port of port_mappings[%d] (%s); dokku binds one container port per scheme:host pair", i, first, key)
			}
			seen[key] = i
		}
	}
	return nil
}

// Plan reports the drift the PortsTask would produce.
func (t PortsTask) Plan(ctx context.Context) PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planPortsPresent(ctx, t) },
		StateAbsent:  func() PlanResult { return planPortsAbsent(ctx, t) },
		StateSet:     func() PlanResult { return planPortsSet(ctx, t) },
		StateClear:   func() PlanResult { return planPortsClear(ctx, t) },
	})
}

// planPortsPresent reports drift for the present-state mapping add.
func planPortsPresent(ctx context.Context, t PortsTask) PlanResult {
	currentPorts, err := getPorts(ctx, t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	// dokku validates the existing and new mappings together, so an add whose
	// scheme:host pair is already bound to a different container port is
	// refused. Mappings the server already has are skipped below, which leaves
	// a collision with an existing mapping as the only way the combined list
	// can reuse a pair; Validate() covers a collision within the recipe.
	currentBySchemeHost := map[string]PortMapping{}
	for _, pm := range currentPorts {
		currentBySchemeHost[pm.schemeHost()] = pm
	}
	toAdd := []string{}
	mutations := []string{}
	for _, pm := range t.PortMappings {
		if _, ok := currentPorts[pm.String()]; ok {
			continue
		}
		if existing, ok := currentBySchemeHost[pm.schemeHost()]; ok {
			return planErr(fmt.Errorf("%s conflicts with the existing mapping %s; dokku binds one container port per scheme:host pair, so remove %s first or use state 'set' with the full desired list", pm.String(), existing.String(), existing.String()))
		}
		toAdd = append(toAdd, pm.String())
		mutations = append(mutations, fmt.Sprintf("add %s", pm.String()))
	}
	if len(toAdd) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	status := PlanStatusModify
	if len(currentPorts) == 0 {
		status = PlanStatusCreate
	}
	inputs := dokkuArgsInputs("ports:add", t.App, toAdd)
	return PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%d port mapping(s) to add", len(toAdd)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply:     applyDokkuArgs("ports:add", t.App, toAdd, StatePresent, StateAbsent),
	}
}

// planPortsAbsent reports drift for the absent-state mapping remove.
func planPortsAbsent(ctx context.Context, t PortsTask) PlanResult {
	currentPorts, err := getPorts(ctx, t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	toRemove := []string{}
	mutations := []string{}
	for _, pm := range t.PortMappings {
		if _, ok := currentPorts[pm.String()]; ok {
			toRemove = append(toRemove, pm.String())
			mutations = append(mutations, fmt.Sprintf("remove %s", pm.String()))
		}
	}
	if len(toRemove) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	inputs := dokkuArgsInputs("ports:remove", t.App, toRemove)
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("%d port mapping(s) to remove", len(toRemove)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply:     applyDokkuArgs("ports:remove", t.App, toRemove, StateAbsent, StatePresent),
	}
}

// planPortsSet reports drift for the set-state full replacement. The itemized
// mutations are the adds and removes the replacement works out to; the command
// itself always carries the complete desired list.
func planPortsSet(ctx context.Context, t PortsTask) PlanResult {
	currentPorts, err := getPorts(ctx, t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	desired := map[string]bool{}
	for _, pm := range t.PortMappings {
		desired[pm.String()] = true
	}
	mutations := []string{}
	for _, pm := range t.PortMappings {
		if _, ok := currentPorts[pm.String()]; !ok {
			mutations = append(mutations, fmt.Sprintf("add %s", pm.String()))
		}
	}
	for _, mapping := range sortedPortMappings(currentPorts) {
		if !desired[mapping] {
			mutations = append(mutations, fmt.Sprintf("remove %s", mapping))
		}
	}
	if len(mutations) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	status := PlanStatusModify
	if len(currentPorts) == 0 {
		status = PlanStatusCreate
	}
	args := portMappingArgs(t.PortMappings)
	inputs := dokkuArgsInputs("ports:set", t.App, args)
	return PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%d port mapping change(s)", len(mutations)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply:     applyDokkuArgs("ports:set", t.App, args, StateSet, StateAbsent),
	}
}

// planPortsClear reports drift for the clear-state operation.
func planPortsClear(ctx context.Context, t PortsTask) PlanResult {
	currentPorts, err := getPorts(ctx, t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	if len(currentPorts) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	mappings := sortedPortMappings(currentPorts)
	mutations := make([]string, 0, len(mappings))
	for _, mapping := range mappings {
		mutations = append(mutations, fmt.Sprintf("remove %s", mapping))
	}
	inputs := dokkuArgsInputs("ports:clear", t.App, nil)
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("clear %d port mapping(s)", len(currentPorts)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply:     applyDokkuArgs("ports:clear", t.App, nil, StateClear, StatePresent),
	}
}

// portMappingArgs renders mappings as the scheme:host:container arguments the
// ports subcommands take.
func portMappingArgs(mappings []PortMapping) []string {
	args := make([]string, 0, len(mappings))
	for _, pm := range mappings {
		args = append(args, pm.String())
	}
	return args
}

// sortedPortMappings returns the mapping strings of a getPorts result in a
// stable order, since the probe hands back a map.
func sortedPortMappings(mappings map[string]PortMapping) []string {
	keys := make([]string, 0, len(mappings))
	for k := range mappings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// ExportApp reads the app's port mappings and returns a dokku_ports task, or
// nil when none are configured. Mappings are sorted for deterministic output.
// state:set replaces the whole mapping list for an exact match.
func (t PortsTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	mappings, err := getPorts(ctx, app)
	if err != nil {
		return nil, err
	}
	if len(mappings) == 0 {
		return nil, nil
	}
	list := make([]PortMapping, 0, len(mappings))
	for _, k := range sortedPortMappings(mappings) {
		list = append(list, mappings[k])
	}
	return []interface{}{PortsTask{App: app, PortMappings: list, State: StateSet}}, nil
}

// getPorts gets the ports for a given app. A transport-level failure
// (`*subprocess.SSHError`) is propagated; a dokku-level non-zero exit
// (e.g. app does not exist) is treated as "no ports configured."
func getPorts(ctx context.Context, appName string) (map[string]PortMapping, error) {
	// dokku master has a --ports-map-json that would replace this text parse,
	// but it is not in a release yet; see #431.
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"--quiet",
			"ports:report",
			appName,
			"--ports-map",
		},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return map[string]PortMapping{}, nil
	}

	portMappings := map[string]PortMapping{}
	for _, mapping := range strings.Fields(result.StdoutContents()) {
		parts := strings.Split(mapping, ":")
		if len(parts) != 3 {
			continue
		}

		scheme := parts[0]
		host, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}

		container, err := strconv.Atoi(parts[2])
		if err != nil {
			continue
		}

		portMappings[mapping] = PortMapping{
			Scheme:    scheme,
			Host:      host,
			Container: container,
		}
	}

	return portMappings, nil
}

// init registers the PortsTask with the task registry
func init() {
	RegisterTask(&PortsTask{})
}
