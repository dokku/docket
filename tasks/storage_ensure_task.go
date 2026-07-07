package tasks

import (
	"errors"
	"github.com/dokku/docket/subprocess"
)

// StorageEnsureTask manages the storage for a given dokku application
type StorageEnsureTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app" description:"Name of the app"`

	// Chown is the chown value to set
	Chown string `required:"false" yaml:"chown,omitempty" description:"Chown value to set"`

	// State is the desired state of the storage
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the storage"`
}

// StorageEnsureTaskExample contains an example of a StorageEnsureTask
type StorageEnsureTaskExample struct {
	// Name is the task name holding the StorageEnsureTask description
	Name string `yaml:"-"`

	// StorageEnsureTask is the StorageEnsureTask configuration
	StorageEnsureTask StorageEnsureTask `yaml:"dokku_storage_ensure"`
}

// GetName returns the name of the example
func (e StorageEnsureTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the storage ensure task
func (t StorageEnsureTask) Doc() string {
	return "Ensures the storage for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t StorageEnsureTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportUnsupported, Caveat: "deprecated; storage state is exported via dokku_storage_mount"}
}

// Deprecation marks dokku_storage_ensure as deprecated. dokku's
// underlying storage:ensure-directory subcommand has been deprecated in
// favor of storage:create, which docket exposes through
// dokku_storage_entry.
func (t StorageEnsureTask) Deprecation() string {
	return "use dokku_storage_entry instead; dokku's storage:ensure-directory has been deprecated in favor of storage:create"
}

// Examples returns the examples for the storage ensure task
func (t StorageEnsureTask) Examples() ([]Doc, error) {
	return MarshalExamples([]StorageEnsureTaskExample{
		{
			Name: "Ensure a storage directory owned by the herokuish user",
			StorageEnsureTask: StorageEnsureTask{
				App:   "node-js-app",
				Chown: "herokuish",
			},
		},
		{
			Name: "Ensure a storage directory owned by root",
			StorageEnsureTask: StorageEnsureTask{
				App:   "node-js-app",
				Chown: "root",
			},
		},
	})
}

// Execute ensures the storage for a given app
func (t StorageEnsureTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Validate checks the StorageEnsureTask's inputs without contacting the server.
func (t StorageEnsureTask) Validate() error {
	if t.State == StatePresent {
		chownValues := map[string]bool{
			"heroku": true, "herokuish": true, "paketo": true, "root": true, "false": true,
		}
		if !chownValues[t.Chown] {
			return errors.New("invalid chown value specified")
		}
	}
	if t.State == StateAbsent {
		return errors.New("the absent state is not supported for storage:ensure")
	}
	return nil
}

// Plan reports the drift the StorageEnsureTask would produce. dokku does
// not expose a probe for storage:ensure-directory, so the plan reports
// drift unconditionally.
func (t StorageEnsureTask) Plan() PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "storage:ensure-directory", "--chown", t.Chown, t.App},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusModify,
				Reason:    "directory presence not probed",
				Mutations: []string{"storage:ensure-directory --chown " + t.Chown + " " + t.App},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
	})
}

// init registers the StorageEnsureTask with the task registry
func init() {
	RegisterTask(&StorageEnsureTask{})
}
