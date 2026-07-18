package tasks

import (
	"errors"
	"strconv"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// StorageEnsureTask manages the storage for a given dokku application
type StorageEnsureTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app" description:"Name of the app"`

	// Chown is the chown value to set
	Chown string `required:"false" yaml:"chown,omitempty" options:"heroku,herokuish,paketo,root,false" description:"Chown value to set: an ownership preset or a numeric uid (0-65535)"`

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
// chown is optional: an omitted value lets dokku apply its default (herokuish)
// ownership. A non-empty value must be one dokku's storage:ensure-directory
// accepts - a named ownership preset or a numeric uid in 0-65535 (see
// validChown) - so a typo or out-of-range value is caught before dispatch.
func (t StorageEnsureTask) Validate() error {
	if t.State == StatePresent && t.Chown != "" && !validChown(t.Chown) {
		return errors.New("'chown' must be one of heroku, herokuish, paketo, root, false or a numeric uid (0-65535)")
	}
	if t.State == StateAbsent {
		return errors.New("the absent state is not supported for storage:ensure")
	}
	return nil
}

// validChown reports whether a chown value is one dokku's
// storage:ensure-directory accepts: a named ownership preset or a raw
// numeric uid. dokku's ResolveChownID maps the presets to uids and parses
// a numeric value with strconv.ParseUint(value, 10, 16), so docket mirrors
// that here - accepting a uid in 0-65535 - while still catching typos in
// non-numeric values before dispatch. dokku's deprecated 'packeto' alias is
// intentionally not accepted so the typo surfaces at validate time. The raw
// string is validated with the same base-10 parser dokku applies to the
// --chown argument, so docket's decision always matches dokku's runtime parse.
func validChown(chown string) bool {
	switch chown {
	case "heroku", "herokuish", "paketo", "root", "false":
		return true
	}
	_, err := strconv.ParseUint(chown, 10, 16)
	return err == nil
}

// ensureArgs builds the storage:ensure-directory command arguments. The
// --chown flag is omitted when no chown value is set so the field stays
// genuinely optional and dokku applies its default ownership.
func (t StorageEnsureTask) ensureArgs() []string {
	args := []string{"--quiet", "storage:ensure-directory"}
	if t.Chown != "" {
		args = append(args, "--chown", t.Chown)
	}
	return append(args, t.App)
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
			args := t.ensureArgs()
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    args,
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusModify,
				Reason:    "directory presence not probed",
				Mutations: []string{strings.Join(args[1:], " ")},
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
