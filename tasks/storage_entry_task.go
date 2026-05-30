package tasks

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dokku/docket/subprocess"
)

// StorageEntryTask manages a named storage registry entry — the thing
// `storage:create` produces and `storage:list-entries` reports. Entries
// exist independently of any single app, and an attachment created via
// dokku_storage_mount references them by name.
//
// Idempotency is keyed on the entry name: when an entry with the given
// name exists, the task is in sync regardless of the other field
// values. Attribute changes are therefore not drift-detected; to change
// scheduler, size, or any other attribute, the recipe must destroy and
// re-create the entry.
type StorageEntryTask struct {
	// Name is the name of the storage entry
	Name string `required:"true" yaml:"name" description:"Name of the storage entry"`

	// Path is the host path for the entry (docker-local scheduler).
	// Optional; defaults to the dokku storage root + name.
	Path string `required:"false" yaml:"path,omitempty" description:"Host path for the entry (docker-local scheduler; defaults to the dokku storage root joined with the entry name)"`

	// Scheduler is the scheduler that backs the entry
	Scheduler string `required:"false" yaml:"scheduler,omitempty" default:"docker-local" description:"Scheduler that backs the entry"`

	// Size is the volume size (scheduler-dependent)
	Size string `required:"false" yaml:"size,omitempty" description:"Volume size (scheduler-dependent)"`

	// AccessMode is the volume access mode (scheduler-dependent)
	AccessMode string `required:"false" yaml:"access_mode,omitempty" description:"Volume access mode (scheduler-dependent)"`

	// StorageClass is the storage class name (scheduler-dependent)
	StorageClass string `required:"false" yaml:"storage_class,omitempty" description:"Storage class name (scheduler-dependent)"`

	// Namespace is the namespace (scheduler-dependent)
	Namespace string `required:"false" yaml:"namespace,omitempty" description:"Namespace (scheduler-dependent)"`

	// Chown is the chown value applied when the entry's host directory is created
	Chown string `required:"false" yaml:"chown,omitempty" description:"Chown value applied when the entry's host directory is created"`

	// ReclaimPolicy is the reclaim policy (scheduler-dependent)
	ReclaimPolicy string `required:"false" yaml:"reclaim_policy,omitempty" description:"Reclaim policy (scheduler-dependent)"`

	// State is the desired state of the storage entry
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the storage entry"`
}

// StorageEntryTaskExample contains an example of a StorageEntryTask
type StorageEntryTaskExample struct {
	// Name is the task name holding the StorageEntryTask description
	Name string `yaml:"-"`

	// StorageEntryTask is the StorageEntryTask configuration
	StorageEntryTask StorageEntryTask `yaml:"dokku_storage_entry"`
}

// GetName returns the name of the example
func (e StorageEntryTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the storage entry task
func (t StorageEntryTask) Doc() string {
	return "Creates or destroys a named storage registry entry"
}

// Examples returns the examples for the storage entry task
func (t StorageEntryTask) Examples() ([]Doc, error) {
	return MarshalExamples([]StorageEntryTaskExample{
		{
			Name: "Create a docker-local storage entry owned by the herokuish user",
			StorageEntryTask: StorageEntryTask{
				Name:  "node-js-app-data",
				Chown: "herokuish",
			},
		},
		{
			Name: "Create a storage entry at an explicit host path",
			StorageEntryTask: StorageEntryTask{
				Name: "node-js-app-data",
				Path: "/var/lib/dokku/data/storage/node-js-app-data",
			},
		},
		{
			Name: "Destroy a storage entry",
			StorageEntryTask: StorageEntryTask{
				Name:  "node-js-app-data",
				State: StateAbsent,
			},
		},
	})
}

// Execute creates or destroys the storage entry
func (t StorageEntryTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the StorageEntryTask would produce.
func (t StorageEntryTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			exists, err := storageEntryExists(t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if exists {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			args := []string{"--quiet", "storage:create"}
			if t.Scheduler != "" {
				args = append(args, "--scheduler", t.Scheduler)
			}
			if t.Size != "" {
				args = append(args, "--size", t.Size)
			}
			if t.AccessMode != "" {
				args = append(args, "--access-mode", t.AccessMode)
			}
			if t.StorageClass != "" {
				args = append(args, "--storage-class-name", t.StorageClass)
			}
			if t.Namespace != "" {
				args = append(args, "--namespace", t.Namespace)
			}
			if t.Chown != "" {
				args = append(args, "--chown", t.Chown)
			}
			if t.ReclaimPolicy != "" {
				args = append(args, "--reclaim-policy", t.ReclaimPolicy)
			}
			args = append(args, t.Name)
			if t.Path != "" {
				args = append(args, t.Path)
			}
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    "entry missing",
				Mutations: []string{fmt.Sprintf("create storage entry %s", t.Name)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			exists, err := storageEntryExists(t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !exists {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "storage:destroy", "--force", t.Name},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    "entry present",
				Mutations: []string{fmt.Sprintf("destroy storage entry %s", t.Name)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// storageEntryExists reports whether a named storage registry entry
// exists. A transport-level failure (`*subprocess.SSHError`) is
// propagated; a dokku-level non-zero exit is treated as "no entry."
func storageEntryExists(name string) (bool, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "storage:list-entries", "--format", "json"},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return false, err
		}
		return false, nil
	}

	var entries []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(result.StdoutBytes(), &entries); err != nil {
		return false, nil
	}
	for _, entry := range entries {
		if entry.Name == name {
			return true, nil
		}
	}
	return false, nil
}

// init registers the StorageEntryTask with the task registry
func init() {
	RegisterTask(&StorageEntryTask{})
}
