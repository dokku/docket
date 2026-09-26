package tasks

import (
	"context"
	"errors"
	"fmt"

	"github.com/dokku/docket/subprocess"
)

// AclServiceTask manages the dokku-acl access list for a dokku service
type AclServiceTask struct {
	// Service is the name of the service instance
	Service string `required:"true" identity:"key" yaml:"service" description:"Name of the service instance"`

	// Type is the type of service (e.g. redis, postgres)
	Type string `required:"true" identity:"key" yaml:"type" description:"Type of service (e.g. redis, postgres)"`

	// Users is the list of users to add, remove, or set on the ACL
	Users []string `required:"false" identity:"collection" yaml:"users,omitempty" description:"List of users to add, remove, or set on the ACL; omit for state 'clear'"`

	// State is the desired state of the ACL entries
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent,set,clear" description:"Desired state of the ACL entries"`
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

// ExportSupport reports how docket export handles this task.
func (t AclServiceTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t AclServiceTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// ExportGlobal reconstructs the dokku-acl access list of every datastore
// service on the server. Discovery is via listServices; the members are read
// from `acl:list-service <type> <name>` (reusing getAclServiceUsers). Note the
// field inversion versus the other service tasks: Service holds the instance
// name and Type the datastore type. Services with no ACL entries - and every
// service when the dokku-acl plugin is absent - are skipped. state:set
// replaces the whole list for an exact match.
func (t AclServiceTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	services, err := listServices(ctx)
	if err != nil {
		return nil, err
	}
	var out []interface{}
	for _, s := range services {
		users, err := getAclServiceUsers(ctx, s.Type, s.Name)
		if err != nil {
			var sshErr *subprocess.SSHError
			if errors.As(err, &sshErr) {
				return nil, err
			}
			// acl:list-service fails when the dokku-acl plugin is not installed;
			// treat that (and any other dokku-level failure) as "no ACL" so
			// export stays quiet on servers without dokku-acl.
			continue
		}
		if len(users) == 0 {
			continue
		}
		out = append(out, AclServiceTask{
			Service: s.Name,
			Type:    s.Type,
			Users:   sortedSetKeys(users),
			State:   StateSet,
		})
	}
	return out, nil
}

// Requirements lists the non-core dokku plugins this task depends on.
func (t AclServiceTask) Requirements() []string {
	return []string{"dokku-acl plugin >= 2.0.1"}
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
			Name: "Replace the users with access to a redis service",
			AclServiceTask: AclServiceTask{
				Service: "my-redis",
				Type:    "redis",
				Users:   []string{"alice"},
				State:   StateSet,
			},
		},
		{
			Name: "Clear the entire ACL for a redis service",
			AclServiceTask: AclServiceTask{
				Service: "my-redis",
				Type:    "redis",
				State:   StateClear,
			},
		},
	})
}

// Execute manages the service ACL
func (t AclServiceTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// Validate checks the AclServiceTask's inputs without contacting the server.
func (t AclServiceTask) Validate() error {
	if err := validateAclServiceTask(t); err != nil {
		return err
	}
	return validateAclUsers(t.State, t.Users)
}

// Plan reports the drift the AclServiceTask would produce.
func (t AclServiceTask) Plan(ctx context.Context) PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	probe := func() (map[string]bool, error) { return getAclServiceUsers(ctx, t.Type, t.Service) }
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			return planAclPresent(ctx, probe, t.Users, func(u string) []string {
				return []string{"--quiet", "acl:add-service", t.Type, t.Service, u}
			})
		},
		StateAbsent: func() PlanResult {
			return planAclAbsent(ctx, probe, t.Users, func(u string) []string {
				return []string{"--quiet", "acl:remove-service", t.Type, t.Service, u}
			})
		},
		StateSet: func() PlanResult {
			return planAclSet(ctx, probe, t.Users, aclSetServiceUsersInputs(t.Type, t.Service, t.Users))
		},
		StateClear: func() PlanResult {
			return planAclClear(ctx, probe, aclSetServiceUsersInputs(t.Type, t.Service, nil))
		},
	})
}

// aclSetServiceUsersInputs returns the single `acl:set-service-users` call
// that replaces a service's ACL with users, clearing it when users is empty.
func aclSetServiceUsersInputs(serviceType, service string, users []string) []subprocess.ExecCommandInput {
	args := append([]string{"--quiet", "acl:set-service-users", serviceType, service}, users...)
	return []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
}

// getAclServiceUsers reads the current ACL for a service via
// `acl:list-service TYPE SERVICE`, which emits one username per line.
// dokku-acl 2.0.0 and later print the users on stdout; 1.5.1 and earlier
// print them on stderr (via `ls -1 ... >&2`), so stderr is read when stdout
// is empty.
func getAclServiceUsers(ctx context.Context, serviceType, service string) (map[string]bool, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "acl:list-service", serviceType, service},
	})
	if err != nil {
		return nil, err
	}

	output := result.StdoutContents()
	if output == "" {
		output = result.StderrContents()
	}
	return parseAclUsers(output), nil
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
