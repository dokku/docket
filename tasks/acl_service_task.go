package tasks

import (
	"fmt"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// AclServiceTask manages the dokku-acl access list for a dokku service
type AclServiceTask struct {
	// Service is the name of the service instance
	Service string `required:"true" yaml:"service"`

	// Type is the type of service (e.g. redis, postgres)
	Type string `required:"true" yaml:"type"`

	// Users is the list of users to add or remove from the ACL
	Users []string `required:"false" yaml:"users"`

	// State is the desired state of the ACL entries
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// AclServiceTaskExample contains an example of an AclServiceTask
type AclServiceTaskExample struct {
	// Name is the task name holding the AclServiceTask description
	Name string `yaml:"-"`

	// AclServiceTask is the AclServiceTask configuration
	AclServiceTask AclServiceTask `yaml:"dokku_acl_service"`
}

// GetName returns the name of the example
func (e AclServiceTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the acl service task
func (t AclServiceTask) Doc() string {
	return "Manages the dokku-acl access list for a dokku service"
}

// Examples returns the examples for the acl service task
func (t AclServiceTask) Examples() ([]Doc, error) {
	return MarshalExamples([]AclServiceTaskExample{
		{
			Name: "Grant users access to a redis service",
			AclServiceTask: AclServiceTask{
				Service: "my-redis",
				Type:    "redis",
				Users:   []string{"alice", "bob"},
			},
		},
		{
			Name: "Revoke a user's access to a redis service",
			AclServiceTask: AclServiceTask{
				Service: "my-redis",
				Type:    "redis",
				Users:   []string{"bob"},
				State:   StateAbsent,
			},
		},
		{
			Name: "Clear the entire ACL for a redis service",
			AclServiceTask: AclServiceTask{
				Service: "my-redis",
				Type:    "redis",
				State:   StateAbsent,
			},
		},
	})
}

// Execute manages the service ACL
func (t AclServiceTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the AclServiceTask would produce.
func (t AclServiceTask) Plan() PlanResult {
	if err := validateAclServiceTask(t); err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			if len(t.Users) == 0 {
				return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("'users' must not be empty for state 'present'")}
			}
			current, err := getAclServiceUsers(t.Type, t.Service)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			toAdd := []string{}
			mutations := []string{}
			for _, u := range t.Users {
				if !current[u] {
					toAdd = append(toAdd, u)
					mutations = append(mutations, "add "+u)
				}
			}
			if len(toAdd) == 0 {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := make([]subprocess.ExecCommandInput, 0, len(toAdd))
			for _, u := range toAdd {
				inputs = append(inputs, subprocess.ExecCommandInput{
					Command: "dokku",
					Args:    []string{"--quiet", "acl:add-service", t.Type, t.Service, u},
				})
			}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusModify,
				Reason:    fmt.Sprintf("%d user(s) to add", len(toAdd)),
				Mutations: mutations,
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			current, err := getAclServiceUsers(t.Type, t.Service)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			toRemove := []string{}
			mutations := []string{}
			if len(t.Users) == 0 {
				for u := range current {
					toRemove = append(toRemove, u)
					mutations = append(mutations, "remove "+u)
				}
			} else {
				for _, u := range t.Users {
					if current[u] {
						toRemove = append(toRemove, u)
						mutations = append(mutations, "remove "+u)
					}
				}
			}
			if len(toRemove) == 0 {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := make([]subprocess.ExecCommandInput, 0, len(toRemove))
			for _, u := range toRemove {
				inputs = append(inputs, subprocess.ExecCommandInput{
					Command: "dokku",
					Args:    []string{"--quiet", "acl:remove-service", t.Type, t.Service, u},
				})
			}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    fmt.Sprintf("%d user(s) to remove", len(toRemove)),
				Mutations: mutations,
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// getAclServiceUsers reads the current ACL for a service via
// `acl:list-service TYPE SERVICE`. The plugin's `cmd-acl-list-service`
// emits one username per line on STDERR (via `ls -1 ... >&2`), unlike
// `acl:list` which uses stdout, so we read stderr here.
func getAclServiceUsers(serviceType, service string) (map[string]bool, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "acl:list-service", serviceType, service},
	})
	if err != nil {
		return nil, err
	}

	users := map[string]bool{}
	for _, line := range strings.Split(result.StderrContents(), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		users[trimmed] = true
	}
	return users, nil
}

// validateAclServiceTask checks the required fields shared by both states
func validateAclServiceTask(t AclServiceTask) error {
	if t.Service == "" {
		return fmt.Errorf("'service' is required")
	}
	if t.Type == "" {
		return fmt.Errorf("'type' is required")
	}
	return nil
}

// init registers the AclServiceTask with the task registry
func init() {
	RegisterTask(&AclServiceTask{})
}
