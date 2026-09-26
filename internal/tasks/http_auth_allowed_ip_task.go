package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/dokku/docket/internal/subprocess"
)

// HttpAuthAllowedIpTask manages the set of IP addresses allowed to bypass HTTP
// auth for a dokku application
type HttpAuthAllowedIpTask struct {
	// App is the name of the app
	App string `required:"true" identity:"key" yaml:"app" description:"Name of the app"`

	// AllowedIps is the list of IP addresses to allow or remove
	AllowedIps []string `required:"false" identity:"collection" yaml:"allowed_ips,omitempty" description:"List of IP addresses to allow or remove; omit for state 'clear'"`

	// State is the desired state of the allowed IP entries
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent,set,clear" description:"Desired state of the allowed IP entries"`
}

// HttpAuthAllowedIpTaskExample contains an example of an HttpAuthAllowedIpTask
type HttpAuthAllowedIpTaskExample struct {
	// Name is the task name holding the HttpAuthAllowedIpTask description
	Name string `yaml:"-"`

	// DokkuHttpAuthAllowedIp is the HttpAuthAllowedIpTask configuration
	DokkuHttpAuthAllowedIp HttpAuthAllowedIpTask `yaml:"dokku_http_auth_allowed_ip"`
}

// GetName returns the name of the example
func (e HttpAuthAllowedIpTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the HTTP auth allowed ip task
func (t HttpAuthAllowedIpTask) Doc() string {
	return "Manages the set of IP addresses allowed to bypass HTTP auth for a dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t HttpAuthAllowedIpTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t HttpAuthAllowedIpTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// Requirements lists the non-core dokku plugins this task depends on.
func (t HttpAuthAllowedIpTask) Requirements() []string {
	return []string{"dokku-http-auth plugin >= 0.14.0"}
}

// Examples returns a list of HttpAuthAllowedIpTaskExamples as yaml
func (t HttpAuthAllowedIpTask) Examples() ([]Doc, error) {
	return MarshalExamples([]HttpAuthAllowedIpTaskExample{
		{
			Name: "Allow IP addresses to bypass HTTP auth for an app",
			DokkuHttpAuthAllowedIp: HttpAuthAllowedIpTask{
				App:        "hello-world",
				AllowedIps: []string{"192.0.2.1", "198.51.100.0/24"},
			},
		},
		{
			Name: "Remove an allowed IP address from an app",
			DokkuHttpAuthAllowedIp: HttpAuthAllowedIpTask{
				App:        "hello-world",
				AllowedIps: []string{"192.0.2.1"},
				State:      StateAbsent,
			},
		},
		{
			Name: "Replace the set of allowed IP addresses for an app",
			DokkuHttpAuthAllowedIp: HttpAuthAllowedIpTask{
				App:        "hello-world",
				AllowedIps: []string{"192.0.2.1"},
				State:      StateSet,
			},
		},
		{
			Name: "Clear all allowed IP addresses from an app",
			DokkuHttpAuthAllowedIp: HttpAuthAllowedIpTask{
				App:   "hello-world",
				State: StateClear,
			},
		},
	})
}

// Execute manages the app's HTTP auth allowed IPs
func (t HttpAuthAllowedIpTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// Validate checks the HttpAuthAllowedIpTask's inputs without contacting the server.
func (t HttpAuthAllowedIpTask) Validate() error {
	if t.App == "" {
		return fmt.Errorf("'app' is required")
	}
	if t.State == StatePresent && len(t.AllowedIps) == 0 {
		return fmt.Errorf("'allowed_ips' must not be empty for state 'present'")
	}
	if t.State == StateAbsent && len(t.AllowedIps) == 0 {
		return fmt.Errorf("'allowed_ips' must not be empty for state 'absent'")
	}
	if t.State == StateSet && len(t.AllowedIps) == 0 {
		return fmt.Errorf("'allowed_ips' must not be empty for state 'set'")
	}
	// http-auth:set-allowed-ips is called with no addresses to clear, so a list
	// supplied alongside it would be silently discarded rather than removed.
	if t.State == StateClear && len(t.AllowedIps) > 0 {
		return fmt.Errorf("'allowed_ips' must not be set for state 'clear'")
	}
	// Only the states that write addresses are checked. The plugin validates in
	// http-auth:add-allowed-ip and http-auth:set-allowed-ips but deliberately
	// not in http-auth:remove-allowed-ip, so an address a pre-0.14.0 plugin
	// stored without checking stays removable through state 'absent'.
	if t.State == StatePresent || t.State == StateSet {
		for i, ip := range t.AllowedIps {
			if !validHttpAuthAddress(ip) {
				return fmt.Errorf("'allowed_ips' must be an ip address, a cidr block, 'all' or a 'unix:' path for allowed_ips[%d], got %q", i, ip)
			}
		}
	}
	return nil
}

// Plan reports the drift the HttpAuthAllowedIpTask would produce.
func (t HttpAuthAllowedIpTask) Plan(ctx context.Context) PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planHttpAuthAllowedIpsPresent(ctx, t) },
		StateAbsent:  func() PlanResult { return planHttpAuthAllowedIpsAbsent(ctx, t) },
		StateSet:     func() PlanResult { return planHttpAuthAllowedIpsSet(ctx, t) },
		StateClear:   func() PlanResult { return planHttpAuthAllowedIpsClear(ctx, t) },
	})
}

// planHttpAuthAllowedIpsPresent reports drift for the present-state address add.
func planHttpAuthAllowedIpsPresent(ctx context.Context, t HttpAuthAllowedIpTask) PlanResult {
	current, err := getHttpAuthAllowedIps(ctx, t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	toAdd := []string{}
	mutations := []string{}
	for _, ip := range t.AllowedIps {
		if !current[ip] {
			toAdd = append(toAdd, ip)
			mutations = append(mutations, "add "+ip)
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
	for _, ip := range toAdd {
		inputs = append(inputs, subprocess.ExecCommandInput{
			Command: "dokku",
			Args:    []string{"--quiet", "http-auth:add-allowed-ip", t.App, ip},
		})
	}
	return PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%d allowed ip(s) to add", len(toAdd)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
		},
	}
}

// planHttpAuthAllowedIpsAbsent reports drift for the absent-state address remove.
func planHttpAuthAllowedIpsAbsent(ctx context.Context, t HttpAuthAllowedIpTask) PlanResult {
	current, err := getHttpAuthAllowedIps(ctx, t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	toRemove := []string{}
	mutations := []string{}
	for _, ip := range t.AllowedIps {
		if current[ip] {
			toRemove = append(toRemove, ip)
			mutations = append(mutations, "remove "+ip)
		}
	}
	if len(toRemove) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	inputs := make([]subprocess.ExecCommandInput, 0, len(toRemove))
	for _, ip := range toRemove {
		inputs = append(inputs, subprocess.ExecCommandInput{
			Command: "dokku",
			Args:    []string{"--quiet", "http-auth:remove-allowed-ip", t.App, ip},
		})
	}
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("%d allowed ip(s) to remove", len(toRemove)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateAbsent, inputs)
		},
	}
}

// planHttpAuthAllowedIpsSet reports drift for the set-state full replacement.
func planHttpAuthAllowedIpsSet(ctx context.Context, t HttpAuthAllowedIpTask) PlanResult {
	current, err := getHttpAuthAllowedIps(ctx, t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	desired := map[string]bool{}
	for _, ip := range t.AllowedIps {
		desired[ip] = true
	}
	mutations := []string{}
	for _, ip := range sortedSetKeys(desired) {
		if !current[ip] {
			mutations = append(mutations, "add "+ip)
		}
	}
	for _, ip := range sortedSetKeys(current) {
		if !desired[ip] {
			mutations = append(mutations, "remove "+ip)
		}
	}
	if len(mutations) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	status := PlanStatusModify
	if len(current) == 0 {
		status = PlanStatusCreate
	}
	inputs := dokkuArgsInputs("http-auth:set-allowed-ips", t.App, t.AllowedIps)
	return PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%d allowed ip change(s)", len(mutations)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply:     applyDokkuArgs("http-auth:set-allowed-ips", t.App, t.AllowedIps, StateSet, StateAbsent),
	}
}

// planHttpAuthAllowedIpsClear reports drift for the clear-state operation.
func planHttpAuthAllowedIpsClear(ctx context.Context, t HttpAuthAllowedIpTask) PlanResult {
	current, err := getHttpAuthAllowedIps(ctx, t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	if len(current) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	ips := sortedSetKeys(current)
	mutations := make([]string, 0, len(ips))
	for _, ip := range ips {
		mutations = append(mutations, "remove "+ip)
	}
	inputs := dokkuArgsInputs("http-auth:set-allowed-ips", t.App, nil)
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("clear %d allowed ip(s)", len(current)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply:     applyDokkuArgs("http-auth:set-allowed-ips", t.App, nil, StateClear, StatePresent),
	}
}

// httpAuthIPv4Pattern, httpAuthIPv6ShapePattern and httpAuthPrefixPattern back
// validHttpAuthAddress and mirror the bash patterns the plugin uses.
var (
	httpAuthIPv4Pattern      = regexp.MustCompile(`^[0-9]{1,3}(\.[0-9]{1,3}){3}$`)
	httpAuthIPv6ShapePattern = regexp.MustCompile(`^[0-9A-Fa-f:]+$`)
	httpAuthPrefixPattern    = regexp.MustCompile(`^[0-9]{1,3}$`)
)

// validHttpAuthAddress reports whether an address is one nginx's allow
// directive accepts. It mirrors fn-http-auth-valid-address in dokku-http-auth
// so docket never refuses an address the plugin would store: `all` and a
// `unix:` socket path are taken verbatim by nginx rather than as addresses, and
// an IPv6 host is checked by shape rather than parsed, because the compressed
// forms are fiddly enough that a strict parser risks rejecting an address nginx
// accepts while a typo still fails the shape check.
func validHttpAuthAddress(address string) bool {
	if address == "" {
		return false
	}
	if address == "all" || strings.HasPrefix(address, "unix:") {
		return true
	}

	// a slash obliges a prefix length, so `10.0.0.0/` is not an address
	host, prefix := address, ""
	hasPrefix := false
	if slash := strings.Index(address, "/"); slash >= 0 {
		host, prefix, hasPrefix = address[:slash], address[slash+1:], true
	}

	if strings.Contains(host, ":") {
		if !httpAuthIPv6ShapePattern.MatchString(host) {
			return false
		}
		// a second :: run leaves the elision ambiguous
		if elision := strings.Index(host, "::"); elision >= 0 && strings.Contains(host[elision+2:], "::") {
			return false
		}
		if strings.HasPrefix(host, ":") && !strings.HasPrefix(host, "::") {
			return false
		}
		if strings.HasSuffix(host, ":") && !strings.HasSuffix(host, "::") {
			return false
		}
		return !hasPrefix || validHttpAuthPrefix(prefix, 128)
	}

	if !validHttpAuthIPv4(host) {
		return false
	}
	return !hasPrefix || validHttpAuthPrefix(prefix, 32)
}

// validHttpAuthIPv4 reports whether a string is a dotted-quad IPv4 address,
// mirroring fn-http-auth-valid-ipv4.
func validHttpAuthIPv4(address string) bool {
	if !httpAuthIPv4Pattern.MatchString(address) {
		return false
	}
	for _, octet := range strings.Split(address, ".") {
		// the pattern already caps each octet at three digits, so the only way
		// this reads badly is a value above 255
		value, err := strconv.Atoi(octet)
		if err != nil || value > 255 {
			return false
		}
	}
	return true
}

// validHttpAuthPrefix reports whether a cidr prefix length is numeric and
// within a maximum, mirroring fn-http-auth-valid-prefix.
func validHttpAuthPrefix(prefix string, maximum int) bool {
	if !httpAuthPrefixPattern.MatchString(prefix) {
		return false
	}
	value, err := strconv.Atoi(prefix)
	return err == nil && value <= maximum
}

// getHttpAuthAllowedIps reads the current set of allowed IPs for an app from the
// `allowed-ips` key of `http-auth:report --format json`. The plugin strips the
// `http-auth-` prefix from JSON report keys (so the key is `allowed-ips`, not
// `http-auth-allowed-ips`) and emits the addresses as a single space-separated
// string. Addresses are stored verbatim by `http-auth:add-allowed-ip` and
// `http-auth:set-allowed-ips` (no normalization), so comparing the desired
// values against this stored form is fully drift-detectable. A transport-level
// failure (`*subprocess.SSHError`) is propagated; a dokku-level non-zero exit
// (e.g. app does not exist) is treated as "no allowed ips"; malformed JSON
// surfaces as an error.
func getHttpAuthAllowedIps(ctx context.Context, appName string) (map[string]bool, error) {
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
		AllowedIps string `json:"allowed-ips"`
	}
	if err := json.Unmarshal(result.StdoutBytes(), &report); err != nil {
		return nil, err
	}

	ips := map[string]bool{}
	for _, ip := range strings.Fields(report.AllowedIps) {
		ips[ip] = true
	}
	return ips, nil
}

// ExportApp reconstructs the app's HTTP-auth allowed IPs, or nil when none are
// set. state:set replaces the whole allow-list for an exact match.
func (t HttpAuthAllowedIpTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	ips, err := getHttpAuthAllowedIps(ctx, app)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, nil
	}
	return []interface{}{HttpAuthAllowedIpTask{App: app, AllowedIps: sortedSetKeys(ips), State: StateSet}}, nil
}

// init registers the HttpAuthAllowedIpTask with the task registry
func init() {
	RegisterTask(&HttpAuthAllowedIpTask{})
}
