package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// HttpAuthDomainTask manages the set of domains HTTP auth is restricted to for a
// dokku application
type HttpAuthDomainTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app" description:"Name of the app"`

	// Domains is the list of domains to restrict HTTP auth to
	Domains []string `required:"false" yaml:"domains" description:"List of domains to restrict HTTP auth to"`

	// State is the desired state of the HTTP auth domain entries
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent,set,clear" description:"Desired state of the HTTP auth domain entries"`
}

// HttpAuthDomainTaskExample contains an example of an HttpAuthDomainTask
type HttpAuthDomainTaskExample struct {
	// Name is the task name holding the HttpAuthDomainTask description
	Name string `yaml:"-"`

	// DokkuHttpAuthDomain is the HttpAuthDomainTask configuration
	DokkuHttpAuthDomain HttpAuthDomainTask `yaml:"dokku_http_auth_domain"`
}

// GetName returns the name of the example
func (e HttpAuthDomainTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the HTTP auth domain task
func (t HttpAuthDomainTask) Doc() string {
	return "Manages the set of domains HTTP auth is restricted to for a dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t HttpAuthDomainTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Requirements lists the non-core dokku plugins this task depends on.
func (t HttpAuthDomainTask) Requirements() []string {
	return []string{"dokku-http-auth plugin"}
}

// Examples returns a list of HttpAuthDomainTaskExamples as yaml
func (t HttpAuthDomainTask) Examples() ([]Doc, error) {
	return MarshalExamples([]HttpAuthDomainTaskExample{
		{
			Name: "Restrict HTTP auth to specific domains for an app",
			DokkuHttpAuthDomain: HttpAuthDomainTask{
				App:     "hello-world",
				Domains: []string{"app.example.com", "www.example.com"},
			},
		},
		{
			Name: "Stop restricting HTTP auth to a domain for an app",
			DokkuHttpAuthDomain: HttpAuthDomainTask{
				App:     "hello-world",
				Domains: []string{"www.example.com"},
				State:   StateAbsent,
			},
		},
		{
			Name: "Replace the set of HTTP auth domains for an app",
			DokkuHttpAuthDomain: HttpAuthDomainTask{
				App:     "hello-world",
				Domains: []string{"app.example.com"},
				State:   StateSet,
			},
		},
		{
			Name: "Clear all HTTP auth domains from an app",
			DokkuHttpAuthDomain: HttpAuthDomainTask{
				App:   "hello-world",
				State: StateClear,
			},
		},
	})
}

// Execute manages the app's HTTP auth domains
func (t HttpAuthDomainTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Validate checks the HttpAuthDomainTask's inputs without contacting the server.
func (t HttpAuthDomainTask) Validate() error {
	if t.App == "" {
		return fmt.Errorf("'app' is required")
	}
	if t.State == StatePresent && len(t.Domains) == 0 {
		return fmt.Errorf("'domains' must not be empty for state 'present'")
	}
	if t.State == StateAbsent && len(t.Domains) == 0 {
		return fmt.Errorf("'domains' must not be empty for state 'absent'")
	}
	if t.State == StateSet && len(t.Domains) == 0 {
		return fmt.Errorf("'domains' must not be empty for state 'set'")
	}
	return nil
}

// Plan reports the drift the HttpAuthDomainTask would produce.
func (t HttpAuthDomainTask) Plan() PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planHttpAuthDomainsPresent(t) },
		StateAbsent:  func() PlanResult { return planHttpAuthDomainsAbsent(t) },
		StateSet:     func() PlanResult { return planHttpAuthDomainsSet(t) },
		StateClear:   func() PlanResult { return planHttpAuthDomainsClear(t) },
	})
}

// planHttpAuthDomainsPresent reports drift for the present-state domain add.
func planHttpAuthDomainsPresent(t HttpAuthDomainTask) PlanResult {
	current, err := getHttpAuthDomains(t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	toAdd := []string{}
	mutations := []string{}
	for _, d := range t.Domains {
		if !current[d] {
			toAdd = append(toAdd, d)
			mutations = append(mutations, "add "+d)
		}
	}
	if len(toAdd) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	status := PlanStatusModify
	if len(current) == 0 {
		status = PlanStatusCreate
	}
	inputs := make([]subprocess.ExecCommandInput, 0, len(toAdd))
	for _, d := range toAdd {
		inputs = append(inputs, subprocess.ExecCommandInput{
			Command: "dokku",
			Args:    []string{"--quiet", "http-auth:add-domain", t.App, d},
		})
	}
	return PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%d domain(s) to add", len(toAdd)),
		Mutations: mutations,
		Commands:  resolveCommands(inputs),
		apply: func() TaskOutputState {
			return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
		},
	}
}

// planHttpAuthDomainsAbsent reports drift for the absent-state domain remove.
func planHttpAuthDomainsAbsent(t HttpAuthDomainTask) PlanResult {
	current, err := getHttpAuthDomains(t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	toRemove := []string{}
	mutations := []string{}
	for _, d := range t.Domains {
		if current[d] {
			toRemove = append(toRemove, d)
			mutations = append(mutations, "remove "+d)
		}
	}
	if len(toRemove) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	inputs := make([]subprocess.ExecCommandInput, 0, len(toRemove))
	for _, d := range toRemove {
		inputs = append(inputs, subprocess.ExecCommandInput{
			Command: "dokku",
			Args:    []string{"--quiet", "http-auth:remove-domain", t.App, d},
		})
	}
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("%d domain(s) to remove", len(toRemove)),
		Mutations: mutations,
		Commands:  resolveCommands(inputs),
		apply: func() TaskOutputState {
			return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
		},
	}
}

// planHttpAuthDomainsSet reports drift for the set-state full replacement.
func planHttpAuthDomainsSet(t HttpAuthDomainTask) PlanResult {
	current, err := getHttpAuthDomains(t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	desired := map[string]bool{}
	for _, d := range t.Domains {
		desired[d] = true
	}
	mutations := []string{}
	for _, d := range t.Domains {
		if !current[d] {
			mutations = append(mutations, "add "+d)
		}
	}
	toRemove := []string{}
	for d := range current {
		if !desired[d] {
			toRemove = append(toRemove, d)
		}
	}
	sort.Strings(toRemove)
	for _, d := range toRemove {
		mutations = append(mutations, "remove "+d)
	}
	if len(mutations) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	inputs := dokkuArgsInputs("http-auth:set-domains", t.App, t.Domains)
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusModify,
		Reason:    fmt.Sprintf("%d domain change(s)", len(mutations)),
		Mutations: mutations,
		Commands:  resolveCommands(inputs),
		apply:     applyDokkuArgs("http-auth:set-domains", t.App, t.Domains, StateSet, StateAbsent),
	}
}

// planHttpAuthDomainsClear reports drift for the clear-state operation.
func planHttpAuthDomainsClear(t HttpAuthDomainTask) PlanResult {
	current, err := getHttpAuthDomains(t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	if len(current) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	domains := make([]string, 0, len(current))
	for d := range current {
		domains = append(domains, d)
	}
	sort.Strings(domains)
	mutations := make([]string, 0, len(domains))
	for _, d := range domains {
		mutations = append(mutations, "remove "+d)
	}
	inputs := dokkuArgsInputs("http-auth:set-domains", t.App, nil)
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("clear %d domain(s)", len(current)),
		Mutations: mutations,
		Commands:  resolveCommands(inputs),
		apply:     applyDokkuArgs("http-auth:set-domains", t.App, nil, StateClear, StatePresent),
	}
}

// getHttpAuthDomains reads the current set of domains HTTP auth is restricted to
// for an app from the `domains` key of `http-auth:report --format json`. The
// plugin strips the `http-auth-` prefix from JSON report keys (so the key is
// `domains`, not `http-auth-domains`) and emits the domains as a single
// space-separated string. `http-auth:add-domain`/`http-auth:set-domains`
// lowercase each domain before storing it, so comparing already-lowercased
// desired values against this stored form is fully drift-detectable; mixed-case
// desired values may report perpetual drift. A transport-level failure
// (`*subprocess.SSHError`) is propagated; a dokku-level non-zero exit (e.g. app
// does not exist) is treated as "no domains"; malformed JSON surfaces as an
// error.
func getHttpAuthDomains(appName string) (map[string]bool, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"http-auth:report",
			appName,
			"--format",
			"json",
		},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return map[string]bool{}, nil
	}

	var report struct {
		Domains string `json:"domains"`
	}
	if err := json.Unmarshal(result.StdoutBytes(), &report); err != nil {
		return nil, err
	}

	domains := map[string]bool{}
	for _, d := range strings.Fields(report.Domains) {
		domains[d] = true
	}
	return domains, nil
}

// ExportApp reconstructs the domains HTTP auth is restricted to, or nil when
// none are set. state:set replaces the whole domain set for an exact match.
func (t HttpAuthDomainTask) ExportApp(app string) ([]interface{}, error) {
	domains, err := getHttpAuthDomains(app)
	if err != nil {
		return nil, err
	}
	if len(domains) == 0 {
		return nil, nil
	}
	return []interface{}{HttpAuthDomainTask{App: app, Domains: sortedSetKeys(domains), State: StateSet}}, nil
}

// init registers the HttpAuthDomainTask with the task registry
func init() {
	RegisterTask(&HttpAuthDomainTask{})
}
