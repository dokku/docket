package tasks

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dokku/docket/subprocess"
)

// StorageMountTask manages the storage for a given dokku application
type StorageMountTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app"`

	// HostDir is the host directory to mount
	HostDir string `required:"true" yaml:"host_dir"`

	// ContainerDir is the container directory to mount
	ContainerDir string `required:"true" yaml:"container_dir"`

	// State is the desired state of the storage
	State State `required:"false" yaml:"state" default:"present" options:"present,absent"`
}

// StorageMountTaskExample contains an example of a StorageMountTask
type StorageMountTaskExample struct {
	// Name is the task name holding the StorageMountTask description
	Name string `yaml:"-"`

	// StorageMountTask is the StorageMountTask configuration
	StorageMountTask StorageMountTask `yaml:"dokku_storage_mount"`
}

// GetName returns the name of the example
func (e StorageMountTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the storage mount task
func (t StorageMountTask) Doc() string {
	return "Mounts or unmounts the storage for a given dokku application"
}

// Examples returns the examples for the storage mount task
func (t StorageMountTask) Examples() ([]Doc, error) {
	return MarshalExamples([]StorageMountTaskExample{})
}

// Execute mounts or unmounts the storage for a given app
func (t StorageMountTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the StorageMountTask would produce.
func (t StorageMountTask) Plan() PlanResult {
	mountSpec := fmt.Sprintf("%s:%s", t.HostDir, t.ContainerDir)
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			exists, err := mountExists(t.App, t.HostDir, t.ContainerDir)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if exists {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "storage:mount", t.App, mountSpec},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    "mount missing",
				Mutations: []string{fmt.Sprintf("mount %s on %s", mountSpec, t.App)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			exists, err := mountExists(t.App, t.HostDir, t.ContainerDir)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !exists {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "storage:unmount", t.App, mountSpec},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    "mount present",
				Mutations: []string{fmt.Sprintf("unmount %s on %s", mountSpec, t.App)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// mountExists checks if the storage mount exists. A transport-level
// failure (`*subprocess.SSHError`) is propagated; a dokku-level non-
// zero exit (e.g. app does not exist) is treated as "no mount."
func mountExists(app, hostDir, containerDir string) (bool, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"--quiet",
			"storage:list",
			app,
			"--format",
			"json",
		},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return false, err
		}
		return false, nil
	}

	var mounts []struct {
		HostPath      string `json:"host_path"`
		ContainerPath string `json:"container_path"`
	}

	if err := json.Unmarshal(result.StdoutBytes(), &mounts); err != nil {
		return false, nil
	}

	for _, mount := range mounts {
		if mount.HostPath == hostDir && mount.ContainerPath == containerDir {
			return true, nil
		}
	}
	return false, nil
}

// init registers the StorageMountTask with the task registry
func init() {
	RegisterTask(&StorageMountTask{})
}
