package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// storageDefaultProcessType is dokku's wildcard process type (DefaultProcessType
// in the storage plugin). An attachment carrying it applies to every process, so
// export omits process_type in that case to keep the recipe minimal.
const storageDefaultProcessType = "_default_"

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
// on (source, container_path, volume_options) using `storage:report <app>
// --format json`; the other attachment attributes (phases, process_type,
// subpath, readonly, volume_chown) are applied at mount time only and
// are not drift-detected (mirroring the partial-probe pattern in
// service_backup and storage_ensure). Export reads the same report and
// reconstructs every attribute in full.
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

// ExportSupport reports how docket export handles this task.
func (t StorageMountTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
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

// Validate checks the StorageMountTask's inputs without contacting the server.
func (t StorageMountTask) Validate() error {
	if err := t.validate(); err != nil {
		return err
	}
	return nil
}

// Plan reports the drift the StorageMountTask would produce.
func (t StorageMountTask) Plan() PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
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
			var args []string
			reason := "mount missing"
			// A brand-new attachment is a create; drift on an existing one
			// is an in-place modify, matching the create-vs-modify split in
			// sibling tasks such as service_expose.
			status := PlanStatusCreate
			if existing == nil {
				args = t.mountArgs()
			} else {
				// Drift on an existing attachment: re-mount via the
				// named-entry CLI form so dokku upserts in place. The
				// legacy CLI form would error with "Mount path already
				// exists." (dokku/dokku#8713 kept that contract).
				args = t.namedMountArgs(existing.EntryName)
				reason = fmt.Sprintf("volume_options drift (have %q, want %q)", existing.VolumeOptions, t.VolumeOptions)
				status = PlanStatusModify
			}
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
			return PlanResult{
				InSync:    false,
				Status:    status,
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
				Args:    t.namedUnmountArgs(existing.EntryName),
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

// mountArgs renders the storage:mount invocation for the first-time
// mount of a recipe. When entry_name is set, the named-entry CLI form
// is used directly. When host_dir is set and no attachment yet exists,
// the legacy host:container[:opts] colon form is used so dokku
// auto-generates a `legacy-<id>` entry; later operations against that
// attachment go through namedMountArgs/namedUnmountArgs via the entry
// name discovered from storage:report.
func (t StorageMountTask) mountArgs() []string {
	if t.EntryName != "" {
		return t.namedMountArgs(t.EntryName)
	}
	args := []string{"--quiet", "storage:mount", t.App, t.legacySpec()}
	return append(args, t.attachmentFlags()...)
}

// namedMountArgs renders storage:mount in the named-entry CLI form for
// the given entry. Used both when the recipe specifies entry_name and
// when docket is remediating drift on a legacy-CLI mount whose auto-
// generated entry name was discovered from storage:report. Dokku 0.38.13's
// upsert semantic (dokku/dokku#8713) makes the call idempotent.
func (t StorageMountTask) namedMountArgs(entryName string) []string {
	args := []string{"--quiet", "storage:mount", t.App, entryName, "--container-dir", t.ContainerDir}
	args = append(args, t.attachmentFlags()...)
	if t.VolumeOptions != "" {
		args = append(args, "--volume-options", t.VolumeOptions)
	}
	return args
}

// namedUnmountArgs mirrors namedMountArgs for the storage:unmount path.
func (t StorageMountTask) namedUnmountArgs(entryName string) []string {
	return []string{"--quiet", "storage:unmount", t.App, entryName, "--container-dir", t.ContainerDir}
}

// legacySpec renders the legacy host:container[:opts] colon syntax used
// for the first-time mount when the recipe specifies host_dir. volume_options
// is appended as the third segment so dokku's parser stores it in
// Attachment.VolumeOptions verbatim.
func (t StorageMountTask) legacySpec() string {
	if t.VolumeOptions != "" {
		return fmt.Sprintf("%s:%s:%s", t.HostDir, t.ContainerDir, t.VolumeOptions)
	}
	return fmt.Sprintf("%s:%s", t.HostDir, t.ContainerDir)
}

// attachmentFlags renders the optional --phase / --process-type /
// --volume-subpath / --volume-readonly / --volume-chown flags. Empty
// fields are omitted so the resulting command line stays minimal.
// --volume-options is NOT emitted here: namedMountArgs appends it
// directly, and the legacy first-time-mount path carries it inside the
// host:container:opts spec returned by legacySpec.
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

// existingMount captures the subset of an existing attachment that
// docket compares against the desired state. EntryName is the attachment's
// dokku-side identifier (user-supplied for named-entry recipes; auto-
// generated as `legacy-<id>` for attachments created via the legacy CLI
// form), and is the address docket uses for follow-up mount/unmount
// commands. VolumeOptions enables option-drift detection on
// state: present. The other mount-time attributes (phases, process_type,
// subpath, readonly, volume_chown) are read by the exporter but
// intentionally not drift-detected here.
type existingMount struct {
	EntryName     string
	VolumeOptions string
}

// ExportApp reads the app's storage attachments via storage:report and returns
// a dokku_storage_mount task per attachment, reconstructing every mount-time
// attribute (phases, process_type, subpath, readonly, volume_options,
// volume_chown). Legacy bind-mounts (dokku wraps these under an auto-generated
// "legacy-" entry name) use the host_dir form; named registry entries use the
// entry_name form. Phases and process_type that match dokku's defaults are
// omitted so the exported recipe stays minimal.
func (t StorageMountTask) ExportApp(app string) ([]interface{}, error) {
	attachments, err := readStorageAttachments(app)
	if err != nil {
		return nil, err
	}

	var out []interface{}
	for _, a := range attachments {
		task := StorageMountTask{
			App:           app,
			ContainerDir:  a.ContainerPath,
			Subpath:       a.Subpath,
			Readonly:      a.Readonly,
			VolumeChown:   a.VolumeChown,
			VolumeOptions: a.VolumeOptions,
		}
		if a.EntryName == "" || strings.HasPrefix(a.EntryName, "legacy-") {
			task.HostDir = a.HostPath
		} else {
			task.EntryName = a.EntryName
		}
		if !isDefaultPhases(a.Phases) {
			task.Phases = a.Phases
		}
		if a.ProcessType != "" && a.ProcessType != storageDefaultProcessType {
			task.ProcessType = a.ProcessType
		}
		out = append(out, task)
	}
	return out, nil
}

// reportAttachment is one storage attachment reconstructed from
// `storage:report <app> --format json`. Unlike the deploy-phase-filtered
// storage:list view, it carries every mount-time attribute the dokku
// Attachment model exposes.
type reportAttachment struct {
	EntryName     string
	HostPath      string
	ContainerPath string
	Phases        []string
	ProcessType   string
	Subpath       string
	VolumeOptions string
	VolumeChown   string
	Readonly      bool
}

// readStorageAttachments reads every storage attachment on an app via
// `storage:report <app> --format json`. That report (unlike storage:list)
// exposes all attachment attributes and is not filtered to the deploy phase.
// dokku emits each attachment as a set of flat indexed keys
// (attachment.<N>.<field>); this regroups them by index and returns the
// attachments sorted by (container_path, process_type, entry_name) for stable
// output.
//
// A transport-level failure (*subprocess.SSHError) is propagated; a dokku-level
// non-zero exit (e.g. app does not exist) or a JSON parse failure is treated as
// "no attachments."
func readStorageAttachments(app string) ([]reportAttachment, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "storage:report", app, "--format", "json"},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return nil, nil
	}

	payload := map[string]string{}
	if err := json.Unmarshal(result.StdoutBytes(), &payload); err != nil {
		return nil, nil
	}

	// Enumerate attachment indices via the always-present container-path key.
	// Keys look like "attachment.1.container-path"; the legacy-prefixed
	// duplicates ("storage-attachment.1.container-path") are skipped since they
	// do not start with the bare "attachment." prefix.
	var indices []int
	for key := range payload {
		rest := strings.TrimPrefix(key, "attachment.")
		if rest == key {
			continue
		}
		dot := strings.IndexByte(rest, '.')
		if dot < 0 || rest[dot+1:] != "container-path" {
			continue
		}
		n, err := strconv.Atoi(rest[:dot])
		if err != nil {
			continue
		}
		indices = append(indices, n)
	}
	sort.Ints(indices)

	field := func(n int, name string) string {
		return payload[fmt.Sprintf("attachment.%d.%s", n, name)]
	}

	attachments := make([]reportAttachment, 0, len(indices))
	for _, n := range indices {
		a := reportAttachment{
			EntryName:     field(n, "entry-name"),
			HostPath:      field(n, "host-path"),
			ContainerPath: field(n, "container-path"),
			ProcessType:   field(n, "process-type"),
			Subpath:       field(n, "subpath"),
			VolumeOptions: field(n, "volume-options"),
			VolumeChown:   field(n, "volume-chown"),
			Readonly:      field(n, "readonly") == "true",
		}
		if phases := field(n, "phases"); phases != "" {
			a.Phases = strings.Split(phases, ",")
		}
		attachments = append(attachments, a)
	}

	sort.Slice(attachments, func(i, j int) bool {
		a, b := attachments[i], attachments[j]
		if a.ContainerPath != b.ContainerPath {
			return a.ContainerPath < b.ContainerPath
		}
		if a.ProcessType != b.ProcessType {
			return a.ProcessType < b.ProcessType
		}
		return a.EntryName < b.EntryName
	})
	return attachments, nil
}

// isDefaultPhases reports whether phases is exactly dokku's default set
// {deploy, run} (in any order). Export omits the phases field in that case so
// the recipe defers to the default, matching the "empty means both phases"
// contract on StorageMountTask.Phases.
func isDefaultPhases(phases []string) bool {
	if len(phases) != 2 {
		return false
	}
	seen := map[string]bool{}
	for _, phase := range phases {
		seen[phase] = true
	}
	return seen["deploy"] && seen["run"]
}

// findMount returns the existing attachment matching either the named-entry
// form (entry_name + container_path) or the legacy form (host_path +
// container_path), or nil if none exists. It reads storage:report (via
// readStorageAttachments) so attachments on any phase are visible - storage:list
// only reports the deploy phase, which would hide run-only mounts. A
// transport-level failure (`*subprocess.SSHError`) is propagated; a dokku-level
// non-zero exit (e.g. app does not exist) is treated as "no mount."
func findMount(app, entryName, hostDir, containerDir string) (*existingMount, error) {
	attachments, err := readStorageAttachments(app)
	if err != nil {
		return nil, err
	}

	for _, a := range attachments {
		if a.ContainerPath != containerDir {
			continue
		}
		if entryName != "" && a.EntryName == entryName {
			return &existingMount{EntryName: a.EntryName, VolumeOptions: a.VolumeOptions}, nil
		}
		if hostDir != "" && a.HostPath == hostDir {
			return &existingMount{EntryName: a.EntryName, VolumeOptions: a.VolumeOptions}, nil
		}
	}
	return nil, nil
}

// init registers the StorageMountTask with the task registry
func init() {
	RegisterTask(&StorageMountTask{})
}
