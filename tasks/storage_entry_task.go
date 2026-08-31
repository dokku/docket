package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// StorageEntryTask manages a named storage registry entry — the thing
// `storage:create` produces and `storage:list-entries` reports. Entries
// exist independently of any single app, and an attachment created via
// dokku_storage_mount references them by name.
//
// The entry name selects the entry; every other attribute is compared
// against what `storage:list-entries` records for it, so a recipe that
// changes one converges on the next apply rather than being applied once at
// create time. A field the recipe omits is unmanaged: it is neither
// compared nor cleared, which is what stops a recipe naming only `name` and
// `chown` from dropping a `size` it never mentions. Clearing an attribute is
// a manual `dokku storage:set <name> <property>` with no value.
//
// `destroy_host_dir` is the one field that is not an attribute. It modifies
// the `storage:destroy` the `absent` state issues, so there is nothing on
// the entry to compare it against and nothing for an export to reconstruct.
//
// Four fields cannot be converged, because dokku cannot change them on an
// entry that exists: `storage:set` refuses an access-mode or storage-class
// swap outright, and there is no command at all for the scheduler or the
// host path. A recipe that disagrees with the entry on one of them is
// reported as a plan error naming both values, so it fails before an apply
// starts rather than part way through one.
type StorageEntryTask struct {
	// Name is the name of the storage entry
	Name string `required:"true" identity:"key" yaml:"name" description:"Name of the storage entry"`

	// Path is the host path for the entry. Optional; on docker-local it
	// defaults to the dokku storage root + name.
	Path string `required:"false" yaml:"path,omitempty" description:"Host path for the entry: an absolute path, or a docker named volume on docker-local. Defaults to the dokku storage root joined with the entry name. Cannot be changed on an entry that exists; a recipe that disagrees with the recorded path is reported as an error"`

	// Scheduler is the scheduler that backs the entry
	Scheduler string `required:"false" yaml:"scheduler,omitempty" default:"docker-local" options:"docker-local,k3s" description:"Scheduler that backs the entry. Cannot be changed on an entry that exists; a recipe that disagrees with the recorded scheduler is reported as an error"`

	// Size is the volume size (k3s scheduler; required there, rejected on docker-local)
	Size string `required:"false" yaml:"size,omitempty" description:"Volume size (k3s scheduler; required there and rejected on docker-local). Converged on an entry that exists"`

	// AccessMode is the volume access mode (k3s scheduler)
	AccessMode string `required:"false" yaml:"access_mode,omitempty" options:"ReadWriteOnce,ReadOnlyMany,ReadWriteMany,ReadWriteOncePod" description:"Volume access mode (k3s scheduler; rejected on docker-local). Cannot be changed on an entry that exists, since kubernetes cannot rebind a bound claim; a recipe that disagrees with the recorded value is reported as an error"`

	// StorageClass is the storage class name (k3s scheduler)
	StorageClass string `required:"false" yaml:"storage_class,omitempty" description:"Storage class name (k3s scheduler; rejected on docker-local, and mutually exclusive with path). Cannot be changed on an entry that exists; a recipe that disagrees with the recorded value is reported as an error"`

	// Namespace is the namespace (scheduler-dependent)
	Namespace string `required:"false" yaml:"namespace,omitempty" description:"Namespace (scheduler-dependent). Converged on an entry that exists"`

	// Chown is the ownership applied to the entry's host directory, on
	// creation and again whenever the recipe changes it.
	Chown string `required:"false" yaml:"chown,omitempty" options:"heroku,herokuish,paketo,root,false" description:"Ownership applied to the entry's host directory: an ownership preset or a numeric uid (0-65535). dokku sets the owner and the group to the same id, and refuses the value unless the entry sits at its default host path. Converged on an entry that exists, which re-runs the chown on a docker-local directory"`

	// Mode is the octal permission set applied to the entry's host
	// directory, on creation and again whenever the recipe changes it.
	Mode string `required:"false" yaml:"mode,omitempty" description:"Octal permissions applied to the entry's host directory: a 3 or 4 digit mode, recorded in its 4 digit form so 755 and 0755 are the same value (docker-local scheduler; rejected on k3s). dokku refuses the value unless the entry sits at its default host path, and applies it non-recursively. Converged on an entry that exists, which re-runs the chmod on a docker-local directory. Omit it to leave the directory at the 0755 dokku creates it with"`

	// ReclaimPolicy is the reclaim policy (k3s scheduler)
	ReclaimPolicy string `required:"false" yaml:"reclaim_policy,omitempty" options:"Retain,Delete" description:"Reclaim policy applied to the underlying volume (k3s scheduler). Converged on an entry that exists"`

	// Annotations are the volume annotations (k3s scheduler)
	Annotations map[string]string `required:"false" yaml:"annotations,omitempty" description:"Map of annotations set on the underlying volume (k3s scheduler). Converged one key at a time on an entry that exists, so a key the recipe omits is left alone"`

	// Labels are the volume labels (k3s scheduler)
	Labels map[string]string `required:"false" yaml:"labels,omitempty" description:"Map of labels set on the underlying volume (k3s scheduler). Converged one key at a time on an entry that exists, so a key the recipe omits is left alone"`

	// DestroyHostDir removes the host directory alongside the entry, and
	// is only meaningful under state 'absent'.
	DestroyHostDir bool `required:"false" yaml:"destroy_host_dir,omitempty" description:"Remove the entry's host directory and its contents alongside deregistering the entry (docker-local scheduler). Only valid with state 'absent', and only where the entry sits at its default host path. An entry recording reclaim_policy 'Delete' loses its host directory whether or not this is set"`

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

// ExportSupport reports how docket export handles this task.
func (t StorageEntryTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t StorageEntryTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported, Caveat: "every attribute is compared against what storage:list-entries records for the entry, which is the recorded chown and mode rather than the host directory's ownership and permissions on disk; a directory chowned or chmodded out of band is not detected"}
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
			Name: "Change an entry's ownership, leaving the attributes the recipe does not name alone",
			StorageEntryTask: StorageEntryTask{
				Name:  "node-js-app-data",
				Chown: "root",
			},
		},
		{
			Name: "Set the permissions on an entry's host directory",
			StorageEntryTask: StorageEntryTask{
				Name: "node-js-app-data",
				Mode: "0777",
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
		{
			Name: "Destroy a storage entry and remove its host directory",
			StorageEntryTask: StorageEntryTask{
				Name:           "node-js-app-data",
				DestroyHostDir: true,
				State:          StateAbsent,
			},
		},
	})
}

// Execute creates or destroys the storage entry
func (t StorageEntryTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// createArgs builds the storage:create command arguments. Every flag is
// omitted when its field is empty so dokku applies its own default, and
// the order is fixed - including the sorted map keys - so plan and apply
// build byte-identical argv across runs.
func (t StorageEntryTask) createArgs() []string {
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
	if t.Mode != "" {
		args = append(args, "--mode", normalizeDirectoryMode(t.Mode))
	}
	if t.ReclaimPolicy != "" {
		args = append(args, "--reclaim-policy", t.ReclaimPolicy)
	}
	args = append(args, kvFlags("--annotation", t.Annotations)...)
	args = append(args, kvFlags("--label", t.Labels)...)
	args = append(args, t.Name)
	if t.Path != "" {
		args = append(args, t.Path)
	}
	return args
}

// kvFlags renders a string map as the repeated `--<flag> key=value` pairs
// dokku collects into a map. Keys are sorted so the rendered command is
// stable across runs.
func kvFlags(flag string, values map[string]string) []string {
	if len(values) == 0 {
		return nil
	}
	keys := mapKeys(values)
	sort.Strings(keys)
	args := make([]string, 0, len(keys)*2)
	for _, k := range keys {
		args = append(args, flag, fmt.Sprintf("%s=%s", k, values[k]))
	}
	return args
}

// setEntryOptions returns the recipe keys of every create-time option the
// task supplied, in field order. Keep it in step with createArgs: an option
// that renders a flag but is missing here would be silently accepted under
// state 'absent', where storage:destroy has nowhere to put it. 'scheduler'
// is deliberately absent - it carries a default, so it is never empty by
// the time Validate runs. So is 'destroy_host_dir', which renders a flag on
// storage:destroy rather than storage:create and is therefore the one field
// state 'absent' wants rather than refuses; Validate rejects it from the
// other direction instead.
func (t StorageEntryTask) setEntryOptions() []string {
	var set []string
	for _, opt := range []struct {
		name string
		used bool
	}{
		{"path", t.Path != ""},
		{"size", t.Size != ""},
		{"access_mode", t.AccessMode != ""},
		{"storage_class", t.StorageClass != ""},
		{"namespace", t.Namespace != ""},
		{"chown", t.Chown != ""},
		{"mode", t.Mode != ""},
		{"reclaim_policy", t.ReclaimPolicy != ""},
		{"annotations", len(t.Annotations) > 0},
		{"labels", len(t.Labels) > 0},
	} {
		if opt.used {
			set = append(set, opt.name)
		}
	}
	return set
}

// dockerNamedVolume matches the docker named-volume token dokku accepts in
// place of an absolute path on a docker-local entry. Mirrors the regexp in
// dokku's plugins/storage/entry.go.
var dockerNamedVolume = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]+$`)

// directoryMode matches the 3 or 4 digit octal directory mode dokku accepts
// for --mode. Mirrors the regexp in dokku's plugins/storage/entry.go.
var directoryMode = regexp.MustCompile(`^[0-7]{3,4}$`)

// normalizeDirectoryMode canonicalizes a mode to the 4 digit form the
// registry records, mirroring NormalizeDirectoryMode in dokku's
// plugins/storage/entry.go. Convergence depends on it: dokku stores 0755
// whether the caller wrote 755 or 0755, so comparing the raw recipe value
// against the recorded one would report drift on every run for a recipe
// that wrote three digits, and each apply would "fix" a mode that was
// already correct. Callers validate the value first, so a mode that is
// neither 3 nor 4 digits is returned unchanged rather than mangled.
func normalizeDirectoryMode(mode string) string {
	if len(mode) == 3 {
		return "0" + mode
	}
	return mode
}

// storageAccessModes lists the values dokku accepts for --access-mode.
var storageAccessModes = map[string]bool{
	"ReadWriteOnce":    true,
	"ReadOnlyMany":     true,
	"ReadWriteMany":    true,
	"ReadWriteOncePod": true,
}

// Validate checks the StorageEntryTask's inputs without contacting the
// server. It mirrors the rules dokku's own Entry.Validate enforces, so a
// combination the server would refuse fails at `docket validate` time
// instead of halfway through an apply. It deliberately goes no further:
// dokku ignores rather than rejects namespace, reclaim_policy,
// annotations, and labels on a docker-local entry, and a stricter docket
// would refuse recipes the server accepts.
func (t StorageEntryTask) Validate() error {
	// storage:destroy takes only the entry name, so any create-time option
	// supplied alongside state 'absent' would be silently discarded.
	if t.State == StateAbsent {
		if set := t.setEntryOptions(); len(set) > 0 {
			return fmt.Errorf("'%s' must not be set for state 'absent'", strings.Join(set, "', '"))
		}
		return nil
	}

	// The mirror of the check above: storage:destroy is the only command
	// that takes the flag, so a recipe asking for it under any other state
	// is asking for a removal that will never happen.
	if t.DestroyHostDir {
		return errors.New("'destroy_host_dir' must not be set for state 'present'")
	}

	// An omitted scheduler is docker-local: createArgs drops the flag so
	// dokku applies its own default, and a task built in Go rather than
	// parsed from a recipe never went through the `default:` tag.
	switch t.Scheduler {
	case "", "docker-local":
		for _, field := range []struct {
			name  string
			value string
		}{
			{"size", t.Size},
			{"access_mode", t.AccessMode},
			{"storage_class", t.StorageClass},
		} {
			if field.value != "" {
				return fmt.Errorf("'%s' must not be set for scheduler 'docker-local'", field.name)
			}
		}
		if t.Path != "" && !strings.HasPrefix(t.Path, "/") && !dockerNamedVolume.MatchString(t.Path) {
			return fmt.Errorf("'path' must be an absolute path or a docker named volume, got %q", t.Path)
		}
	case "k3s":
		if t.Size == "" {
			return errors.New("'size' is required for scheduler 'k3s'")
		}
		if t.StorageClass != "" && t.Path != "" {
			return errors.New("'storage_class' and 'path' must not both be set: the cluster provisions class-backed volumes")
		}
		if t.AccessMode != "" && !storageAccessModes[t.AccessMode] {
			return errors.New("'access_mode' must be one of ReadWriteOnce, ReadOnlyMany, ReadWriteMany, ReadWriteOncePod")
		}
		if t.Path != "" && !strings.HasPrefix(t.Path, "/") {
			return fmt.Errorf("'path' must be an absolute path, got %q", t.Path)
		}
		if t.ReclaimPolicy != "" && t.ReclaimPolicy != "Retain" && t.ReclaimPolicy != "Delete" {
			return errors.New("'reclaim_policy' must be one of Retain, Delete")
		}
		if t.Mode != "" {
			return errors.New("'mode' must not be set for scheduler 'k3s'")
		}
	default:
		return fmt.Errorf("'scheduler' must be one of docker-local, k3s, got %q", t.Scheduler)
	}

	if t.Chown != "" && !validChown(t.Chown) {
		return errors.New("'chown' must be one of heroku, herokuish, paketo, root, false or a numeric uid (0-65535)")
	}

	if t.Mode != "" && !directoryMode.MatchString(t.Mode) {
		return errors.New("'mode' must be a 3 or 4 digit octal directory mode, such as 0755")
	}

	for _, m := range []struct {
		name   string
		values map[string]string
	}{
		{"annotations", t.Annotations},
		{"labels", t.Labels},
	} {
		if err := validateKVFlag(m.name, m.values); err != nil {
			return err
		}
	}

	return nil
}

// validateKVFlag rejects keys and values a repeated `--<flag> key=value`
// argument cannot carry intact. dokku splits each pair on its first '=',
// so a key holding one would silently restructure the map; and the flag is
// a pflag string slice, which comma-splits its argument through a CSV
// reader before dokku ever sees it, so a comma or a double quote on either
// side of the pair breaks the parse rather than the value.
//
// An empty value is refused for a different reason: it is what
// `storage:annotations:set` and `storage:labels:set` read as "delete this
// key", so a pair the recipe declares empty is one the server can never
// hold, and the convergence pass would clear it and find it missing again
// on every run. Same rule the scheduler-k3s pair tasks carry (#358).
// Callers reach this only under state 'present'; Validate returns before it
// for state 'absent', where no pair is dispatched at all.
func validateKVFlag(name string, values map[string]string) error {
	keys := mapKeys(values)
	sort.Strings(keys)
	for _, k := range keys {
		if k == "" {
			return fmt.Errorf("'%s' keys must not be empty", name)
		}
		if strings.Contains(k, "=") {
			return fmt.Errorf("'%s' key %q must not contain '='", name, k)
		}
		if strings.ContainsAny(k, `,"`) {
			return fmt.Errorf("'%s' key %q must not contain ',' or '\"'", name, k)
		}
		if strings.ContainsAny(values[k], `,"`) {
			return fmt.Errorf("'%s' value for %q must not contain ',' or '\"'", name, k)
		}
		if values[k] == "" {
			return fmt.Errorf("'%s' value for %q must not be empty; dokku reads an empty value as a delete", name, k)
		}
	}
	return nil
}

// Plan reports the drift the StorageEntryTask would produce.
func (t StorageEntryTask) Plan(ctx context.Context) PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			entry, err := lookupStorageEntry(ctx, t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if entry != nil {
				return planStorageEntryAttributes(ctx, t, *entry)
			}
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: t.createArgs()}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    "entry missing",
				Mutations: []string{fmt.Sprintf("create storage entry %s", t.Name)},
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			entry, err := lookupStorageEntry(ctx, t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if entry == nil {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			// dokku refuses the flag outright on anything but a docker-local
			// entry, where the underlying volume follows the reclaim policy
			// instead. Reporting it here fails the plan rather than the apply,
			// the same way an immutable attribute mismatch does.
			if t.DestroyHostDir && entry.Scheduler != "docker-local" {
				return PlanResult{
					Status: PlanStatusError,
					Error: fmt.Errorf("storage entry %s records scheduler %q: 'destroy_host_dir' only applies to a docker-local entry, whose counterpart elsewhere is the entry's reclaim_policy",
						t.Name, entry.Scheduler),
				}
			}
			args := []string{"--quiet", "storage:destroy", "--force"}
			if t.DestroyHostDir {
				args = append(args, "--destroy-host-dir")
			}
			args = append(args, t.Name)
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    "entry present",
				Mutations: []string{formatStorageEntryDestroy(t, *entry)},
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// storageEntryAttribute is one recipe field paired with what the registry
// records for it. Property is the `storage:set` property that converges the
// field, and is empty for a field dokku cannot change once the entry
// exists.
type storageEntryAttribute struct {
	Field    string
	Property string
	Desired  string
	Current  string
}

// drifted reports whether the recipe declared the field and the entry holds
// something else. A field the recipe omits is unmanaged: docket neither
// compares it nor clears it, so a recipe naming only `name` and `chown`
// leaves a `size` it never mentions alone.
func (a storageEntryAttribute) drifted() bool {
	return a.Desired != "" && a.Desired != a.Current
}

// immutableStorageEntryAttributes are the fields no dokku command changes
// once the entry exists. `storage:set` refuses an access-mode or a
// storage-class swap outright, since Kubernetes cannot rebind a bound PVC,
// and neither the scheduler nor the host path is a settable property at
// all - `storage:create` on an existing entry refuses a scheduler
// redefinition rather than honoring it.
//
// The scheduler is always compared. It carries a `default:` tag, so a
// decoded recipe always names one, and an empty value on a task built in Go
// means docker-local everywhere else: createArgs drops the flag so dokku
// applies the same default, and Validate reads it the same way.
func immutableStorageEntryAttributes(t StorageEntryTask, e storageEntry) []storageEntryAttribute {
	scheduler := t.Scheduler
	if scheduler == "" {
		scheduler = "docker-local"
	}
	return []storageEntryAttribute{
		{Field: "scheduler", Desired: scheduler, Current: e.Scheduler},
		{Field: "path", Desired: t.Path, Current: e.HostPath},
		{Field: "access_mode", Desired: t.AccessMode, Current: e.AccessMode},
		{Field: "storage_class", Desired: t.StorageClass, Current: e.StorageClass},
	}
}

// mutableStorageEntryAttributes are the scalar fields `storage:set`
// converges, in struct field order so plan and apply build byte-identical
// argv across runs. dokku re-applies the host directory's ownership and
// permissions whenever a storage:set touches chown or mode, so setting
// either here changes the directory on a docker-local entry rather than only
// the recorded value - which is the whole reason attribute convergence is
// worth doing. mode is normalized to the 4 digit form the registry records,
// or a recipe writing three digits would drift forever against its own
// applied value.
func mutableStorageEntryAttributes(t StorageEntryTask, e storageEntry) []storageEntryAttribute {
	return []storageEntryAttribute{
		{Field: "size", Property: "size", Desired: t.Size, Current: e.Size},
		{Field: "namespace", Property: "namespace", Desired: t.Namespace, Current: e.Namespace},
		{Field: "chown", Property: "chown", Desired: t.Chown, Current: e.Chown},
		{Field: "mode", Property: "mode", Desired: normalizeDirectoryMode(t.Mode), Current: e.Mode},
		{Field: "reclaim_policy", Property: "reclaim-policy", Desired: t.ReclaimPolicy, Current: e.ReclaimPolicy},
	}
}

// planStorageEntryAttributes reports the drift between the recipe and an
// entry that already exists. An immutable mismatch short-circuits to an
// error before any mutation is planned, so an apply never gets half way
// through a set of changes it was always going to be refused for.
//
// It is a package-level func rather than a method because
// TestProbeSupportMatchesPlanWiring walks the tasks package for
// plan-returning methods and allows exactly one per task, Plan itself.
func planStorageEntryAttributes(ctx context.Context, t StorageEntryTask, entry storageEntry) PlanResult {
	for _, attr := range immutableStorageEntryAttributes(t, entry) {
		if !attr.drifted() {
			continue
		}
		return PlanResult{
			Status: PlanStatusError,
			Error: fmt.Errorf("storage entry %s records %s %q, recipe declares %q: dokku cannot change it on an entry that exists, so destroy and re-create the entry to apply it",
				t.Name, attr.Field, attr.Current, attr.Desired),
		}
	}

	var inputs []subprocess.ExecCommandInput
	var mutations []string

	for _, attr := range mutableStorageEntryAttributes(t, entry) {
		if !attr.drifted() {
			continue
		}
		inputs = append(inputs, subprocess.ExecCommandInput{
			Command: "dokku",
			Args:    []string{"--quiet", "storage:set", t.Name, attr.Property, attr.Desired},
		})
		mutations = append(mutations, formatStorageEntrySet(attr.Field, attr.Desired, attr.Current))
	}

	// The map fields converge one key at a time. dokku's wholesale
	// `storage:set --annotation` replaces the entire map, which would clear
	// every key the recipe does not name; the per-key subcommands leave
	// their siblings in place, which is what makes an omitted key unmanaged
	// here the same way an omitted scalar is.
	for _, m := range []struct {
		noun    string
		command string
		desired map[string]string
		current map[string]string
	}{
		{"annotation", "storage:annotations:set", t.Annotations, entry.Annotations},
		{"label", "storage:labels:set", t.Labels, entry.Labels},
	} {
		drifted, _ := driftedKeys(m.desired, m.current)
		for _, key := range drifted {
			inputs = append(inputs, subprocess.ExecCommandInput{
				Command: "dokku",
				Args:    []string{"--quiet", m.command, t.Name, key, m.desired[key]},
			})
			mutations = append(mutations, formatStorageEntrySet(m.noun+" "+key, m.desired[key], m.current[key]))
		}
	}

	if len(inputs) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}

	return PlanResult{
		InSync:    false,
		Status:    PlanStatusModify,
		Reason:    fmt.Sprintf("%d attribute(s) to set", len(inputs)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StatePresent, inputs)
		},
	}
}

// formatStorageEntrySet renders one mutation line in the shape
// formatSetMutations produces for the map-pair tasks. An entry that records
// nothing for the field reads as new: the registry marshals an unset
// attribute and an empty one identically, so there is no third case to
// distinguish.
func formatStorageEntrySet(label, desired, current string) string {
	if current == "" {
		return fmt.Sprintf("set %s=%s (new)", label, desired)
	}
	return fmt.Sprintf("set %s=%s (was %q)", label, desired, current)
}

// formatStorageEntryDestroy renders the destroy mutation, naming the host
// directory when the apply would take it with the entry. dokku removes it
// either because the recipe asked or because the entry records a Delete
// reclaim policy, so the recipe's flag alone does not answer the question -
// a plan reporting only what the recipe asked for would stay silent about a
// directory that is about to go.
//
// One case over-reports: dokku's removal helper only ever operates on the
// default host path, so a Delete policy on a custom path is warned about and
// skipped. dokku's own entry validation refuses to create that combination,
// which leaves only entries written before it did - and docket cannot tell
// them apart anyway, since the default path depends on the server's
// DOKKU_LIB_ROOT.
func formatStorageEntryDestroy(t StorageEntryTask, entry storageEntry) string {
	removesHostDir := entry.Scheduler == "docker-local" &&
		(t.DestroyHostDir || entry.ReclaimPolicy == "Delete")
	if !removesHostDir {
		return fmt.Sprintf("destroy storage entry %s", t.Name)
	}
	return fmt.Sprintf("destroy storage entry %s and its host directory %s", t.Name, entry.HostPath)
}

// ExportGlobal reads the named storage registry entries and returns a
// dokku_storage_entry task per explicitly-created entry. Auto-generated
// "legacy-" entries (created implicitly by legacy bind-mounts) are skipped
// since they are reconstructed by dokku_storage_mount.
//
// storage:list-entries marshals the whole entry, so every field the task
// can set comes back from the one call. Reconstructing all of them is what
// makes the export lossless: dropping chown or mode would lose the host
// directory's ownership and permissions, and dropping size would produce a
// k3s entry the task's own validation rejects. destroy_host_dir is not
// exported because it is not state - it modifies a destroy the exported
// recipe does not perform.
func (t StorageEntryTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	entries, err := storageEntries(ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name < entries[j].Name })

	var out []interface{}
	for _, e := range entries {
		if e.Name == "" || strings.HasPrefix(e.Name, "legacy-") {
			continue
		}
		out = append(out, StorageEntryTask{
			Name:          e.Name,
			Path:          e.HostPath,
			Scheduler:     e.Scheduler,
			Size:          e.Size,
			AccessMode:    e.AccessMode,
			StorageClass:  e.StorageClass,
			Namespace:     e.Namespace,
			Chown:         e.Chown,
			Mode:          e.Mode,
			ReclaimPolicy: e.ReclaimPolicy,
			Annotations:   e.Annotations,
			Labels:        e.Labels,
		})
	}
	return out, nil
}

// storageEntry mirrors the JSON dokku's storage:list-entries emits.
type storageEntry struct {
	Name          string            `json:"name"`
	Scheduler     string            `json:"scheduler"`
	HostPath      string            `json:"host_path"`
	Size          string            `json:"size"`
	AccessMode    string            `json:"access_mode"`
	StorageClass  string            `json:"storage_class"`
	Namespace     string            `json:"namespace"`
	Chown         string            `json:"chown"`
	Mode          string            `json:"mode"`
	ReclaimPolicy string            `json:"reclaim_policy"`
	Annotations   map[string]string `json:"annotations"`
	Labels        map[string]string `json:"labels"`
}

// storageEntries reads the named storage registry. A transport-level
// failure (`*subprocess.SSHError`) is propagated; a dokku-level non-zero
// exit or unparseable output is treated as an empty registry, since a
// server without the entries directory has none to report.
func storageEntries(ctx context.Context) ([]storageEntry, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "storage:list-entries", "--format", "json"},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return nil, nil
	}

	var entries []storageEntry
	if err := json.Unmarshal(result.StdoutBytes(), &entries); err != nil {
		return nil, nil
	}
	return entries, nil
}

// lookupStorageEntry returns the named storage registry entry, or nil when
// the registry holds no entry by that name. Plan needs the whole entry
// rather than a yes/no answer, since every field the recipe declares is
// compared against what the registry records. A transport-level failure
// (`*subprocess.SSHError`) is propagated; a dokku-level non-zero exit is
// treated as "no entry."
func lookupStorageEntry(ctx context.Context, name string) (*storageEntry, error) {
	entries, err := storageEntries(ctx)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		if entries[i].Name == name {
			return &entries[i], nil
		}
	}
	return nil, nil
}

// init registers the StorageEntryTask with the task registry
func init() {
	RegisterTask(&StorageEntryTask{})
}
