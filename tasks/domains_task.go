package tasks

import (
	"fmt"
	"github.com/dokku/docket/subprocess"
	"strings"
)

// DomainsTask manages the domains for a given dokku application or globally
type DomainsTask struct {
	// App is the name of the app
	App string `required:"false" yaml:"app"`

	// Global is a flag indicating if the domains should be applied globally
	Global bool `required:"false" yaml:"global,omitempty"`

	// Domains is the list of domain names
	Domains []string `required:"false" yaml:"domains"`

	// State is the desired state of the domains
	State State `required:"false" yaml:"state" default:"present" options:"present,absent,set,clear"`
}

// DomainsTaskExample contains an example of a DomainsTask
type DomainsTaskExample struct {
	// Name is the task name holding the DomainsTask description
	Name string `yaml:"-"`

	// DomainsTask is the DomainsTask configuration
	DomainsTask DomainsTask `yaml:"dokku_domains"`
}

// GetName returns the name of the example
func (e DomainsTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the domains task
func (t DomainsTask) Doc() string {
	return "Manages the domains for a given dokku application or globally"
}

// Examples returns the examples for the domains task
func (t DomainsTask) Examples() ([]Doc, error) {
	return MarshalExamples([]DomainsTaskExample{
		{
			Name: "Add domains to an app",
			DomainsTask: DomainsTask{
				App:     "example-app",
				Domains: []string{"example.com", "www.example.com"},
			},
		},
		{
			Name: "Remove domains from an app",
			DomainsTask: DomainsTask{
				App:     "example-app",
				Domains: []string{"old.example.com"},
				State:   "absent",
			},
		},
		{
			Name: "Set global domains",
			DomainsTask: DomainsTask{
				Global:  true,
				Domains: []string{"global.example.com"},
				State:   "set",
			},
		},
		{
			Name: "Clear all domains from an app",
			DomainsTask: DomainsTask{
				App:   "example-app",
				State: "clear",
			},
		},
	})
}

// Execute manages the domains
func (t DomainsTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the DomainsTask would produce.
func (t DomainsTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planDomainsPresent(t) },
		StateAbsent:  func() PlanResult { return planDomainsAbsent(t) },
		StateSet:     func() PlanResult { return planDomainsSet(t) },
		StateClear:   func() PlanResult { return planDomainsClear(t) },
	})
}

// planDomainsPresent reports drift for the present-state domain add.
func planDomainsPresent(t DomainsTask) PlanResult {
	if err := validateDomainsTask(t, true); err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	currentDomains, err := getDomains(t.App, t.Global)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	toAdd := []string{}
	mutations := []string{}
	for _, d := range t.Domains {
		if !currentDomains[d] {
			toAdd = append(toAdd, d)
			mutations = append(mutations, fmt.Sprintf("add %s", d))
		}
	}
	if len(toAdd) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	status := PlanStatusModify
	if len(currentDomains) == 0 {
		status = PlanStatusCreate
	}
	subcommand := "domains:add"
	appName := t.App
	if t.Global {
		subcommand = "domains:add-global"
		appName = "--global"
	}
	inputs := dokkuArgsInputs(subcommand, appName, toAdd)
	return PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%d domain(s) to add", len(toAdd)),
		Mutations: mutations,
		Commands:  resolveCommands(inputs),
		apply:     applyDokkuArgs(subcommand, appName, toAdd, StatePresent, StateAbsent),
	}
}

// planDomainsAbsent reports drift for the absent-state domain remove.
func planDomainsAbsent(t DomainsTask) PlanResult {
	if err := validateDomainsTask(t, true); err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	currentDomains, err := getDomains(t.App, t.Global)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	toRemove := []string{}
	mutations := []string{}
	for _, d := range t.Domains {
		if currentDomains[d] {
			toRemove = append(toRemove, d)
			mutations = append(mutations, fmt.Sprintf("remove %s", d))
		}
	}
	if len(toRemove) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	subcommand := "domains:remove"
	appName := t.App
	if t.Global {
		subcommand = "domains:remove-global"
		appName = "--global"
	}
	inputs := dokkuArgsInputs(subcommand, appName, toRemove)
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("%d domain(s) to remove", len(toRemove)),
		Mutations: mutations,
		Commands:  resolveCommands(inputs),
		apply:     applyDokkuArgs(subcommand, appName, toRemove, StateAbsent, StatePresent),
	}
}

// planDomainsSet reports drift for the set-state full replacement.
func planDomainsSet(t DomainsTask) PlanResult {
	if err := validateDomainsTask(t, true); err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	currentDomains, err := getDomains(t.App, t.Global)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	desired := map[string]bool{}
	for _, d := range t.Domains {
		desired[d] = true
	}
	mutations := []string{}
	for d := range desired {
		if !currentDomains[d] {
			mutations = append(mutations, fmt.Sprintf("add %s", d))
		}
	}
	for d := range currentDomains {
		if !desired[d] {
			mutations = append(mutations, fmt.Sprintf("remove %s", d))
		}
	}
	if len(mutations) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	subcommand := "domains:set"
	appName := t.App
	if t.Global {
		subcommand = "domains:set-global"
		appName = "--global"
	}
	inputs := dokkuArgsInputs(subcommand, appName, t.Domains)
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusModify,
		Reason:    fmt.Sprintf("%d domain change(s)", len(mutations)),
		Mutations: mutations,
		Commands:  resolveCommands(inputs),
		apply:     applyDokkuArgs(subcommand, appName, t.Domains, StateSet, StateAbsent),
	}
}

// planDomainsClear reports drift for the clear-state operation.
func planDomainsClear(t DomainsTask) PlanResult {
	if err := validateDomainsTask(t, false); err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	currentDomains, err := getDomains(t.App, t.Global)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	if len(currentDomains) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	mutations := make([]string, 0, len(currentDomains))
	for d := range currentDomains {
		mutations = append(mutations, fmt.Sprintf("remove %s", d))
	}
	subcommand := "domains:clear"
	appName := t.App
	if t.Global {
		subcommand = "domains:clear-global"
		appName = "--global"
	}
	inputs := dokkuArgsInputs(subcommand, appName, nil)
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("clear %d domain(s)", len(currentDomains)),
		Mutations: mutations,
		Commands:  resolveCommands(inputs),
		apply:     applyDokkuArgs(subcommand, appName, nil, StateClear, StatePresent),
	}
}

// dokkuArgsInputs returns the subprocess inputs that run `dokku --quiet
// <subcommand> <target> <extra...>`.
func dokkuArgsInputs(subcommand, target string, extra []string) []subprocess.ExecCommandInput {
	args := []string{"--quiet", subcommand, target}
	args = append(args, extra...)
	return []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
}

// applyDokkuArgs returns a closure that runs `dokku --quiet <subcommand>
// <target> <extra...>`. It is used by domains plan paths to share the
// boilerplate around constructing the subprocess call.
func applyDokkuArgs(subcommand, target string, extra []string, finalState State, errState State) func() TaskOutputState {
	inputs := dokkuArgsInputs(subcommand, target, extra)
	return func() TaskOutputState {
		return runExecInputs(TaskOutputState{State: errState}, finalState, inputs)
	}
}

// validateDomainsTask validates the domains task parameters
func validateDomainsTask(t DomainsTask, requireDomains bool) error {
	if t.Global && t.App != "" {
		return fmt.Errorf("'app' must not be set when 'global' is set to true")
	}
	if !t.Global && t.App == "" {
		return fmt.Errorf("'app' is required when 'global' is not set to true")
	}
	if requireDomains && len(t.Domains) == 0 {
		return fmt.Errorf("'domains' must not be empty for state '%s'", t.State)
	}
	return nil
}

// getDomains fetches current domains for an app or globally
func getDomains(app string, global bool) (map[string]bool, error) {
	reportFlag := "--domains-app-vhosts"
	args := []string{
		"domains:report",
		app,
		reportFlag,
	}
	if global {
		args = []string{
			"domains:report",
			"--global",
			"--domains-global-vhosts",
		}
	}

	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    args,
	})
	if err != nil {
		return nil, err
	}

	domains := map[string]bool{}
	for _, domain := range strings.Fields(result.StdoutContents()) {
		domains[domain] = true
	}
	return domains, nil
}

// init registers the DomainsTask with the task registry
func init() {
	RegisterTask(&DomainsTask{})
}
