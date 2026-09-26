package tasks

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/dokku/docket/internal/subprocess"
)

// storageDefaultProcessType is dokku's wildcard process type (DefaultProcessType
// in the storage plugin). An attachment carrying it applies to every process, so
// export omits process_type in that case to keep the recipe minimal.
const storageDefaultProcessType = "_default_"

// StorageMountTask manages storage attachments for a dokku application.
// It has two mutually exclusive forms:
//
//   - Single mount: the top-level entry_name / host_dir / container_dir and
//     mount-time fields describe one attachment, for state present or absent.
//     A named entry maps to `storage:mount <app> <entry_name> --container-dir
//     <container_dir>` plus the matching attachment flags; host_dir passes the
//     legacy host:container[:opts] colon form. The attachment is found by
//     (source, container_path, process_type), and drift in any of its
//     mount-time attributes is remediated with an in-place upsert.
//   - Mount list: `mounts` lists the attachments of one process-type scope, for
//     any of the four states. Every state is a single whole-scope command -
//     `storage:mount --replace` or `storage:unmount --all --process-type` - and
//     every attribute is drift-detected.
//
// Export reads `storage:report <app> --format json` and emits one mount list per
// process type with state set.
type StorageMountTask struct {
	// App is the name of the app
	App string `required:"true" identity:"key" yaml:"app" description:"Name of the app"`

	// EntryName is the named storage registry entry to attach. Mutually
	// exclusive with host_dir.
	EntryName string `required:"false" yaml:"entry_name,omitempty" description:"Named storage registry entry to attach (mutually exclusive with host_dir and mounts)"`

	// HostDir is the host directory to mount (legacy bind-mount form).
	// Mutually exclusive with entry_name.
	HostDir string `required:"false" yaml:"host_dir,omitempty" description:"Host directory to mount in the legacy bind-mount form (mutually exclusive with entry_name and mounts)"`

	// ContainerDir is the container directory to mount
	ContainerDir string `required:"false" identity:"key" yaml:"container_dir,omitempty" description:"Container directory to mount; required unless mounts is used"`

	// Phases is the deployment phases the attachment applies to (deploy, run).
	// Empty defers to the dokku default (both phases).
	Phases []string `required:"false" yaml:"phases,omitempty" description:"Deployment phases the attachment applies to (deploy, run). Empty defers to the dokku default."`

	// ProcessType limits the attachment to a specific process type. For the
	// mounts list it is the scope the list describes.
	ProcessType string `required:"false" identity:"key" yaml:"process_type,omitempty" description:"Process type the attachment applies to. With mounts, the process type whose mounts the list describes; mounts of other process types are left alone. Empty means dokku's _default_ scope."`

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

	// Mounts is the list form: the attachments of one process-type scope.
	Mounts []StorageMount `required:"false" identity:"collection" yaml:"mounts,omitempty" description:"Attachments of the process_type scope, instead of the single-mount fields. Under state 'set' the list is the scope's complete set of mounts; 'present' adds or updates the listed mounts and 'absent' removes them, leaving the rest of the scope in place; omit for state 'clear'."`

	// State is the desired state of the storage
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent,set,clear" description:"Desired state of the storage. 'set' and 'clear' require the mounts list form."`
}

// StorageMount is one attachment in a StorageMountTask mounts list.
type StorageMount struct {
	// EntryName is the named storage registry entry to attach. Mutually
	// exclusive with host_dir.
	EntryName string `required:"false" yaml:"entry_name,omitempty" description:"Named storage registry entry to attach (mutually exclusive with host_dir)"`

	// HostDir is the host directory to mount (legacy bind-mount form).
	// Mutually exclusive with entry_name.
	HostDir string `required:"false" yaml:"host_dir,omitempty" description:"Host directory to mount in the legacy bind-mount form, docker-local apps only (mutually exclusive with entry_name)"`

	// ContainerDir is the container directory to mount. It identifies the
	// mount within its scope: dokku holds at most one attachment per
	// container path per process type.
	ContainerDir string `required:"true" identity:"key" yaml:"container_dir" description:"Absolute container directory to mount; no two mounts in a list may share one"`

	// Phases is the deployment phases the attachment applies to.
	Phases []string `required:"false" yaml:"phases,omitempty" description:"Deployment phases the attachment applies to (deploy, run). Empty defers to the dokku default."`

	// Subpath is the subpath within the entry to mount
	Subpath string `required:"false" yaml:"subpath,omitempty" description:"Subpath within the entry to mount; must not contain ','"`

	// Readonly mounts the attachment as read-only when true
	Readonly bool `required:"false" yaml:"readonly,omitempty" description:"Mount the attachment as read-only"`

	// VolumeChown is the chown option applied to the volume at mount time
	VolumeChown string `required:"false" yaml:"volume_chown,omitempty" description:"Chown option applied to the volume at mount time; must not contain ','"`

	// VolumeOptions is the comma-separated Docker mount options.
	VolumeOptions string `required:"false" yaml:"volume_options,omitempty" description:"Comma-separated Docker mount options (e.g. 'Z', 'noexec,nosuid'). A key=value option and ro/rw are not accepted in a list; use readonly for ro."`
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
	return "Attaches, detaches or replaces storage mounts on a dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t StorageMountTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t StorageMountTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// examples returns the examples for the storage mount task
func (t StorageMountTask) examples() ([]Doc, error) {
	return MarshalExamples([]StorageMountTaskExample{
		{
			Name: "Attach a named storage entry to an app",
			StorageMountTask: StorageMountTask{
				App:          "node-js-app",
				EntryName:    "node-js-app-data",
				ContainerDir: "/app/storage",
			},
		},
		// The web-scoped examples mount at paths no _default_ example uses:
		// dokku's _default_ scope overlaps every named process type, so it
		// refuses a web mount at a container path a _default_ mount holds.
		{
			Name: "Attach a named entry on deploy only, read-only, for the web process",
			StorageMountTask: StorageMountTask{
				App:          "node-js-app",
				EntryName:    "node-js-app-data",
				ContainerDir: "/app/assets",
				Phases:       []string{"deploy"},
				ProcessType:  "web",
				Readonly:     true,
			},
		},
		// The host-directory examples mount somewhere other than /app/storage
		// on purpose: dokku 0.38.29 refuses a second source at a container path
		// an existing attachment already holds, so a recipe that copied the
		// named-entry example above and then one of these verbatim would be
		// rejected rather than merely confusing.
		{
			Name: "Mount a host directory into an app (legacy form)",
			StorageMountTask: StorageMountTask{
				App:          "node-js-app",
				HostDir:      "/var/lib/dokku/data/storage/node-js-app",
				ContainerDir: "/app/uploads",
			},
		},
		{
			Name: "Mount a host directory with SELinux relabeling",
			StorageMountTask: StorageMountTask{
				App:           "node-js-app",
				HostDir:       "/var/lib/dokku/data/storage/node-js-app",
				ContainerDir:  "/app/shared",
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
		{
			Name: "Declare the complete set of mounts for an app, removing any other",
			StorageMountTask: StorageMountTask{
				App: "node-js-app",
				Mounts: []StorageMount{
					{EntryName: "node-js-app-data", ContainerDir: "/app/storage"},
					{HostDir: "/var/lib/dokku/data/storage/node-js-app", ContainerDir: "/app/uploads", VolumeOptions: "Z"},
				},
				State: StateSet,
			},
		},
		{
			Name: "Declare the web process's mounts, each with its own subpath",
			StorageMountTask: StorageMountTask{
				App:         "node-js-app",
				ProcessType: "web",
				Mounts: []StorageMount{
					{EntryName: "node-js-app-data", ContainerDir: "/app/assets", Subpath: "assets", Phases: []string{"deploy"}, Readonly: true},
					{EntryName: "node-js-app-data", ContainerDir: "/app/cache", Subpath: "cache"},
				},
				State: StateSet,
			},
		},
		{
			Name: "Add mounts to an app, leaving its other mounts in place",
			StorageMountTask: StorageMountTask{
				App: "node-js-app",
				Mounts: []StorageMount{
					{EntryName: "node-js-app-data", ContainerDir: "/app/tmp", VolumeOptions: "noexec,nosuid"},
				},
			},
		},
		{
			Name: "Remove mounts from an app, leaving its other mounts in place",
			StorageMountTask: StorageMountTask{
				App: "node-js-app",
				Mounts: []StorageMount{
					{EntryName: "node-js-app-data", ContainerDir: "/app/tmp"},
				},
				State: StateAbsent,
			},
		},
		{
			Name: "Remove every mount of the default process type",
			StorageMountTask: StorageMountTask{
				App:   "node-js-app",
				State: StateClear,
			},
		},
	})
}

// Execute attaches or detaches storage for a given app
func (t StorageMountTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// Validate checks the StorageMountTask's inputs without contacting the server.
func (t StorageMountTask) Validate() error {
	if err := t.validate(); err != nil {
		return err
	}
	return nil
}

// Plan reports the drift the StorageMountTask would produce.
func (t StorageMountTask) Plan(ctx context.Context) PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	if len(t.Mounts) > 0 || t.State == StateSet || t.State == StateClear {
		return DispatchPlan(t.State, map[State]func() PlanResult{
			StatePresent: func() PlanResult { return planStorageMountsPresent(ctx, t) },
			StateAbsent:  func() PlanResult { return planStorageMountsAbsent(ctx, t) },
			StateSet:     func() PlanResult { return planStorageMountsSet(ctx, t) },
			StateClear:   func() PlanResult { return planStorageMountsClear(ctx, t) },
		})
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return planStorageMountPresent(ctx, t) },
		StateAbsent:  func() PlanResult { return planStorageMountAbsent(ctx, t) },
	})
}

// planStorageMountPresent reports drift for the single-mount present state. A
// missing attachment is a create; drift in any mount-time field of an existing
// one is an in-place modify, matching the create-vs-modify split in sibling
// tasks such as service_expose.
func planStorageMountPresent(ctx context.Context, t StorageMountTask) PlanResult {
	attachments, err := readStorageAttachments(ctx, t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	existing := findSingleMount(attachments, t.EntryName, t.HostDir, t.ContainerDir, t.scope())
	if existing == nil {
		inputs := t.firstMountInputs(attachments)
		return PlanResult{
			InSync:    false,
			Status:    PlanStatusCreate,
			Reason:    "mount missing",
			Mutations: []string{fmt.Sprintf("mount %s on %s", t.describeMount(), t.App)},
			Commands:  resolveCommands(ctx, inputs),
			apply: func(ctx context.Context) TaskOutputState {
				return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
			},
		}
	}
	drift := t.singleMountDrift(*existing)
	if len(drift) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	// Re-mount via the named-entry CLI form so dokku upserts every mount-time
	// field in place. The legacy CLI form would error with "Mount path already
	// exists." (dokku/dokku#8713 kept that contract).
	inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: t.namedMountArgs(existing.EntryName)}}
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusModify,
		Reason:    strings.Join(drift, "; "),
		Mutations: []string{fmt.Sprintf("remount %s on %s", t.describeMount(), t.App)},
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StatePresent, inputs)
		},
	}
}

// planStorageMountAbsent reports drift for the single-mount absent state.
// `storage:unmount <entry> --container-dir` removes the entry at that path from
// every process type, so it is only used when the recipe's scope is the one
// holding it; otherwise the scope alone is rewritten with the whole-scope
// commands the mounts list uses.
func planStorageMountAbsent(ctx context.Context, t StorageMountTask) PlanResult {
	attachments, err := readStorageAttachments(ctx, t.App)
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	scope := t.scope()
	existing := findSingleMount(attachments, t.EntryName, t.HostDir, t.ContainerDir, scope)
	if existing == nil {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	mutations := []string{fmt.Sprintf("unmount %s on %s", t.describeMount(), t.App)}

	var remaining []reportAttachment
	shared := false
	for _, a := range attachments {
		inScope := effectiveStorageProcessType(a.ProcessType) == scope
		switch {
		case inScope && a.EntryName == existing.EntryName && a.ContainerPath == existing.ContainerPath:
		case inScope:
			remaining = append(remaining, a)
		case a.EntryName == existing.EntryName && a.ContainerPath == existing.ContainerPath:
			shared = true
		}
	}

	if !shared {
		inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: t.namedUnmountArgs(existing.EntryName)}}
		return PlanResult{
			InSync:    false,
			Status:    PlanStatusDestroy,
			Reason:    "mount present",
			Mutations: mutations,
			Commands:  resolveCommands(ctx, inputs),
			apply: func(ctx context.Context) TaskOutputState {
				return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateAbsent, inputs)
			},
		}
	}
	if len(remaining) == 0 {
		return storageMountsPlan(ctx, PlanStatusDestroy, "mount present", mutations,
			"storage:unmount", t.App, storageUnmountAllArgs(scope), StateAbsent, StatePresent)
	}
	specs := make([]string, 0, len(remaining))
	for _, a := range remaining {
		spec, err := attachmentSpec(a)
		if err != nil {
			return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("another process type also mounts %s, so only the %s scope can be rewritten, but %w", describeAttachment(*existing), scope, err)}
		}
		specs = append(specs, spec)
	}
	return storageMountsPlan(ctx, PlanStatusDestroy, "mount present", mutations,
		"storage:mount", t.App, storageReplaceArgs(specs, scope), StateAbsent, StatePresent)
}

// asMount returns the single-mount fields as a mounts-list entry, so the
// single-mount form shares the list form's field comparison.
func (t StorageMountTask) asMount() StorageMount {
	return StorageMount{
		EntryName:     t.EntryName,
		HostDir:       t.HostDir,
		ContainerDir:  t.ContainerDir,
		Phases:        t.Phases,
		Subpath:       t.Subpath,
		Readonly:      t.Readonly,
		VolumeChown:   t.VolumeChown,
		VolumeOptions: t.VolumeOptions,
	}
}

// singleMountDrift names every mount-time field of an existing attachment that
// differs from the recipe, or returns nil when none does.
func (t StorageMountTask) singleMountDrift(a reportAttachment) []string {
	if t.asMount().fieldsMatch(a) {
		return nil
	}
	var drift []string
	have, want := strings.Join(canonicalStoragePhases(a.Phases), ","), strings.Join(canonicalStoragePhases(t.Phases), ",")
	if have != want {
		drift = append(drift, fmt.Sprintf("phases drift (have %q, want %q)", have, want))
	}
	if a.Subpath != t.Subpath {
		drift = append(drift, fmt.Sprintf("subpath drift (have %q, want %q)", a.Subpath, t.Subpath))
	}
	if a.Readonly != t.Readonly {
		drift = append(drift, fmt.Sprintf("readonly drift (have %t, want %t)", a.Readonly, t.Readonly))
	}
	if a.VolumeChown != t.VolumeChown {
		drift = append(drift, fmt.Sprintf("volume_chown drift (have %q, want %q)", a.VolumeChown, t.VolumeChown))
	}
	if a.VolumeOptions != t.VolumeOptions {
		drift = append(drift, fmt.Sprintf("volume_options drift (have %q, want %q)", a.VolumeOptions, t.VolumeOptions))
	}
	return drift
}

// hasSingleMount reports whether any of the single-mount fields is set.
func (t StorageMountTask) hasSingleMount() bool {
	return t.EntryName != "" || t.HostDir != "" || t.ContainerDir != "" || len(t.Phases) > 0 ||
		t.Subpath != "" || t.Readonly || t.VolumeChown != "" || t.VolumeOptions != ""
}

// validate enforces the cross-field rules that the docs generator's
// required-field check cannot express: which form each state accepts, exactly
// one of entry_name / host_dir per mount, every phases entry is one of deploy /
// run, and - for the mounts list - that every mount can be written in the
// `storage:mount --replace` spec grammar.
func (t StorageMountTask) validate() error {
	single := t.hasSingleMount()
	switch t.State {
	case StateSet, StateClear:
		if single {
			return fmt.Errorf("the single-mount fields (entry_name, host_dir, container_dir, phases, subpath, readonly, volume_chown, volume_options) must not be set for state '%s'; use 'mounts'", t.State)
		}
		if t.State == StateSet && len(t.Mounts) == 0 {
			return fmt.Errorf("'mounts' must not be empty for state '%s'", t.State)
		}
		// storage:unmount --all takes no mounts, so a list supplied alongside
		// clear would be silently discarded rather than removed.
		if t.State == StateClear && len(t.Mounts) > 0 {
			return fmt.Errorf("'mounts' must not be set for state '%s'", t.State)
		}
	default:
		if single && len(t.Mounts) > 0 {
			return errors.New("'mounts' and the single-mount fields (entry_name, host_dir, container_dir, phases, subpath, readonly, volume_chown, volume_options) are mutually exclusive")
		}
		if !single && len(t.Mounts) == 0 {
			return errors.New("either 'mounts' or one of 'entry_name' or 'host_dir' is required")
		}
	}

	if single {
		if err := validateStorageMountSource(t.EntryName, t.HostDir); err != nil {
			return err
		}
		if t.ContainerDir == "" {
			return errors.New("'container_dir' is required")
		}
		return validateStoragePhases(t.Phases)
	}
	return validateStorageMounts(t.Mounts, t.State)
}

// validateStorageMountSource enforces exactly one of entry_name / host_dir.
func validateStorageMountSource(entryName, hostDir string) error {
	if entryName == "" && hostDir == "" {
		return errors.New("exactly one of 'entry_name' or 'host_dir' is required")
	}
	if entryName != "" && hostDir != "" {
		return errors.New("'entry_name' and 'host_dir' are mutually exclusive")
	}
	return nil
}

// validateStoragePhases rejects any phase other than deploy / run.
func validateStoragePhases(phases []string) error {
	for _, phase := range phases {
		if phase != "deploy" && phase != "run" {
			return fmt.Errorf("invalid phase %q (must be 'deploy' or 'run')", phase)
		}
	}
	return nil
}

// validateStorageMounts checks each mounts entry. Under absent only the source
// and container_dir identify the attachment to remove, so the mount-time fields
// are not checked there.
func validateStorageMounts(mounts []StorageMount, state State) error {
	seen := map[string]bool{}
	for i, m := range mounts {
		if err := m.validate(state); err != nil {
			return fmt.Errorf("mounts[%d]: %w", i, err)
		}
		if seen[m.ContainerDir] {
			return fmt.Errorf("mounts[%d]: container_dir %q is listed more than once", i, m.ContainerDir)
		}
		seen[m.ContainerDir] = true
	}
	return nil
}

// validate checks one mounts entry against the `storage:mount --replace` spec
// grammar, `<entry-or-/host>:<container>[:<tokens>]`: the spec is split on its
// first two colons and the tokens on commas, with no escaping.
func (m StorageMount) validate(state State) error {
	if err := validateStorageMountSource(m.EntryName, m.HostDir); err != nil {
		return err
	}
	if m.EntryName != "" && (strings.HasPrefix(m.EntryName, "/") || strings.Contains(m.EntryName, ":")) {
		return fmt.Errorf("entry_name %q must not start with '/' or contain ':'", m.EntryName)
	}
	if m.HostDir != "" && (!strings.HasPrefix(m.HostDir, "/") || strings.Contains(m.HostDir, ":")) {
		return fmt.Errorf("host_dir %q must start with '/' and must not contain ':'", m.HostDir)
	}
	if m.ContainerDir == "" {
		return errors.New("'container_dir' is required")
	}
	if !strings.HasPrefix(m.ContainerDir, "/") || strings.Contains(m.ContainerDir, ":") {
		return fmt.Errorf("container_dir %q must start with '/' and must not contain ':'", m.ContainerDir)
	}
	if state == StateAbsent {
		return nil
	}
	if err := validateStoragePhases(m.Phases); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, phase := range m.Phases {
		if seen[phase] {
			return fmt.Errorf("phase %q is listed more than once", phase)
		}
		seen[phase] = true
	}
	return storageMountSpecProblem(m.Subpath, m.VolumeChown, m.VolumeOptions)
}

// storageMountSpecProblem reports why a mount's subpath, chown, and options
// cannot be written as `storage:mount --replace` spec tokens, or nil when they
// can. The tokens are comma-separated with no escaping, a key=value token is a
// dokku field (an unknown key is rejected), and ro / rw are readonly.
func storageMountSpecProblem(subpath, volumeChown, volumeOptions string) error {
	if strings.Contains(subpath, ",") {
		return fmt.Errorf("subpath %q must not contain ','", subpath)
	}
	if strings.Contains(volumeChown, ",") {
		return fmt.Errorf("volume_chown %q must not contain ','", volumeChown)
	}
	if volumeOptions == "" {
		return nil
	}
	for _, token := range strings.Split(volumeOptions, ",") {
		switch {
		case token == "":
			return fmt.Errorf("volume_options %q has an empty option", volumeOptions)
		case strings.Contains(token, "="):
			return fmt.Errorf("volume_options %q has key=value option %q, which storage:mount --replace does not accept", volumeOptions, token)
		case token == "ro" || token == "rw":
			return fmt.Errorf("volume_options %q has option %q; use readonly instead", volumeOptions, token)
		}
	}
	return nil
}

// describeMount renders the short label used in Mutations entries. The
// named form quotes the entry; the legacy form keeps the original
// host:container layout so existing recipes diff cleanly.
func (t StorageMountTask) describeMount() string {
	return describeStorageMount(t.EntryName, t.HostDir, t.ContainerDir)
}

// describeStorageMount is describeMount for any source.
func describeStorageMount(entryName, hostDir, containerDir string) string {
	if entryName != "" {
		return fmt.Sprintf("%s at %s", entryName, containerDir)
	}
	return fmt.Sprintf("%s:%s", hostDir, containerDir)
}

// firstMountInputs renders the commands that create the recipe's attachment
// when none exists in its scope. attachments is the app's storage:report.
//
// When entry_name is set, the named-entry CLI form is used directly. A host_dir
// needs its dokku-generated `legacy-<hash>` entry registered before the named
// form can address it. When an attachment on the app already uses that entry,
// it is registered and the named form is used directly. Otherwise the legacy
// host:container[:opts] colon form registers it. That form ignores every
// mount-time flag other than the ro and Docker options in its spec, and always
// writes a _default_, both-phases attachment, so a recipe with other phases, a
// subpath, a chown, or a process type follows it with a named-form upsert. For a
// process type other than _default_ the colon form's attachment is unmounted
// first, since dokku refuses a named scope at a path _default_ holds; no other
// scope can hold that path once the colon form's _default_ mount succeeded.
func (t StorageMountTask) firstMountInputs(attachments []reportAttachment) []subprocess.ExecCommandInput {
	dokku := func(args []string) subprocess.ExecCommandInput {
		return subprocess.ExecCommandInput{Command: "dokku", Args: args}
	}
	if t.EntryName != "" {
		return []subprocess.ExecCommandInput{dokku(t.namedMountArgs(t.EntryName))}
	}
	legacy := legacyStorageEntryName(t.HostDir)
	for _, a := range attachments {
		if a.EntryName == legacy {
			return []subprocess.ExecCommandInput{dokku(t.namedMountArgs(legacy))}
		}
	}
	inputs := []subprocess.ExecCommandInput{dokku([]string{"--quiet", "storage:mount", t.App, t.legacySpec()})}
	if t.scope() != storageDefaultProcessType {
		inputs = append(inputs, dokku(t.namedUnmountArgs(legacy)))
	} else if isDefaultPhases(canonicalStoragePhases(t.Phases)) && t.Subpath == "" && t.VolumeChown == "" {
		return inputs
	}
	return append(inputs, dokku(t.namedMountArgs(legacy)))
}

// legacyStorageEntryName mirrors dokku's LegacyMountToEntry: the entry the colon
// form registers for a host path (or docker volume name) is `legacy-` plus the
// first ten hex digits of the SHA-1 of the path, or of `vol:<name>` for a
// volume.
func legacyStorageEntryName(hostDir string) string {
	hashInput := hostDir
	if !strings.HasPrefix(hostDir, "/") {
		hashInput = "vol:" + hostDir
	}
	sum := sha1.Sum([]byte(hashInput))
	return "legacy-" + hex.EncodeToString(sum[:])[:10]
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
// for the first-time mount when the recipe specifies host_dir. readonly becomes
// a leading ro token, which dokku's parser hoists into Attachment.Readonly, and
// volume_options follows it so the parser stores it in Attachment.VolumeOptions
// verbatim.
func (t StorageMountTask) legacySpec() string {
	var tokens []string
	if t.Readonly {
		tokens = append(tokens, "ro")
	}
	if t.VolumeOptions != "" {
		tokens = append(tokens, t.VolumeOptions)
	}
	if len(tokens) > 0 {
		return fmt.Sprintf("%s:%s:%s", t.HostDir, t.ContainerDir, strings.Join(tokens, ","))
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

// scope returns the process type the mounts list describes, with an empty
// process_type resolved to dokku's _default_ scope.
func (t StorageMountTask) scope() string {
	return effectiveStorageProcessType(t.ProcessType)
}

// effectiveStorageProcessType mirrors dokku's Attachment.EffectiveProcessType:
// a blank process type is the _default_ scope.
func effectiveStorageProcessType(processType string) string {
	if processType == "" {
		return storageDefaultProcessType
	}
	return processType
}

// scopeAttachments returns the app's attachments in the given process-type
// scope, in readStorageAttachments order.
func scopeAttachments(ctx context.Context, app, scope string) ([]reportAttachment, error) {
	all, err := readStorageAttachments(ctx, app)
	if err != nil {
		return nil, err
	}
	var out []reportAttachment
	for _, a := range all {
		if effectiveStorageProcessType(a.ProcessType) == scope {
			out = append(out, a)
		}
	}
	return out, nil
}

// isLegacyStorageEntry reports whether an attachment's entry is one dokku
// generated for a host-path mount, which docket presents as host_dir.
func isLegacyStorageEntry(entryName string) bool {
	return entryName == "" || strings.HasPrefix(entryName, "legacy-")
}

// describeAttachment labels an existing attachment the way describeMount
// labels the recipe side.
func describeAttachment(a reportAttachment) string {
	if isLegacyStorageEntry(a.EntryName) {
		return describeStorageMount("", a.HostPath, a.ContainerPath)
	}
	return describeStorageMount(a.EntryName, "", a.ContainerPath)
}

// sourceMatches reports whether an attachment mounts the entry m names. A
// host_dir only matches a dokku-generated legacy entry: `--replace` resolves a
// /host spec to its legacy-<hash> entry, so matching a named entry that happens
// to share the host path would hide an entry swap the next write performs.
func (m StorageMount) sourceMatches(a reportAttachment) bool {
	if m.EntryName != "" {
		return a.EntryName == m.EntryName
	}
	return a.HostPath == m.HostDir && isLegacyStorageEntry(a.EntryName)
}

// fieldsMatch reports whether every mount-time attribute of m matches a.
func (m StorageMount) fieldsMatch(a reportAttachment) bool {
	return slices.Equal(canonicalStoragePhases(m.Phases), canonicalStoragePhases(a.Phases)) &&
		m.Subpath == a.Subpath &&
		m.Readonly == a.Readonly &&
		m.VolumeChown == a.VolumeChown &&
		m.VolumeOptions == a.VolumeOptions
}

// describe labels the mount in Mutations entries.
func (m StorageMount) describe() string {
	return describeStorageMount(m.EntryName, m.HostDir, m.ContainerDir)
}

// spec renders the mount as a `storage:mount --replace` spec.
func (m StorageMount) spec() string {
	source := m.EntryName
	if source == "" {
		source = m.HostDir
	}
	return storageMountSpec(source, m.ContainerDir, m.Phases, m.Readonly, m.Subpath, m.VolumeChown, m.VolumeOptions)
}

// attachmentSpec renders an existing attachment as a `storage:mount --replace`
// spec, so present and absent can carry the scope's other mounts through the
// whole-scope write unchanged. It always names the attachment's entry - a legacy
// attachment by its registered legacy-<hash> entry - and reports an error when
// the attachment's fields cannot be written as spec tokens.
func attachmentSpec(a reportAttachment) (string, error) {
	if err := storageMountSpecProblem(a.Subpath, a.VolumeChown, a.VolumeOptions); err != nil {
		return "", fmt.Errorf("mount %s cannot be carried through storage:mount --replace (%v); manage it with the single-mount form (entry_name/host_dir and container_dir) instead of mounts", describeAttachment(a), err)
	}
	return storageMountSpec(a.EntryName, a.ContainerPath, a.Phases, a.Readonly, a.Subpath, a.VolumeChown, a.VolumeOptions), nil
}

// storageMountSpec renders `<source>:<container>[:<tokens>]`. The tokens are ro,
// one phase= per phase unless the phases are dokku's default pair, then
// volume-subpath=, volume-chown=, and the Docker options; the third field is
// omitted when there are none.
func storageMountSpec(source, containerDir string, phases []string, readonly bool, subpath, volumeChown, volumeOptions string) string {
	var tokens []string
	if readonly {
		tokens = append(tokens, "ro")
	}
	if canonical := canonicalStoragePhases(phases); len(canonical) != 2 {
		for _, phase := range canonical {
			tokens = append(tokens, "phase="+phase)
		}
	}
	if subpath != "" {
		tokens = append(tokens, "volume-subpath="+subpath)
	}
	if volumeChown != "" {
		tokens = append(tokens, "volume-chown="+volumeChown)
	}
	if volumeOptions != "" {
		tokens = append(tokens, volumeOptions)
	}
	spec := source + ":" + containerDir
	if len(tokens) > 0 {
		spec += ":" + strings.Join(tokens, ",")
	}
	return spec
}

// canonicalStoragePhases returns phases in dokku's deploy, run order, with an
// empty list resolved to the default of both.
func canonicalStoragePhases(phases []string) []string {
	if len(phases) == 0 {
		return []string{"deploy", "run"}
	}
	seen := map[string]bool{}
	for _, phase := range phases {
		seen[phase] = true
	}
	var out []string
	for _, phase := range []string{"deploy", "run"} {
		if seen[phase] {
			out = append(out, phase)
		}
	}
	return out
}

// storageMountChanges collects the Mutations lines of a mounts-list plan,
// keyed by container path so each group sorts deterministically regardless of
// declared or report order.
type storageMountChanges struct {
	mounts   map[string]string
	remounts map[string]string
	unmounts map[string]string
}

func newStorageMountChanges() storageMountChanges {
	return storageMountChanges{mounts: map[string]string{}, remounts: map[string]string{}, unmounts: map[string]string{}}
}

// diff records what writing m over the attachment at its path changes. A
// changed source replaces the attachment, so it reads as an unmount of the old
// one plus a mount of the new one; any other drift is a remount.
func (c storageMountChanges) diff(m StorageMount, existing *reportAttachment) {
	switch {
	case existing == nil:
		c.mounts[m.ContainerDir] = m.describe()
	case !m.sourceMatches(*existing):
		c.unmounts[m.ContainerDir] = describeAttachment(*existing)
		c.mounts[m.ContainerDir] = m.describe()
	case !m.fieldsMatch(*existing):
		c.remounts[m.ContainerDir] = m.describe()
	}
}

func (c storageMountChanges) empty() bool {
	return len(c.mounts) == 0 && len(c.remounts) == 0 && len(c.unmounts) == 0
}

// onlyMounts reports whether every change adds a mount at a free path.
func (c storageMountChanges) onlyMounts() bool {
	return len(c.remounts) == 0 && len(c.unmounts) == 0
}

// mutations renders the mounts, then the remounts, then the unmounts, each
// sorted by container path.
func (c storageMountChanges) mutations(app string) []string {
	var out []string
	for _, group := range []struct {
		verb  string
		lines map[string]string
	}{{"mount", c.mounts}, {"remount", c.remounts}, {"unmount", c.unmounts}} {
		paths := make([]string, 0, len(group.lines))
		for path := range group.lines {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		for _, path := range paths {
			out = append(out, fmt.Sprintf("%s %s on %s", group.verb, group.lines[path], app))
		}
	}
	return out
}

// storageReplaceArgs is the extra argument list for
// `storage:mount <app> <spec>... --replace --process-type <scope>`. The scope is
// always passed, _default_ included, so the command names what it replaces.
func storageReplaceArgs(specs []string, scope string) []string {
	return append(append([]string{}, specs...), "--replace", "--process-type", scope)
}

// storageUnmountAllArgs is the extra argument list for
// `storage:unmount <app> --all --process-type <scope>`. The scope must always be
// passed: without it dokku removes every mount of every process type.
func storageUnmountAllArgs(scope string) []string {
	return []string{"--all", "--process-type", scope}
}

// storageMountsPlan builds the PlanResult for one whole-scope command.
func storageMountsPlan(ctx context.Context, status PlanStatus, reason string, mutations []string, subcommand, app string, extra []string, finalState, errState State) PlanResult {
	inputs := dokkuArgsInputs(subcommand, app, extra)
	return PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    reason,
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply:     applyDokkuArgs(subcommand, app, extra, finalState, errState),
	}
}

// planStorageMountsSet reports drift for the set-state full replacement of the
// scope's mounts.
func planStorageMountsSet(ctx context.Context, t StorageMountTask) PlanResult {
	current, err := scopeAttachments(ctx, t.App, t.scope())
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	byPath := map[string]*reportAttachment{}
	for i := range current {
		byPath[current[i].ContainerPath] = &current[i]
	}
	changes := newStorageMountChanges()
	declared := map[string]bool{}
	specs := make([]string, 0, len(t.Mounts))
	for _, m := range t.Mounts {
		declared[m.ContainerDir] = true
		changes.diff(m, byPath[m.ContainerDir])
		specs = append(specs, m.spec())
	}
	for _, a := range current {
		if !declared[a.ContainerPath] {
			changes.unmounts[a.ContainerPath] = describeAttachment(a)
		}
	}
	if changes.empty() {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	status := PlanStatusModify
	if len(current) == 0 {
		status = PlanStatusCreate
	}
	mutations := changes.mutations(t.App)
	return storageMountsPlan(ctx, status, fmt.Sprintf("%d mount change(s)", len(mutations)), mutations,
		"storage:mount", t.App, storageReplaceArgs(specs, t.scope()), StateSet, StateAbsent)
}

// planStorageMountsClear reports drift for removing every mount in the scope.
func planStorageMountsClear(ctx context.Context, t StorageMountTask) PlanResult {
	current, err := scopeAttachments(ctx, t.App, t.scope())
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	if len(current) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	changes := newStorageMountChanges()
	for _, a := range current {
		changes.unmounts[a.ContainerPath] = describeAttachment(a)
	}
	return storageMountsPlan(ctx, PlanStatusDestroy, fmt.Sprintf("clear %d mount(s)", len(current)), changes.mutations(t.App),
		"storage:unmount", t.App, storageUnmountAllArgs(t.scope()), StateClear, StatePresent)
}

// planStorageMountsPresent reports drift for adding or updating the listed
// mounts while keeping the scope's other mounts. The write is a single
// `--replace` of the merged scope: the other mounts are carried through as
// specs rendered from storage:report, in report order, with the listed mounts
// replacing whatever holds their container path and new ones appended in
// declared order. storage:report omits an attachment whose entry has been
// destroyed, so such a dangling attachment - which dokku cannot mount anyway -
// is dropped by the write.
func planStorageMountsPresent(ctx context.Context, t StorageMountTask) PlanResult {
	current, err := scopeAttachments(ctx, t.App, t.scope())
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	byPath := map[string]*reportAttachment{}
	for i := range current {
		byPath[current[i].ContainerPath] = &current[i]
	}
	declared := map[string]StorageMount{}
	changes := newStorageMountChanges()
	for _, m := range t.Mounts {
		declared[m.ContainerDir] = m
		changes.diff(m, byPath[m.ContainerDir])
	}
	if changes.empty() {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}

	specs := make([]string, 0, len(current)+len(t.Mounts))
	for _, a := range current {
		if m, ok := declared[a.ContainerPath]; ok {
			specs = append(specs, m.spec())
			continue
		}
		spec, err := attachmentSpec(a)
		if err != nil {
			return PlanResult{Status: PlanStatusError, Error: err}
		}
		specs = append(specs, spec)
	}
	for _, m := range t.Mounts {
		if byPath[m.ContainerDir] == nil {
			specs = append(specs, m.spec())
		}
	}

	status := PlanStatusModify
	if changes.onlyMounts() {
		status = PlanStatusCreate
	}
	mutations := changes.mutations(t.App)
	return storageMountsPlan(ctx, status, fmt.Sprintf("%d mount change(s)", len(mutations)), mutations,
		"storage:mount", t.App, storageReplaceArgs(specs, t.scope()), StatePresent, StateAbsent)
}

// planStorageMountsAbsent reports drift for removing the listed mounts while
// keeping the scope's other mounts. A listed mount matches on container path
// and source alone. The remaining mounts are written back with a single
// `--replace`, or the scope is emptied with `unmount --all` when none remain.
func planStorageMountsAbsent(ctx context.Context, t StorageMountTask) PlanResult {
	current, err := scopeAttachments(ctx, t.App, t.scope())
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	byPath := map[string]StorageMount{}
	for _, m := range t.Mounts {
		byPath[m.ContainerDir] = m
	}
	changes := newStorageMountChanges()
	var remaining []reportAttachment
	for _, a := range current {
		if m, ok := byPath[a.ContainerPath]; ok && m.sourceMatches(a) {
			changes.unmounts[a.ContainerPath] = describeAttachment(a)
			continue
		}
		remaining = append(remaining, a)
	}
	if changes.empty() {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}
	mutations := changes.mutations(t.App)
	reason := fmt.Sprintf("%d mount(s) to remove", len(mutations))
	if len(remaining) == 0 {
		return storageMountsPlan(ctx, PlanStatusDestroy, reason, mutations,
			"storage:unmount", t.App, storageUnmountAllArgs(t.scope()), StateAbsent, StatePresent)
	}
	specs := make([]string, 0, len(remaining))
	for _, a := range remaining {
		spec, err := attachmentSpec(a)
		if err != nil {
			return PlanResult{Status: PlanStatusError, Error: err}
		}
		specs = append(specs, spec)
	}
	return storageMountsPlan(ctx, PlanStatusDestroy, reason, mutations,
		"storage:mount", t.App, storageReplaceArgs(specs, t.scope()), StateAbsent, StatePresent)
}

// ExportApp reads the app's storage attachments via storage:report and returns
// one dokku_storage_mount per process type, with state set and a mounts list
// reconstructing every mount-time attribute (phases, subpath, readonly,
// volume_options, volume_chown). Legacy bind-mounts (dokku wraps these under an
// auto-generated "legacy-" entry name) use the host_dir form; named registry
// entries use the entry_name form. Phases and process_type that match dokku's
// defaults are omitted so the exported recipe stays minimal.
//
// A process type holding an attachment the mounts list cannot express - a
// key=value or ro/rw volume option, or a comma in its subpath or chown - is
// exported as one single-mount task per attachment with state present instead,
// which reproduces the mounts without the list's authority over the scope.
func (t StorageMountTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	attachments, err := readStorageAttachments(ctx, app)
	if err != nil {
		return nil, err
	}

	byScope := map[string][]reportAttachment{}
	for _, a := range attachments {
		scope := effectiveStorageProcessType(a.ProcessType)
		byScope[scope] = append(byScope[scope], a)
	}
	scopes := make([]string, 0, len(byScope))
	for scope := range byScope {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)

	var out []interface{}
	for _, scope := range scopes {
		processType := scope
		if processType == storageDefaultProcessType {
			processType = ""
		}
		scoped := byScope[scope]
		mounts, ok := exportStorageMounts(scoped)
		if ok {
			out = append(out, StorageMountTask{App: app, ProcessType: processType, Mounts: mounts, State: StateSet})
			continue
		}
		for _, m := range mounts {
			out = append(out, StorageMountTask{
				App:           app,
				EntryName:     m.EntryName,
				HostDir:       m.HostDir,
				ContainerDir:  m.ContainerDir,
				Phases:        m.Phases,
				ProcessType:   processType,
				Subpath:       m.Subpath,
				Readonly:      m.Readonly,
				VolumeChown:   m.VolumeChown,
				VolumeOptions: m.VolumeOptions,
				State:         StatePresent,
			})
		}
	}
	return out, nil
}

// exportStorageMounts converts one scope's attachments to mounts entries and
// reports whether every one of them passes the mounts list validation.
func exportStorageMounts(attachments []reportAttachment) ([]StorageMount, bool) {
	mounts := make([]StorageMount, 0, len(attachments))
	ok := true
	for _, a := range attachments {
		m := StorageMount{
			ContainerDir:  a.ContainerPath,
			Subpath:       a.Subpath,
			Readonly:      a.Readonly,
			VolumeChown:   a.VolumeChown,
			VolumeOptions: a.VolumeOptions,
		}
		if isLegacyStorageEntry(a.EntryName) {
			m.HostDir = a.HostPath
		} else {
			m.EntryName = a.EntryName
		}
		if !isDefaultPhases(a.Phases) {
			m.Phases = a.Phases
		}
		if m.validate(StateSet) != nil {
			ok = false
		}
		mounts = append(mounts, m)
	}
	return mounts, ok
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
func readStorageAttachments(ctx context.Context, app string) ([]reportAttachment, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
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

// findSingleMount returns the attachment matching either the named-entry form
// (entry_name + container_path) or the legacy form (host_path + container_path)
// in the given process-type scope, or nil if none exists. The scope matters
// because dokku keys an attachment on (entry, container path, process type): the
// same entry at the same path for another process type is a different
// attachment. attachments comes from readStorageAttachments, which reads
// storage:report so attachments on any phase are visible - storage:list only
// reports the deploy phase, which would hide run-only mounts. The returned
// attachment's EntryName is the address docket uses for follow-up mount and
// unmount commands: user-supplied for named entries, dokku-generated
// `legacy-<hash>` for host directories.
func findSingleMount(attachments []reportAttachment, entryName, hostDir, containerDir, scope string) *reportAttachment {
	for i, a := range attachments {
		if a.ContainerPath != containerDir || effectiveStorageProcessType(a.ProcessType) != scope {
			continue
		}
		if entryName != "" && a.EntryName == entryName {
			return &attachments[i]
		}
		if hostDir != "" && a.HostPath == hostDir {
			return &attachments[i]
		}
	}
	return nil
}

// init registers the StorageMountTask with the task registry
func init() {
	RegisterTask(&StorageMountTask{})
}
