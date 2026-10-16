package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dokku/docket/internal/subprocess"
)

// HttpAuthDomainTask manages the set of domains HTTP auth is restricted to for a
// dokku application
type HttpAuthDomainTask struct {
	// App is the name of the app
	App string `required:"true" identity:"key" yaml:"app" description:"Name of the app"`

	// Domains is the list of domains to restrict HTTP auth to
	Domains []string `required:"false" identity:"collection" ordered:"false" yaml:"domains,omitempty" description:"List of domains to restrict HTTP auth to; omit for state 'clear'"`

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

// ProbeSupport reports whether Plan() can read this task's current state.
func (t HttpAuthDomainTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// MinimumVersions reports the oldest Dokku, and any plugin, this task's probe parses.
// The probe is `http-auth:report`. 0.38.31 is the Dokku the integration suite runs.
func (t HttpAuthDomainTask) MinimumVersions() VersionSupport {
	return VersionSupport{
		Dokku: "0.38.31",
		Plugins: []PluginRequirement{{
			Name:    "http-auth",
			Minimum: "0.14.0",
			Text:    "dokku-http-auth plugin >= 0.14.0",
		}},
	}
}

// examples returns a list of HttpAuthDomainTaskExamples as yaml
func (t HttpAuthDomainTask) examples() ([]Doc, error) {
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
func (t HttpAuthDomainTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
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
	// http-auth:set-domains is called with no domains to clear, so a list
	// supplied alongside it would be silently discarded rather than removed.
	if t.State == StateClear && len(t.Domains) > 0 {
		return fmt.Errorf("'domains' must not be set for state 'clear'")
	}
	return nil
}

// Plan reports the drift the HttpAuthDomainTask would produce.
func (t HttpAuthDomainTask) Plan(ctx context.Context) PlanResult {
	if r, ok := gatePlan(ctx, t); !ok {
		return r
	}
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planHttpAuthDomainsPresent(ctx, t) },
		StateAbsent:  func() PlanResult { return planHttpAuthDomainsAbsent(ctx, t) },
		StateSet:     func() PlanResult { return planHttpAuthDomainsSet(ctx, t) },
		StateClear:   func() PlanResult { return planHttpAuthDomainsClear(ctx, t) },
	})
}

// planHttpAuthDomainsPresent reports drift for the present-state domain add.
func planHttpAuthDomainsPresent(ctx context.Context, t HttpAuthDomainTask) PlanResult {
	current, err := getHttpAuthDomains(ctx, t.App)
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
		return observedHttpAuthDomains(t, PlanResult{InSync: true, Status: PlanStatusOK}, current, false)
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
	return observedHttpAuthDomains(t, PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%d domain(s) to add", len(toAdd)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
		},
	}, current, false)
}

// planHttpAuthDomainsAbsent reports drift for the absent-state domain remove.
func planHttpAuthDomainsAbsent(ctx context.Context, t HttpAuthDomainTask) PlanResult {
	current, err := getHttpAuthDomains(ctx, t.App)
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
		return observedHttpAuthDomains(t, PlanResult{InSync: true, Status: PlanStatusOK}, current, false)
	}
	inputs := make([]subprocess.ExecCommandInput, 0, len(toRemove))
	for _, d := range toRemove {
		inputs = append(inputs, subprocess.ExecCommandInput{
			Command: "dokku",
			Args:    []string{"--quiet", "http-auth:remove-domain", t.App, d},
		})
	}
	return observedHttpAuthDomains(t, PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("%d domain(s) to remove", len(toRemove)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateAbsent, inputs)
		},
	}, current, false)
}

// planHttpAuthDomainsSet reports drift for the set-state full replacement.
func planHttpAuthDomainsSet(ctx context.Context, t HttpAuthDomainTask) PlanResult {
	current, err := getHttpAuthDomains(ctx, t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	desired := map[string]bool{}
	for _, d := range t.Domains {
		desired[d] = true
	}
	mutations := []string{}
	for _, d := range sortedSetKeys(desired) {
		if !current[d] {
			mutations = append(mutations, "add "+d)
		}
	}
	for _, d := range sortedSetKeys(current) {
		if !desired[d] {
			mutations = append(mutations, "remove "+d)
		}
	}
	if len(mutations) == 0 {
		return observedHttpAuthDomains(t, PlanResult{InSync: true, Status: PlanStatusOK}, current, true)
	}
	status := PlanStatusModify
	if len(current) == 0 {
		status = PlanStatusCreate
	}
	inputs := dokkuArgsInputs("http-auth:set-domains", t.App, t.Domains)
	return observedHttpAuthDomains(t, PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%d domain change(s)", len(mutations)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply:     applyDokkuArgs("http-auth:set-domains", t.App, t.Domains, StateSet, StateAbsent),
	}, current, true)
}

// planHttpAuthDomainsClear reports drift for the clear-state operation.
func planHttpAuthDomainsClear(ctx context.Context, t HttpAuthDomainTask) PlanResult {
	current, err := getHttpAuthDomains(ctx, t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	if len(current) == 0 {
		return observedHttpAuthDomains(t, PlanResult{InSync: true, Status: PlanStatusOK}, current, true)
	}
	domains := sortedSetKeys(current)
	mutations := make([]string, 0, len(domains))
	for _, d := range domains {
		mutations = append(mutations, "remove "+d)
	}
	inputs := dokkuArgsInputs("http-auth:set-domains", t.App, nil)
	return observedHttpAuthDomains(t, PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("clear %d domain(s)", len(current)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply:     applyDokkuArgs("http-auth:set-domains", t.App, nil, StateClear, StatePresent),
	}, current, true)
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
func getHttpAuthDomains(ctx context.Context, appName string) (map[string]bool, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
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

// observedHttpAuthDomains attaches the auth domains the probe read to r, through
// httpAuthObservedMembers.
func observedHttpAuthDomains(t HttpAuthDomainTask, r PlanResult, current map[string]bool, whole bool) PlanResult {
	held, state := httpAuthObservedMembers(current, t.Domains, whole)
	return observed(r, HttpAuthDomainTask{App: t.App, Domains: held, State: state}, len(held) > 0)
}

// httpAuthObservedMembers returns the members of a string set the probe read
// and the state they amount to, for the http-auth list tasks. An additive
// plan (present, absent) reads only the members it declares, as present or
// absent; a plan that owns the list (set, clear) reads every member, as set,
// or as clear when there are none. The members are sorted, the order export
// emits them in.
func httpAuthObservedMembers(current map[string]bool, declared []string, whole bool) ([]string, State) {
	var held []string
	if whole {
		if len(current) > 0 {
			held = sortedSetKeys(current)
		}
	} else {
		seen := map[string]bool{}
		for _, m := range declared {
			if current[m] && !seen[m] {
				held = append(held, m)
				seen[m] = true
			}
		}
		sort.Strings(held)
	}
	switch {
	case whole && len(held) > 0:
		return held, StateSet
	case whole:
		return held, StateClear
	case len(held) > 0:
		return held, StatePresent
	}
	return held, StateAbsent
}

// ExportApp reconstructs the domains HTTP auth is restricted to, or nil when
// none are set. state:set replaces the whole domain set for an exact match.
func (t HttpAuthDomainTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	domains, err := getHttpAuthDomains(ctx, app)
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
