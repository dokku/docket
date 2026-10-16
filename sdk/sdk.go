package sdk

import (
	"context"

	"github.com/dokku/docket/internal/subprocess"
	"github.com/dokku/docket/internal/tasks"
)

// Building and describing tasks.

// NewTask returns a new task of the registered type typeKey, such as
// "dokku_app", with its `default:` values applied. Use it rather than a struct
// literal, which leaves State as "" instead of the default the field documents.
//
// The task is a pointer to the registered type, so fields are set through a
// type assertion:
//
//	task, err := sdk.NewTask("dokku_app")
//	app := task.(*sdk.AppTask)
//	app.App = "api"
func NewTask(typeKey string) (Task, error) {
	return tasks.NewTask(typeKey)
}

// DecodeTask builds a task of the registered type typeKey from a YAML task
// body, with defaults applied.
func DecodeTask(typeKey string, body []byte) (Task, error) {
	return tasks.DecodeTask(typeKey, body)
}

// TaskTypes returns every registered task type, sorted. The slice is the
// caller's own.
func TaskTypes() []string {
	return tasks.TaskTypes()
}

// Lookup reports whether typeKey is a registered task type and, when it is,
// returns a new zero-value instance of it for reading metadata. It has no
// defaults applied; use NewTask for a task to run.
func Lookup(typeKey string) (Task, bool) {
	return tasks.Lookup(typeKey)
}

// TaskSynopsis returns t's description, or "" when it has none.
func TaskSynopsis(t Task) string {
	return tasks.TaskSynopsis(t)
}

// TaskDeprecation returns t's deprecation notice, or "" when t is current.
func TaskDeprecation(t Task) string {
	return tasks.TaskDeprecation(t)
}

// TaskProbeSupport reports how much of its own state t's Plan can read back.
// The bool is false when t declares nothing.
func TaskProbeSupport(t Task) (ProbeSupport, bool) {
	return tasks.TaskProbeSupport(t)
}

// TaskExportSupport reports whether ExportRecipe can reconstruct t from a
// server. The bool is false when t declares nothing.
func TaskExportSupport(t Task) (ExportSupport, bool) {
	return tasks.TaskExportSupport(t)
}

// TaskMinimumVersions reports the oldest Dokku, and the plugins, t declares
// it runs on. The bool is false when t declares nothing.
func TaskMinimumVersions(t Task) (VersionSupport, bool) {
	return tasks.TaskMinimumVersions(t)
}

// Catalog describes every registered task type. It is what `docket schema`
// emits.
func Catalog() (TaskCatalog, error) {
	return tasks.Catalog()
}

// CatalogFor describes only the given task types. An unregistered type is an
// error.
func CatalogFor(typeKeys []string) (TaskCatalog, error) {
	return tasks.CatalogFor(typeKeys)
}

// CatalogVersion is the TaskCatalog.Version this release emits. It changes
// only when the catalog's shape does, so a consumer can branch on it.
const CatalogVersion = tasks.CatalogVersion

// Field and item type names used in FieldSchema.Type and ItemSchema.Type.
const (
	TypeString  = tasks.TypeString
	TypeBool    = tasks.TypeBool
	TypeInt     = tasks.TypeInt
	TypeFloat   = tasks.TypeFloat
	TypeList    = tasks.TypeList
	TypeDict    = tasks.TypeDict
	TypeObject  = tasks.TypeObject
	TypeAny     = tasks.TypeAny
	TypeUnknown = tasks.TypeUnknown
)

// Identity roles a FieldSchema can carry.
const (
	IdentityRoleKey        = tasks.IdentityRoleKey
	IdentityRoleCollection = tasks.IdentityRoleCollection
)

// How a collection's items are told apart.
const (
	ItemIdentityValue  = tasks.ItemIdentityValue
	ItemIdentityMapKey = tasks.ItemIdentityMapKey
	ItemIdentityFields = tasks.ItemIdentityFields
)

// Scopes a property can be set in.
const (
	PropertyScopeApp    = tasks.PropertyScopeApp
	PropertyScopeGlobal = tasks.PropertyScopeGlobal
)

// PropertyFieldName is the recipe key every property table constrains.
const PropertyFieldName = tasks.PropertyFieldName

// Probe and export support levels.
const (
	ProbeSupported   = tasks.ProbeSupported
	ProbePartial     = tasks.ProbePartial
	ProbeUnsupported = tasks.ProbeUnsupported

	ExportSupported   = tasks.ExportSupported
	ExportPartial     = tasks.ExportPartial
	ExportUnsupported = tasks.ExportUnsupported
)

// Identity addresses.

// IdentityAddress renders the resource t addresses, such as
// `dokku_config[app=api]`. It is the form ParseIdentityAddress and
// ParseResourceSelectors read back.
func IdentityAddress(typeKey string, t Task) string {
	return tasks.IdentityAddress(typeKey, t)
}

// ParseIdentityAddress parses `dokku_config[app=api]` into its type key and
// key map. A bare type key parses to an empty map.
func ParseIdentityAddress(address string) (string, map[string]string, error) {
	return tasks.ParseIdentityAddress(address)
}

// ParseResourceSelectors parses addresses for ExportOptions.Resources. Each
// must name a registered, exportable task type and only keys it declares.
func ParseResourceSelectors(addresses []string) ([]ResourceSelector, error) {
	return tasks.ParseResourceSelectors(addresses)
}

// Reading a server.

// ExportRecipe reads the server ctx targets and returns the recipe that
// describes it. An export that finds nothing is not an error: check
// Report.MissingApps and Report.MissingResources.
//
// An exporter whose task declares a floor newer than the server is skipped
// with a warning and listed in Report.VersionErrors. Set
// ExportOptions.IgnoreVersion to read it anyway.
func ExportRecipe(ctx context.Context, opts ExportOptions) (*ExportResult, error) {
	if !opts.IgnoreVersion {
		ctx = tasks.ContextWithVersionGate(ctx)
	}
	return tasks.ExportRecipe(ctx, opts)
}

// As returns an exported task's body as T, the value type registered under
// e.Type (ConfigTask, not *ConfigTask). It reports false for any other type.
func As[T Task](e ExportedTask) (T, bool) {
	return tasks.As[T](e)
}

// Recipe formats ExportResult.MarshalRecipe accepts.
const (
	FormatYAML  = tasks.FormatYAML
	FormatJSON5 = tasks.FormatNameJSON5
	FormatHCL   = tasks.FormatNameHCL
)

// Planning and applying.

// ContextWithVersionGate returns a copy of ctx on which CheckMinimumVersions
// compares a task's floor with the server. The versions it reads are cached
// per target for as long as the context lives, so install it once per run.
// A context that already carries a gate is returned as-is.
func ContextWithVersionGate(ctx context.Context) context.Context {
	return tasks.ContextWithVersionGate(ctx)
}

// CheckMinimumVersions returns a *VersionError when the server ctx targets,
// or an installed plugin task needs, is older than the floor task declares.
// A task's Plan and Execute already refuse through it when ctx carries a
// gate; call it to check a task before planning it. It returns nil when ctx
// carries no gate, when task declares no floor, and when a plugin task needs
// is not installed, which the task's own probe reports. typeKey names the
// task in the error, such as "dokku_app".
func CheckMinimumVersions(ctx context.Context, typeKey string, task Task) error {
	return tasks.CheckMinimumVersions(ctx, typeKey, task)
}

// ReadTask returns task as it exists on the server, and whether the resource
// exists, from a single Plan; nothing is applied. The returned task is the
// value type registered for task (AppTask, not *AppTask), carrying the
// identity keys from task and every field the server reports. Fields whose
// catalog entry is WriteOnly or ApplyOnly are left empty or at their default.
//
// task must be a valid task: its State picks which members of a collection
// are read, so a dokku_config in state present reads the keys it names, and
// one in state set reads every key it would own. A property task known only
// by its identity keys is read with State absent, which needs no Value.
//
// When ctx carries a version gate and the server is older than task's floor,
// the error is the *VersionError, never a resource reported as absent. A task
// whose probe cannot read its state returns an error wrapping
// ErrStateUnreadable.
func ReadTask(ctx context.Context, task Task) (Task, bool, error) {
	return tasks.ReadTask(ctx, task)
}

// ErrStateUnreadable is wrapped by the error ReadTask returns when a task's
// probe cannot read its current state, such as dokku_storage_ensure, or when
// the probe failed in a way the plan treats as drift.
var ErrStateUnreadable = tasks.ErrStateUnreadable

// ExecutePlan applies p, the result of a task's Plan, without probing the
// server again. A plan that is already in sync applies nothing.
func ExecutePlan(ctx context.Context, p PlanResult) TaskOutputState {
	return tasks.ExecutePlan(ctx, p)
}

// Task states.
const (
	StatePresent  = tasks.StatePresent
	StateAbsent   = tasks.StateAbsent
	StateDeployed = tasks.StateDeployed
	StateSet      = tasks.StateSet
	StateClear    = tasks.StateClear
)

// Plan statuses, as PlanResult.Status.
const (
	PlanStatusOK      = tasks.PlanStatusOK
	PlanStatusModify  = tasks.PlanStatusModify
	PlanStatusCreate  = tasks.PlanStatusCreate
	PlanStatusDestroy = tasks.PlanStatusDestroy
	PlanStatusError   = tasks.PlanStatusError
)

// PlanWarning reasons.
const (
	WarnReasonUnknownProperty    = tasks.WarnReasonUnknownProperty
	WarnReasonProbeRejected      = tasks.WarnReasonProbeRejected
	WarnReasonProbeIndeterminate = tasks.WarnReasonProbeIndeterminate
	WarnReasonServiceImageDrift  = tasks.WarnReasonServiceImageDrift
)

// Sessions, targets and masking.

// NewSession returns a session whose contexts carry masker. A nil masker
// leaves any masker already on the parent context in place.
func NewSession(masker *Masker) *Session {
	return subprocess.NewSession(masker)
}

// ErrSessionClosed is the error a call made under a closed session fails
// with. Test for it with errors.Is.
var ErrSessionClosed = subprocess.ErrSessionClosed

// ContextWithTarget returns a copy of ctx that sends dokku commands to target.
// A context derived from a session context still belongs to that session.
func ContextWithTarget(ctx context.Context, target Target) context.Context {
	return subprocess.ContextWithTarget(ctx, target)
}

// ValidateSSHOptions reports whether opts can be a Target's SSHOptions,
// without contacting anything: each must be non-empty, free of control
// characters, and must not set an option docket manages itself.
func ValidateSSHOptions(opts []string) error {
	return subprocess.ValidateSSHOptions(opts)
}

// NewMasker returns a masker for values.
func NewMasker(values ...string) *Masker {
	return subprocess.NewMasker(values...)
}

// MaskPlaceholder is what a Masker replaces a sensitive value with.
const MaskPlaceholder = subprocess.MaskPlaceholder

// TaskSensitiveValues returns the values the CLI masks for t before a run:
// its `sensitive:"true"` fields, including those of nested items, the value of
// a property marked sensitive, and whatever t's own SensitiveValues method
// returns. Pass them to NewMasker. Empty values are dropped and duplicates
// removed; the order is unspecified.
//
// A value only known once the server is read, such as a secret property's
// current value, is added to the context's masker during Plan.
func TaskSensitiveValues(t Task) []string {
	return tasks.TaskSensitiveValues(t)
}

// Testing.

// ContextWithRunner returns a copy of ctx whose commands go to fn instead of
// to a real process or server. The runner travels on the context rather than
// in a package variable, so two tests can install different fakes at the same
// time and both can call t.Parallel().
//
// Production code must not use it. The seam exists so a test can drive a task
// without a server; a caller that wants to change where commands actually run
// sets a Target.
//
// fn receives every command a call makes, not only dokku's, with its arguments
// as they would have run: unmasked, and without the sudo or SSH wrapping a
// Target adds. A Target on ctx has no effect while fn is installed.
//
// A command that ran and exited non-zero is a response carrying that ExitCode.
// A probe reads it as its answer whether fn returns a nil error alongside it
// or, as the real runner does, an *ExecError with Ran set. An apply fails only
// on an error, so return the *ExecError to fail one. Any other error is a
// command that could not run, which a probe reports as an error rather than
// as absence.
//
// The response's Command is what an apply records in TaskOutputState.Commands,
// so set it when a test reads that.
func ContextWithRunner(ctx context.Context, fn ExecRunner) context.Context {
	return subprocess.ContextWithRunner(ctx, fn)
}
