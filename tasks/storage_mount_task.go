package tasks

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dokku/docket/subprocess"
)

// StorageMountTask manages a storage attachment for a dokku application.
// It supports two forms:
//
//   - Legacy bind mount: set host_dir + container_dir to pass the colon
//     form to `storage:mount <app> <host:container>`. Round-tripped with
//     dokku versions that predate the named-entry registry.
//   - Named-entry attachment: set entry_name + container_dir and (optionally)
//     phases, process_type, subpath, readonly, volume_chown. Maps to
//     `storage:mount <app> <entry_name> --container-dir <container_dir>`
//     plus the matching attachment flags.
//
// Exactly one of entry_name / host_dir must be set. Idempotency is keyed
// on (source, container_path) using `storage:list <app> --format json`;
// the richer attachment attributes are applied at mount time only and
// are not drift-detected (mirroring the partial-probe pattern in
// service_backup and storage_ensure).
type StorageMountTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app" description:"Name of the app"`

	// EntryName is the named storage registry entry to attach. Mutually
	// exclusive with host_dir.
	EntryName string `required:"false" yaml:"entry_name,omitempty" description:"Named storage registry entry to attach (mutually exclusive with host_dir)"`

	// HostDir is the host directory to mount (legacy bind-mount form).
	// Mutually exclusive with entry_name.
	HostDir string `required:"false" yaml:"host_dir,omitempty" description:"Host directory to mount in the legacy bind-mount form (mutually exclusive with entry_name)"`

	// ContainerDir is the container directory to mount
	ContainerDir string `required:"true" yaml:"container_dir" description:"Container directory to mount"`

	// Phases is the deployment phases the attachment applies to (deploy, run).
	// Empty defers to the dokku default (both phases).
	Phases []string `required:"false" yaml:"phases,omitempty" description:"Deployment phases the attachment applies to (deploy, run). Empty defers to the dokku default."`

	// ProcessType limits the attachment to a specific process type
	ProcessType string `required:"false" yaml:"process_type,omitempty" description:"Process type the attachment applies to"`

	// Subpath is the subpath within the entry to mount
	Subpath string `required:"false" yaml:"subpath,omitempty" description:"Subpath within the entry to mount"`

	// Readonly mounts the attachment as read-only when true
	Readonly bool `required:"false" yaml:"readonly,omitempty" description:"Mount the attachment as read-only"`

	// VolumeChown is the chown option applied to the volume at mount time
	VolumeChown string `required:"false" yaml:"volume_chown,omitempty" description:"Chown option applied to the volume at mount time"`

	// State is the desired state of the storage
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the storage"`
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
	return "Attaches or detaches storage on a dokku application"
}

// Examples returns the examples for the storage mount task
func (t StorageMountTask) Examples() ([]Doc, error) {
	return MarshalExamples([]StorageMountTaskExample{
		{
			Name: "Attach a named storage entry to an app",
			StorageMountTask: StorageMountTask{
				App:          "node-js-app",
				EntryName:    "node-js-app-data",
				ContainerDir: "/app/storage",
			},
		},
		{
			Name: "Attach a named entry on deploy only, read-only, for the web process",
			StorageMountTask: StorageMountTask{
				App:          "node-js-app",
				EntryName:    "node-js-app-data",
				ContainerDir: "/app/storage",
				Phases:       []string{"deploy"},
				ProcessType:  "web",
				Readonly:     true,
			},
		},
		{
			Name: "Mount a host directory into an app (legacy form)",
			StorageMountTask: StorageMountTask{
				App:          "node-js-app",
				HostDir:      "/var/lib/dokku/data/storage/node-js-app",
				ContainerDir: "/app/storage",
			},
		},
		{
			Name: "Unmount a named entry from an app",
			StorageMountTask: StorageMountTask{
				App:          "node-js-app",
				EntryName:    "node-js-app-data",
				ContainerDir: "/app/storage",
				State:        StateAbsent,
			},
		},
	})
}

// Execute attaches or detaches storage for a given app
func (t StorageMountTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the StorageMountTask would produce.
func (t StorageMountTask) Plan() PlanResult {
	if err := t.validate(); err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			exists, err := mountExists(t.App, t.EntryName, t.HostDir, t.ContainerDir)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if exists {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    t.mountArgs(),
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    "mount missing",
				Mutations: []string{fmt.Sprintf("mount %s on %s", t.describeMount(), t.App)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			exists, err := mountExists(t.App, t.EntryName, t.HostDir, t.ContainerDir)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !exists {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    t.unmountArgs(),
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    "mount present",
				Mutations: []string{fmt.Sprintf("unmount %s on %s", t.describeMount(), t.App)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// validate enforces the cross-field rules that the docs generator's
// required-field check cannot express: exactly one of entry_name /
// host_dir is set, and every phases entry is one of deploy / run.
func (t StorageMountTask) validate() error {
	if t.EntryName == "" && t.HostDir == "" {
		return errors.New("exactly one of 'entry_name' or 'host_dir' is required")
	}
	if t.EntryName != "" && t.HostDir != "" {
		return errors.New("'entry_name' and 'host_dir' are mutually exclusive")
	}
	for _, phase := range t.Phases {
		if phase != "deploy" && phase != "run" {
			return fmt.Errorf("invalid phase %q (must be 'deploy' or 'run')", phase)
		}
	}
	return nil
}

// describeMount renders the short label used in Mutations entries. The
// named form quotes the entry; the legacy form keeps the original
// host:container layout so existing recipes diff cleanly.
func (t StorageMountTask) describeMount() string {
	if t.EntryName != "" {
		return fmt.Sprintf("%s at %s", t.EntryName, t.ContainerDir)
	}
	return fmt.Sprintf("%s:%s", t.HostDir, t.ContainerDir)
}

// mountArgs renders the dokku storage:mount invocation. The named form
// passes the entry name positionally and --container-dir; the legacy
// form keeps the host:container colon syntax.
func (t StorageMountTask) mountArgs() []string {
	args := []string{"--quiet", "storage:mount"}
	if t.EntryName != "" {
		args = append(args, t.App, t.EntryName, "--container-dir", t.ContainerDir)
	} else {
		args = append(args, t.App, fmt.Sprintf("%s:%s", t.HostDir, t.ContainerDir))
	}
	return append(args, t.attachmentFlags()...)
}

// unmountArgs mirrors mountArgs for the storage:unmount path.
func (t StorageMountTask) unmountArgs() []string {
	args := []string{"--quiet", "storage:unmount"}
	if t.EntryName != "" {
		args = append(args, t.App, t.EntryName, "--container-dir", t.ContainerDir)
	} else {
		args = append(args, t.App, fmt.Sprintf("%s:%s", t.HostDir, t.ContainerDir))
	}
	return args
}

// attachmentFlags renders the optional --phase / --process-type /
// --volume-subpath / --volume-readonly / --volume-chown flags. Empty
// fields are omitted so the resulting command line stays minimal.
func (t StorageMountTask) attachmentFlags() []string {
	var flags []string
	for _, phase := range t.Phases {
		flags = append(flags, "--phase", phase)
	}
	if t.ProcessType != "" {
		flags = append(flags, "--process-type", t.ProcessType)
	}
	if t.Subpath != "" {
		flags = append(flags, "--volume-subpath", t.Subpath)
	}
	if t.Readonly {
		flags = append(flags, "--volume-readonly")
	}
	if t.VolumeChown != "" {
		flags = append(flags, "--volume-chown", t.VolumeChown)
	}
	return flags
}

// mountExists reports whether an attachment matching either the
// named-entry form (entry_name + container_path) or the legacy form
// (host_path + container_path) is present on the app. A transport-level
// failure (`*subprocess.SSHError`) is propagated; a dokku-level non-
// zero exit (e.g. app does not exist) is treated as "no mount." The
// richer attachment attributes (phases, subpath, readonly, etc.) are
// not exposed by storage:list, so a change to those values does not
// surface as drift.
func mountExists(app, entryName, hostDir, containerDir string) (bool, error) {
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
		EntryName     string `json:"entry_name"`
		HostPath      string `json:"host_path"`
		ContainerPath string `json:"container_path"`
	}

	if err := json.Unmarshal(result.StdoutBytes(), &mounts); err != nil {
		return false, nil
	}

	for _, mount := range mounts {
		if mount.ContainerPath != containerDir {
			continue
		}
		if entryName != "" && mount.EntryName == entryName {
			return true, nil
		}
		if hostDir != "" && mount.HostPath == hostDir {
			return true, nil
		}
	}
	return false, nil
}

// init registers the StorageMountTask with the task registry
func init() {
	RegisterTask(&StorageMountTask{})
}
