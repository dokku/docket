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
//     form to `storage:mount <app> <host:container[:opts]>`. Round-tripped
//     with dokku versions that predate the named-entry registry.
//   - Named-entry attachment: set entry_name + container_dir and (optionally)
//     phases, process_type, subpath, readonly, volume_chown, volume_options.
//     Maps to `storage:mount <app> <entry_name> --container-dir <container_dir>`
//     plus the matching attachment flags.
//
// Exactly one of entry_name / host_dir must be set. Idempotency is keyed
// on (source, container_path, volume_options) using `storage:list <app>
// --format json`; the other attachment attributes (phases, process_type,
// subpath, readonly, volume_chown) are applied at mount time only and
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

	// VolumeOptions is the comma-separated mount options applied to the
	// attachment (e.g. "Z" for SELinux relabeling, "noexec,nosuid", NFS
	// opts). On the legacy form it is appended as the third colon segment;
	// on the named-entry form it is passed via --volume-options.
	VolumeOptions string `required:"false" yaml:"volume_options,omitempty" description:"Comma-separated mount options applied to the attachment (e.g. 'Z' for SELinux, 'noexec,nosuid', NFS opts)"`

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
			Name: "Mount a host directory with SELinux relabeling",
			StorageMountTask: StorageMountTask{
				App:           "node-js-app",
				HostDir:       "/var/lib/dokku/data/storage/node-js-app",
				ContainerDir:  "/app/storage",
				VolumeOptions: "Z",
			},
		},
		{
			Name: "Attach a named entry with mount options",
			StorageMountTask: StorageMountTask{
				App:           "node-js-app",
				EntryName:     "node-js-app-data",
				ContainerDir:  "/app/storage",
				VolumeOptions: "noexec,nosuid",
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
			existing, err := findMount(t.App, t.EntryName, t.HostDir, t.ContainerDir)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if existing != nil && existing.VolumeOptions == t.VolumeOptions {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    t.mountArgs(),
			}}
			reason := "mount missing"
			if existing != nil {
				reason = fmt.Sprintf("volume_options drift (have %q, want %q)", existing.VolumeOptions, t.VolumeOptions)
			}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    reason,
				Mutations: []string{fmt.Sprintf("mount %s on %s", t.describeMount(), t.App)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			existing, err := findMount(t.App, t.EntryName, t.HostDir, t.ContainerDir)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if existing == nil {
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
// form keeps the host:container[:opts] colon syntax.
func (t StorageMountTask) mountArgs() []string {
	args := []string{"--quiet", "storage:mount"}
	if t.EntryName != "" {
		args = append(args, t.App, t.EntryName, "--container-dir", t.ContainerDir)
	} else {
		args = append(args, t.App, t.legacySpec())
	}
	return append(args, t.attachmentFlags()...)
}

// unmountArgs mirrors mountArgs for the storage:unmount path.
func (t StorageMountTask) unmountArgs() []string {
	args := []string{"--quiet", "storage:unmount"}
	if t.EntryName != "" {
		args = append(args, t.App, t.EntryName, "--container-dir", t.ContainerDir)
	} else {
		args = append(args, t.App, t.legacySpec())
	}
	return args
}

// legacySpec renders the legacy host:container[:opts] colon syntax used
// by the legacy form. volume_options is appended as the third segment so
// dokku's parser stores it in Attachment.VolumeOptions verbatim.
func (t StorageMountTask) legacySpec() string {
	if t.VolumeOptions != "" {
		return fmt.Sprintf("%s:%s:%s", t.HostDir, t.ContainerDir, t.VolumeOptions)
	}
	return fmt.Sprintf("%s:%s", t.HostDir, t.ContainerDir)
}

// attachmentFlags renders the optional --phase / --process-type /
// --volume-subpath / --volume-readonly / --volume-chown / --volume-options
// flags. Empty fields are omitted so the resulting command line stays
// minimal. --volume-options is only emitted for the named-entry form;
// the legacy form already carries the value inside the host:container:opts
// spec returned by legacySpec.
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
	if t.VolumeOptions != "" && t.EntryName != "" {
		flags = append(flags, "--volume-options", t.VolumeOptions)
	}
	return flags
}

// existingMount captures the subset of an existing attachment that
// docket compares against the desired state. volume_options is returned
// alongside the source/container identity so the caller can detect
// option drift on a state: present plan. The other mount-time attributes
// (phases, subpath, readonly, volume_chown) are not exposed by
// storage:list and so are not drift-detected (mirroring the partial-probe
// pattern in service_backup and storage_ensure).
type existingMount struct {
	VolumeOptions string
}

// findMount returns the existing attachment matching either the named-entry
// form (entry_name + container_path) or the legacy form (host_path +
// container_path), or nil if none exists. A transport-level failure
// (`*subprocess.SSHError`) is propagated; a dokku-level non-zero exit
// (e.g. app does not exist) is treated as "no mount."
func findMount(app, entryName, hostDir, containerDir string) (*existingMount, error) {
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
			return nil, err
		}
		return nil, nil
	}

	var mounts []struct {
		EntryName     string `json:"entry_name"`
		HostPath      string `json:"host_path"`
		ContainerPath string `json:"container_path"`
		VolumeOptions string `json:"volume_options"`
	}

	if err := json.Unmarshal(result.StdoutBytes(), &mounts); err != nil {
		return nil, nil
	}

	for _, mount := range mounts {
		if mount.ContainerPath != containerDir {
			continue
		}
		if entryName != "" && mount.EntryName == entryName {
			return &existingMount{VolumeOptions: mount.VolumeOptions}, nil
		}
		if hostDir != "" && mount.HostPath == hostDir {
			return &existingMount{VolumeOptions: mount.VolumeOptions}, nil
		}
	}
	return nil, nil
}

// init registers the StorageMountTask with the task registry
func init() {
	RegisterTask(&StorageMountTask{})
}
