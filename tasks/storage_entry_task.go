package tasks

import (
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
// Idempotency is keyed on the entry name: when an entry with the given
// name exists, the task is in sync regardless of the other field
// values. Attribute changes are therefore not drift-detected; to change
// scheduler, size, or any other attribute, the recipe must destroy and
// re-create the entry. Converging them in place is tracked in
// https://github.com/dokku/docket/issues/439 - it needs a different
// command per scheduler, since `storage:set --chown` records a new
// ownership value without re-running the chown on a docker-local
// directory.
type StorageEntryTask struct {
	// Name is the name of the storage entry
	Name string `required:"true" yaml:"name" description:"Name of the storage entry"`

	// Path is the host path for the entry. Optional; on docker-local it
	// defaults to the dokku storage root + name.
	Path string `required:"false" yaml:"path,omitempty" description:"Host path for the entry: an absolute path, or a docker named volume on docker-local. Defaults to the dokku storage root joined with the entry name"`

	// Scheduler is the scheduler that backs the entry
	Scheduler string `required:"false" yaml:"scheduler,omitempty" default:"docker-local" options:"docker-local,k3s" description:"Scheduler that backs the entry"`

	// Size is the volume size (k3s scheduler; required there, rejected on docker-local)
	Size string `required:"false" yaml:"size,omitempty" description:"Volume size (k3s scheduler; required there and rejected on docker-local)"`

	// AccessMode is the volume access mode (k3s scheduler)
	AccessMode string `required:"false" yaml:"access_mode,omitempty" options:"ReadWriteOnce,ReadOnlyMany,ReadWriteMany,ReadWriteOncePod" description:"Volume access mode (k3s scheduler; rejected on docker-local)"`

	// StorageClass is the storage class name (k3s scheduler)
	StorageClass string `required:"false" yaml:"storage_class,omitempty" description:"Storage class name (k3s scheduler; rejected on docker-local, and mutually exclusive with path)"`

	// Namespace is the namespace (scheduler-dependent)
	Namespace string `required:"false" yaml:"namespace,omitempty" description:"Namespace (scheduler-dependent)"`

	// Chown is the chown value applied when the entry's host directory is created
	Chown string `required:"false" yaml:"chown,omitempty" options:"heroku,herokuish,paketo,root,false" description:"Ownership applied when the entry's host directory is created: an ownership preset or a numeric uid (0-65535). dokku sets the owner and the group to the same id, and refuses the value unless the entry sits at its default host path"`

	// ReclaimPolicy is the reclaim policy (k3s scheduler)
	ReclaimPolicy string `required:"false" yaml:"reclaim_policy,omitempty" options:"Retain,Delete" description:"Reclaim policy applied to the underlying volume (k3s scheduler)"`

	// Annotations are the volume annotations (k3s scheduler)
	Annotations map[string]string `required:"false" yaml:"annotations,omitempty" description:"Map of annotations set on the underlying volume (k3s scheduler)"`

	// Labels are the volume labels (k3s scheduler)
	Labels map[string]string `required:"false" yaml:"labels,omitempty" description:"Map of labels set on the underlying volume (k3s scheduler)"`

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
// the time Validate runs.
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
	default:
		return fmt.Errorf("'scheduler' must be one of docker-local, k3s, got %q", t.Scheduler)
	}

	if t.Chown != "" && !validChown(t.Chown) {
		return errors.New("'chown' must be one of heroku, herokuish, paketo, root, false or a numeric uid (0-65535)")
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
	}
	return nil
}

// Plan reports the drift the StorageEntryTask would produce.
func (t StorageEntryTask) Plan() PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			exists, err := storageEntryExists(t.Name)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if exists {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: t.createArgs()}}
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

// ExportGlobal reads the named storage registry entries and returns a
// dokku_storage_entry task per explicitly-created entry. Auto-generated
// "legacy-" entries (created implicitly by legacy bind-mounts) are skipped
// since they are reconstructed by dokku_storage_mount.
//
// storage:list-entries marshals the whole entry, so every field the task
// can set comes back from the one call. Reconstructing all of them is what
// makes the export lossless: dropping chown would lose the host
// directory's ownership, and dropping size would produce a k3s entry the
// task's own validation rejects.
func (t StorageEntryTask) ExportGlobal() ([]interface{}, error) {
	entries, err := storageEntries()
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
	ReclaimPolicy string            `json:"reclaim_policy"`
	Annotations   map[string]string `json:"annotations"`
	Labels        map[string]string `json:"labels"`
}

// storageEntries reads the named storage registry. A transport-level
// failure (`*subprocess.SSHError`) is propagated; a dokku-level non-zero
// exit or unparseable output is treated as an empty registry, since a
// server without the entries directory has none to report.
func storageEntries() ([]storageEntry, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
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

// storageEntryExists reports whether a named storage registry entry
// exists. A transport-level failure (`*subprocess.SSHError`) is
// propagated; a dokku-level non-zero exit is treated as "no entry."
func storageEntryExists(name string) (bool, error) {
	entries, err := storageEntries()
	if err != nil {
		return false, err
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
