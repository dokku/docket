package tasks

import (
	"fmt"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// AclAppTask manages the dokku-acl access list for a dokku application
type AclAppTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app" description:"Name of the app"`

	// Users is the list of users to add or remove from the ACL
	Users []string `required:"false" yaml:"users" description:"List of users to add or remove from the ACL"`

	// State is the desired state of the ACL entries
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the ACL entries"`
}

// AclAppTaskExample contains an example of an AclAppTask
type AclAppTaskExample struct {
	// Name is the task name holding the AclAppTask description
	Name string `yaml:"-"`

	// AclAppTask is the AclAppTask configuration
	AclAppTask AclAppTask `yaml:"dokku_acl_app"`
}

// GetName returns the name of the example
func (e AclAppTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the acl app task
func (t AclAppTask) Doc() string {
	return "Manages the dokku-acl access list for a dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t AclAppTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Requirements lists the non-core dokku plugins this task depends on.
func (t AclAppTask) Requirements() []string {
	return []string{"dokku-acl plugin"}
}

// Examples returns the examples for the acl app task
func (t AclAppTask) Examples() ([]Doc, error) {
	return MarshalExamples([]AclAppTaskExample{
		{
			Name: "Grant users access to an app",
			AclAppTask: AclAppTask{
				App:   "node-js-app",
				Users: []string{"alice", "bob"},
			},
		},
		{
			Name: "Revoke a user's access to an app",
			AclAppTask: AclAppTask{
				App:   "node-js-app",
				Users: []string{"bob"},
				State: StateAbsent,
			},
		},
		{
			Name: "Clear the entire ACL for an app",
			AclAppTask: AclAppTask{
				App:   "node-js-app",
				State: StateAbsent,
			},
		},
	})
}

// Execute manages the app ACL
func (t AclAppTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Validate checks the AclAppTask's inputs without contacting the server, so
// `docket validate` and Plan() surface the same errors.
func (t AclAppTask) Validate() error {
	if t.App == "" {
		return fmt.Errorf("'app' is required")
	}
	if t.State == StatePresent && len(t.Users) == 0 {
		return fmt.Errorf("'users' must not be empty for state 'present'")
	}
	return nil
}

// Plan reports the drift the AclAppTask would produce.
func (t AclAppTask) Plan() PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			current, err := getAclAppUsers(t.App)
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
					Args:    []string{"--quiet", "acl:add", t.App, u},
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
			current, err := getAclAppUsers(t.App)
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
					Args:    []string{"--quiet", "acl:remove", t.App, u},
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

// getAclAppUsers reads the current ACL for an app via `acl:list APP`. The
// plugin emits one username per line; an empty ACL produces no output.
func getAclAppUsers(app string) (map[string]bool, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "acl:list", app},
	})
	if err != nil {
		return nil, err
	}

	users := map[string]bool{}
	for _, line := range strings.Split(result.StdoutContents(), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		users[trimmed] = true
	}
	return users, nil
}

// ExportApp reconstructs the app's ACL user list, or nil when it is empty.
func (t AclAppTask) ExportApp(app string) ([]interface{}, error) {
	users, err := getAclAppUsers(app)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, nil
	}
	return []interface{}{AclAppTask{App: app, Users: sortedSetKeys(users)}}, nil
}

// init registers the AclAppTask with the task registry
func init() {
	RegisterTask(&AclAppTask{})
}
