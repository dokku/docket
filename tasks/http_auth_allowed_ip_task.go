package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// HttpAuthAllowedIpTask manages the set of IP addresses allowed to bypass HTTP
// auth for a dokku application
type HttpAuthAllowedIpTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app" description:"Name of the app"`

	// AllowedIps is the list of IP addresses to allow or remove
	AllowedIps []string `required:"false" yaml:"allowed_ips" description:"List of IP addresses to allow or remove"`

	// State is the desired state of the allowed IP entries
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the allowed IP entries"`
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

// Requirements lists the non-core dokku plugins this task depends on.
func (t HttpAuthAllowedIpTask) Requirements() []string {
	return []string{"dokku-http-auth plugin"}
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
			Name: "Remove all allowed IP addresses from an app",
			DokkuHttpAuthAllowedIp: HttpAuthAllowedIpTask{
				App:   "hello-world",
				State: StateAbsent,
			},
		},
	})
}

// Execute manages the app's HTTP auth allowed IPs
func (t HttpAuthAllowedIpTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Validate checks the HttpAuthAllowedIpTask's inputs without contacting the server.
func (t HttpAuthAllowedIpTask) Validate() error {
	if t.App == "" {
		return fmt.Errorf("'app' is required")
	}
	if t.State == StatePresent && len(t.AllowedIps) == 0 {
		return fmt.Errorf("'allowed_ips' must not be empty for state 'present'")
	}
	return nil
}

// Plan reports the drift the HttpAuthAllowedIpTask would produce.
func (t HttpAuthAllowedIpTask) Plan() PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			current, err := getHttpAuthAllowedIps(t.App)
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
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			current, err := getHttpAuthAllowedIps(t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			toRemove := []string{}
			if len(t.AllowedIps) == 0 {
				for ip := range current {
					toRemove = append(toRemove, ip)
				}
				sort.Strings(toRemove)
			} else {
				for _, ip := range t.AllowedIps {
					if current[ip] {
						toRemove = append(toRemove, ip)
					}
				}
			}
			if len(toRemove) == 0 {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			mutations := make([]string, 0, len(toRemove))
			inputs := make([]subprocess.ExecCommandInput, 0, len(toRemove))
			for _, ip := range toRemove {
				mutations = append(mutations, "remove "+ip)
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
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// getHttpAuthAllowedIps reads the current set of allowed IPs for an app from the
// `allowed-ips` key of `http-auth:report --format json`. The plugin strips the
// `http-auth-` prefix from JSON report keys (so the key is `allowed-ips`, not
// `http-auth-allowed-ips`) and emits the addresses as a single space-separated
// string. Addresses are stored verbatim by `http-auth:add-allowed-ip` (no
// normalization), so comparing the desired values against this stored form is
// fully drift-detectable. A transport-level failure (`*subprocess.SSHError`) is
// propagated; a dokku-level non-zero exit (e.g. app does not exist) is treated
// as "no allowed ips"; malformed JSON surfaces as an error.
func getHttpAuthAllowedIps(appName string) (map[string]bool, error) {
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
// set.
func (t HttpAuthAllowedIpTask) ExportApp(app string) ([]interface{}, error) {
	ips, err := getHttpAuthAllowedIps(app)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, nil
	}
	return []interface{}{HttpAuthAllowedIpTask{App: app, AllowedIps: sortedSetKeys(ips)}}, nil
}

// init registers the HttpAuthAllowedIpTask with the task registry
func init() {
	RegisterTask(&HttpAuthAllowedIpTask{})
}
