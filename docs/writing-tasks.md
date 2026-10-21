# Writing tasks

This page is for contributors adding new task types to docket. If you only want to *use* tasks in a
recipe, see the [task reference](tasks/README.md) instead.

Tasks closely follow the modules available in
[`ansible-dokku`](https://github.com/dokku/ansible-dokku), so that existing task lists migrate with
minimal changes. docket adds a few of its own tasks specific to this package. A task has a name and
an execution context that maps to a single module; its fields can be templated from recipe
[inputs](inputs.md) and any function [sigil](https://github.com/gliderlabs/sigil) exposes.

## The Plan / Execute model

Every task implements two methods. `Plan(ctx)` is the canonical one: it probes the live server once,
computes the difference between the current and desired state, and returns a `PlanResult`. When the
server is not already in the desired state, `Plan()` embeds an `apply` closure that performs the
mutation. `Execute(ctx)` is always just `executeTask(ctx, t)`, which takes the locks on the app or
service the task changes (see [declaring objects](#declaring-objects)) and runs
`ExecutePlan(ctx, t.Plan(ctx))` under them - the shared `ExecutePlan` helper handles the in-sync,
error, and apply cases uniformly, so the mutation logic lives in exactly one place per task.

This split is why `plan` and `apply` agree: both call the same `Plan()`. `apply` reuses the probe
to decide whether to mutate, which is what makes back-to-back applies no-ops.

Both take a `context.Context`, and so does every helper below them. Pass it to each
`subprocess.CallExecCommand` / `subprocess.Probe` call rather than reaching for
`context.Background()`: it is what carries cancellation, so an interrupt or a caller's deadline
reaches a task that is mid-command instead of waiting for the command to finish. The `apply` closure
takes one rather than capturing the one `Plan()` ran under, because `ExecutePlan` invokes it
separately and hands it the caller's current context.

## Adding a new task

Create `internal/tasks/${TASK_NAME}_task.go`, where the task name is `lower_underscore_case`. For a task
named `lollipop`, `internal/tasks/lollipop_task.go` would contain:

```go
package tasks

import "context"

type LollipopTask struct {
  App     string   `required:"true" identity:"key" serialize:"app" yaml:"app" description:"Name of the app"`
  Flavors []string `required:"false" identity:"collection" yaml:"flavors,omitempty" description:"Flavors to apply"`
  State   State    `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the lollipop"`
}

func (t LollipopTask) Plan(ctx context.Context) PlanResult {
  // Refuse the task when the context carries a version gate and the server is
  // older than MinimumVersions declares.
  if r, ok := gatePlan(ctx, t); !ok {
    return r
  }
  // DispatchPlan keeps its argument-free branch signature; the closures capture
  // ctx from the enclosing Plan.
  return DispatchPlan(t.State, map[State]func() PlanResult{
    "present": func() PlanResult {
      // Probe the server once, decide whether to mutate.
      current := /* the flavors the server holds, among t.Flavors */
      seen := LollipopTask{App: t.App, Flavors: current, State: StatePresent}
      if len(current) == 0 {
        seen.State = StateAbsent
      }
      if /* already in desired state */ {
        return observed(PlanResult{InSync: true, Status: PlanStatusOK}, seen, len(current) > 0)
      }
      return observed(PlanResult{
        InSync:    false,
        Status:    PlanStatusCreate, // or PlanStatusModify, PlanStatusDestroy
        Reason:    "...",
        Mutations: []string{"create lollipop"},
        apply: func(ctx context.Context) TaskOutputState {
          // Run the underlying dokku command. Return Changed=true on success.
          return TaskOutputState{Changed: true, State: StatePresent}
        },
      }, seen, len(current) > 0)
    },
    "absent": func() PlanResult { /* ... */ },
  })
}

func (t LollipopTask) Execute(ctx context.Context) TaskOutputState {
  return executeTask(ctx, t)
}

func init() {
  RegisterTask(&LollipopTask{})
}
```

A few conventions to follow:

- The struct holds the fields the task needs. The only required field is `State`, the desired state;
  everything else is specific to the task.
- Give every field a `description:"..."` tag. The docs generator reads it (along with `required`,
  `required_when`, `forbidden_when`, `default`, `options`, `sensitive`, `runner_file`, `write_only`,
  `apply_only`, and `ordered`) to build the task's Parameters table, so a field without one renders an empty description cell. Those same tags are what
  [`docket schema`](task-catalog.md) publishes, so a missing description is a hole in the
  machine-readable catalog as well as in the docs. Add `,omitempty` to the `yaml` tag of optional
  fields so example YAML stays clean, and use `required:"false"` whenever a field has a `default`
  (a defaulted field is never actually required).
- For a task that performs several atomic changes in one call (such as setting multiple config
  keys), populate `PlanResult.Mutations` with one entry per change, so `plan` can itemize the diff.
- `DispatchPlan` and `DispatchState` set `DesiredState` on the result automatically.
- `Plan()` starts with the `gatePlan` check, before validation and before any probe, so a task
  planned outside the CLI honours a version gate on its context.
  `TestEveryPlanChecksVersionGate` fails the build for a `Plan()` that does not.
- `Execute()` is exactly `return executeTask(ctx, t)`, so it holds the task's locks across its plan
  and its apply. `TestEveryExecuteTakesTaskLocks` fails the build for an `Execute()` that plans and
  applies itself.
- Every result built from a probe goes through `observed(result, t, read, exists)`, which sets
  `PlanResult.Observed` and `PlanResult.Exists` - what `sdk.ReadTask` returns (see
  [Reading one resource](embedding.md#reading-one-resource)). `t` is the planned task, and a
  shared helper that builds the result takes it as a parameter. `read` is the value type, with
  the identity keys copied from `t` and every field the probe read. Fields tagged `write_only` or
  `apply_only` stay at their zero value or default. `State` is the state that would plan in sync
  against what was read. A collection in an additive state reads only the members `t` names, and
  one in a state that owns the collection reads them all. A drift result observes `read`. An
  in-sync result observes `t` instead, with each field `t` leaves at its zero value filled from
  `read` (matching list items on their identity keys), and `Exists` follows `t`'s state, so build
  `read` the same way either way. Read every field the task's exporter
  reads, even one the drift check ignores, unless reading it would undo a deliberate choice of the
  probe's - such as `dokku_certs` never moving certificate material at plan time - in which case
  tag the field `write_only`. A probe failure the plan treats as drift, rather than as an error,
  sets no observed state. `TestPlanObservesServerState` plans every exported body against a fixture
  in `observed_*_test.go` and checks that it reads itself, and that each case reads what it
  states. `TestInSyncPlanObservesTheDeclaredTask` checks that every plan in the fixture that is in
  sync observes the fields its task sets as declared, and fails a fixture with no such plan.
  `TestObservedFixturesCoverEveryTask` fails the build for a task with no fixture, and
  `TestObservedReadsEveryUntaggedField` fails it for a field that is not an identity key, not
  tagged, and set by no probe's read across the fixture - so give the fixture's server a value
  for every field the probe reads.
- A `readAddress` returns `observedResult(result, true)`, which hands back `read` even when the
  result is in sync, since an address read has no declared task to return.
- `sdk.ReadAddress` reads a resource from its identity address alone. It builds the task from the
  address's keys and plans it in `clear` when the task offers it, else `absent`, else the default.
  When that cannot read the whole resource - a payload `Validate` demands in every state, or states
  that each read only the members they name - implement `readAddress(ctx) (Task, bool, error)` on
  the task, reusing the observed construction `Plan` uses (`dokku_app_clone`).
  `TestReadAddressReadsEveryResource` reads every exported body back from its address and needs an
  address read of a present and an absent resource for every task that can read its state; add the
  missing ones to `observedAddressCases` in `observed_address_test.go`.
- A read that does not find the resource asks whether each app or service the task depends on - its
  `serialize` objects, less the ones `ObjectEffects` says it creates - exists, and reports a missing
  one as an `*ObjectMissingError`, so a probe need not check its parent. A `Plan` that checks its
  parent anyway returns `objectMissing(t, ObjectApp)` or `objectMissing(t, ObjectService)` as its
  error, not a plain one. `TestReadReportsAMissingObject` reads every fixture address from a server
  where every command fails and fails the build for a task that does not report its missing parent;
  each absent address case's parent probe is answered as existing by the fixture unless it lists the
  probe itself.
- `init()` registers the task with `RegisterTask`, which makes it usable in a recipe.
- Every task must implement `MinimumVersions() VersionSupport`. `Dokku` is the oldest release
  whose command and output shape the current probe parses, as `major.minor.patch`. Where the
  task's own comment names that release, use it. A property table that works on an older report,
  with a few keys that need a newer patch, takes the report baseline and leaves those keys in the
  existing comments. When the introducing release cannot be established from the dokku or plugin
  changelog, use `0.38.31` (the release the integration suite installs) and name the command in a
  comment. `Plugins` lists non-core plugins. `Name` is the `plugin:list` name (`letsencrypt`), and
  `Text` is the requirements line the page prints (`dokku-letsencrypt plugin >= 0.25.2`); the two
  are written out separately because they are not the same string. `NameField` is the Go field
  whose value is the plugin:list name (datastore tasks use `Service`). `WhenField` is the Go bool
  that must be true before the requirement applies (`dokku_certs` uses `Global`). The method
  returns the type-level declaration, including conditional plugins. Plan, apply, and export apply
  `WhenField` and `NameField` to the live task, and the catalog publishes the recipe keys for
  those fields. A coverage test fails the build when a task ships without a parseable declaration.
- When a field names a file docket itself reads on the machine it runs on (as
  `dokku_maintenance_custom_page` reads `tarball` and streams it to dokku), tag it
  `runner_file:"true"`. [`docket schema`](task-catalog.md#fields) publishes the flag and the
  generator lists the field under Runner requirements on the task's page, so someone running docket
  against a remote server knows the file has to exist locally. Do not tag a path the task hands to
  dokku as an argument - that resolves on the server.
- When the server stores a field's value but never returns it (a password applied at create, a
  registry username), tag it `write_only:"true"`. When a field only steers how apply runs and is
  not server state at all (a restart flag, a commit author, a forced re-issue), tag it
  `apply_only:"true"`. [`docket schema`](task-catalog.md#fields) publishes both, so a consumer
  refreshing state keeps the configured value instead of reporting drift. Neither may sit on an
  `identity:"key"` field, and a value that is unreadable only for some inputs belongs in the
  `ExportSupport()` / `ProbeSupport()` caveat instead. An exporting task with either tag needs a
  fixture in `internal/tasks/export_readback_test.go`, which fails the build otherwise and proves
  the exporter never fills the field from the server. An exporter that does recover a `write_only`
  field through a command the plan does not run (`dokku_certs` reads its PEM with `certs:show`)
  names the field in `exportReadsWriteOnly` there with the reason, and says so in its
  `ExportSupport()` caveat.
- Tag every slice field, element structs included, `ordered:"true"` or `ordered:"false"`. Use
  `true` only when the task applies the elements in order and its Plan treats a reorder as drift
  (`dokku_buildpacks`'s `buildpacks`); use `false` when it compares them as a set or sorts them.
  [`docket schema`](task-catalog.md#fields) publishes the answer so a consumer can choose a list or
  a set type, the generator marks an ordered field on the task's page, and a list field without
  the tag fails the build.
- When a task type is deprecated (typically because the underlying dokku subcommand was deprecated
  or a richer replacement task exists), implement the optional `Deprecation() string` method.
  The generator renders the returned message in a Deprecated admonition on the task's page and
  appends `(deprecated)` to the task's index entry; `apply --list-tasks` marks it the same way;
  `apply` and `plan` emit a one-time `warning` line above each deprecated task's result line.
  Keep the message short and name the replacement, e.g.
  `"use dokku_storage_entry instead; storage:ensure-directory has been deprecated"`.
- When a task runs an operation rather than declaring a resource (`dokku_app_clone`,
  `dokku_git_sync`, `dokku_git_from_archive`, `dokku_git_from_image`), implement the optional
  `Imperative() bool` method and return true. Such a task has nothing to undo, so it offers no
  `absent` or `clear` state. [`docket schema`](task-catalog.md) marks it `imperative`, and the
  generator adds a note to its page. `TestImperativeTasksOfferNoTeardown` fails the build if an
  imperative task offers a teardown state, or if a task offers neither `absent` nor `clear`
  without declaring itself imperative.
- Every task must also implement `Doc() string` and `examples() ([]Doc, error)`, the optional
  `Documented` interface. They are not part of `Task`, since running a task never needs them, but
  they supply the Synopsis and Examples on the task's generated page and in
  [`docket schema`](task-catalog.md). `TestEveryTaskIsDocumented` fails the build if a task ships
  without them, with an empty `Doc()`, or with no examples.
- Every task must implement `ExportSupport() ExportSupport`, declaring whether `docket export` can
  reconstruct it from a live server: `ExportSupported`, `ExportPartial`, or `ExportUnsupported`,
  with a `Caveat` explaining anything short of supported. The generator renders it in an Export
  support section, and a coverage test fails the build if a task ships without a decision. To
  actually export, implement `ExportApp` or `ExportGlobal` as well - see
  [exporting a task](#exporting-a-task) below.
- Every task must also implement `ProbeSupport() ProbeSupport`, declaring how much of its own state
  `Plan()` can read back: `ProbeSupported`, `ProbePartial` (some fields have no read command), or
  `ProbeUnsupported` (none do, so the task reports drift on every run and never converges). Name
  what cannot be read in the `Caveat`. The generator renders it in a Probe support section, the
  tasks index marks an unsupported task `(never converges)`, and `--list-tasks` marks it the same
  way. `TestProbeSupportMatchesPlanWiring` cross-checks the declaration against the code, and it
  asks the question per `state:`, not per task: every branch of your `DispatchPlan` map must have
  an `InSync: true` result reachable from it, and a task that claims it cannot converge must have
  none. Adding a state you have no read command for therefore fails the build even when the task's
  other states probe fine - there is no way to declare "converges for `present`, drifts forever for
  `absent`". Branches reached through `planToggle`, `planProperty` and `planResource` count as your
  task's own. Declare it for the task type, not for the run - a probe that fails on a particular
  server is a `PlanWarning`, not a `ProbeUnsupported` task.
- Every task must tag the fields that identify the resource it manages with `identity:"key"`. See
  [declaring identity](#declaring-identity) below; a coverage test fails the build if a task ships
  without one.
- When a field is needed in some states and refused in others, or fields depend on each other -
  a list that must be non-empty only when `state: present`, mutually-exclusive fields, a pair set
  together - declare it rather than checking it by hand. See
  [declaring field constraints](#declaring-field-constraints) below. Rules a declaration cannot
  express, such as an enum or a value format, go in the optional `Validate() error` method (the
  `InputValidator` interface), which `gatePlan` calls at the start of `Plan()`. See
  [validating inputs](#validating-inputs).

## Declaring identity

Two field tags say what a task addresses on the server. Nothing else in the struct does: a task's
`required:"true"` fields describe what a recipe must supply, which is a different question.

- `identity:"key"` marks a field that decides *which* resource the task manages. Declaration order
  is key order.
- `identity:"collection"` marks a collection the task manages wholesale. Item identity is inferred
  from the type: the element value for a `[]string`, the map key for a map, and the element struct's
  own `identity:"key"` fields for a slice of structs (see `PortMapping` and `HttpAuthUser`).

The declaration drives three things: the **Identity** section the generator renders on the task's
page, the name an unnamed task receives in the output and the
[`--json` stream](task-envelope.md#names-and-resource-addresses), and what
`docket export --resource` can select.

Three rules decide what is a key, and none of them is "the required fields":

- **The desired state is never a key.** `state:` says what you want done to the resource, not which
  resource it is. Including it would rename a task when its state changed, and would give
  `dokku_domains` four addresses for one app's domain list.
- **A required field can be a payload rather than an address.** `dokku_git_from_image`'s `image` is
  `required:"true"`; the task keys on `app`, because an app has one deploy source.
- **A key need not be required.** `dokku_certs` and `dokku_domains` have no `required:"true"` field
  at all - they key on the mutually exclusive `app` / `global` pair. An empty key is omitted from
  the address, which is what makes the two scopes render differently.

A field must never be both `identity:"key"` and `sensitive:"true"`. An identity key is rendered into
the task name, and every stream masks the name - so a sensitive key would render the whole address
as `dokku_config[app=***]`, which the reader can see but cannot type back into `--start-at-task` or
`docket export --resource`. `TestIdentityKeysAreNeverSensitive` enforces this.

Leave a collection untagged when it is an *attribute* of the resource rather than the set of items
the task manages: `dokku_storage_mount`'s `phases` narrows one mount, it is not a set of mounts. Its
`mounts` list is the set of mounts, and it carries the tag.

## Declaring objects

A task changes at most one app and one datastore service. Tag the fields that name them with
`serialize`, so tasks an [sdk caller](embedding.md#running-tasks-concurrently) applies at once on
the same object take turns:

- `serialize:"app"` on the `app` identity key. Every task with one carries it; a property or toggle
  task gets it from the shared `PropertyFields` or `ToggleFields`.
- `serialize:"service.type"` on the field holding the datastore type and `serialize:"service.name"`
  on the one holding the service's name. The role, not declaration order, decides which is which:
  `dokku_acl_service` names the instance `service` and the datastore `type`, and still shares its
  lock with `dokku_service_property` on the same service.

A server-level task (`dokku_plugin`, `dokku_network`) tags nothing. An object whose fields are all
empty - the app of an app-or-global task with `global: true` - names nothing and takes no lock.

When applying the task creates its object or can restart it, implement the optional
`ObjectEffects() ObjectEffects` method:

- `Creates` lists the kinds the task creates, and destroys in its `absent` state (`dokku_app`,
  `dokku_service_create`). Every other task on the object depends on it.
- `Restarts` lists the kinds the task can restart or redeploy, each with the conditions it does so
  under: `When` the recipe keys of bool fields any of which must be true (`dokku_config`'s
  `restart`), `Unless` the bool keys that stop it (`dokku_ps_scale`'s `skip_deploy`), and `States`
  the states it restarts in (`dokku_domains_toggle` restarts only on `present`). Leave them empty
  for a task that restarts whenever it applies. Count a deploy, a rebuild, a `ps:restart` and a
  container stop and start; a proxy reload is not a restart.

```go
// ObjectEffects reports that the task restarts the app when restart is true.
func (t LollipopTask) ObjectEffects() ObjectEffects {
  return ObjectEffects{
    Restarts: []ObjectRestart{{Kind: ObjectApp, Restart: Restart{When: []string{"restart"}}}},
  }
}
```

Check what the underlying command does in dokku's or the plugin's source, and name the command in
the method's comment. [`docket schema`](task-catalog.md#objects) publishes the declaration and the
generator renders it in an Objects section on the task's page. `TestSerializeTagsCoverEveryObject`
fails the build for an `app` key without the tag or a datastore task missing a role,
`TestObjectEffectsNameRealFields` for a condition naming a field the task does not have, and
`TestObjectEffectsPinned` for a declaration that differs from its pinned table.

## Declaring field constraints

`required:"true"` says a field is always needed. Two more field tags say when a field is needed or
refused, as a comma-separated list of the task's `state` choices:

- `required_when:"present,absent"` - the field must be set in these states.
- `forbidden_when:"clear"` - the field must not be set in these states.

Rules over several fields go in the optional `Constraints() []Constraint` method, on the task or on
an element struct such as `HttpAuthUser`. Each `Constraint` has a `Kind`, the `Fields` it covers, and
optionally the `States` it applies in (every state when empty; an element's rules use the task's
states):

- `ConstraintExactlyOneOf` - exactly one of `Fields` is set, such as `app` or `global`.
  `appOrGlobalConstraints` holds that one.
- `ConstraintAtLeastOneOf` - one or more of `Fields` is set.
- `ConstraintAllOrNoneOf` - `Fields` are set together or not at all.
- `ConstraintConflictsWith` - none of `Fields` is set while `Field` is.
- `ConstraintRequiredWith` - every one of `Fields` is set while `Field` is.

A field is set when it differs from its zero value and from its `default`: a nil pointer, an empty
list or map, and a value equal to the default all count as unset. Every name is a recipe key.

```go
type LollipopTask struct {
  App     string   `required:"true" identity:"key" serialize:"app" yaml:"app" description:"Name of the app"`
  Flavors []string `required:"false" required_when:"present" identity:"collection" ordered:"false" yaml:"flavors,omitempty" description:"Flavors to apply"`
  Wrapper string   `required:"false" yaml:"wrapper,omitempty" description:"Wrapper to use"`
  Stick   string   `required:"false" yaml:"stick,omitempty" description:"Stick to use"`
  State   State    `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the lollipop"`
}

// Constraints declares that a wrapper and a stick come as a pair.
func (t LollipopTask) Constraints() []Constraint {
  return []Constraint{
    {Kind: ConstraintAllOrNoneOf, Fields: []string{"wrapper", "stick"}, States: []State{StatePresent}},
  }
}
```

The declarations are what enforces them. `gatePlan` checks them before any probe, and a task's
`Validate()` starts with `checkConstraints(t)`, so `plan`, `apply` and `validate` all refuse the same
input. `docket validate` reports every constraint a body breaks, one `invalid_task_input` problem
each, rather than stopping at the first. [`docket schema`](task-catalog.md#constraints) publishes the
declarations, and the generator renders them on the task's page - a field note in the Parameters
table and a Constraints section - so do not repeat them in the `description`. The messages are
uniform: `'flavors' is required for state 'present'`, `'wrapper' and 'stick' must be set together
for state 'present'`.

`TestConstraintDeclarationsAreWellFormed` fails the build for a declaration naming a key the struct
does not have, a state the task does not offer, or a `required_when` on a field that is already
required or has a default. `TestEveryDeclaredConstraintIsEnforced` breaks every declaration of every
task and fails unless both `Validate()` and `Plan()` refuse it.

## Validating inputs

`required:"true"` field tags are enforced offline by `docket validate` (it reports
`missing_required_field`), and [field constraints](#declaring-field-constraints) by the declarations
themselves. A rule that depends on a value rather than on whether a field is set - an enum, a format,
a per-item check on a slice - has to live in code. Put it in `Validate() error` rather than inline in
`Plan()`, after the declared constraints:

```go
// Validate checks inputs without contacting the server.
func (t LollipopTask) Validate() error {
  if err := checkConstraints(t); err != nil {
    return err
  }
  for i, flavor := range t.Flavors {
    if strings.TrimSpace(flavor) == "" {
      return fmt.Errorf("'flavors' must not contain an empty flavor, got flavors[%d]", i)
    }
  }
  return nil
}

func (t LollipopTask) Plan(ctx context.Context) PlanResult {
  if r, ok := gatePlan(ctx, t); !ok { // checks the constraints, then runs Validate()
    return r
  }
  // ... probe and DispatchPlan
}
```

`Validate()` takes no context, and that is the point: it never contacts a server, so there is
nothing to cancel. `gatePlan` calls it once the declared constraints hold and before anything
probes, so `plan` and `apply` still report the error. Do not call it again from `Plan()`:
`TestEveryPlanChecksVersionGate` fails the build for a Plan that does. Because every Plan goes
through `gatePlan`, a rule added to a property task's `Validate()` - such as the
`cert-issuer-kind` enum on `dokku_scheduler_k3s_property` - is enforced by `planProperty` tasks too,
which call no `Validate()` of their own (#628).
Because it is a pure function of the struct - it must never call a probing or mutating dokku
command - `docket validate` calls the same method offline, once the declared constraints hold, and
surfaces any error as `invalid_task_input`, catching the mistake before a server is ever contacted.
Keep the error strings identical to what `Plan()` used to return so `plan`, `apply`, and `validate`
all read the same.

## Exporting a task

A task that declares itself exportable also implements one of two methods, and is listed in the
matching order slice in `internal/tasks/export.go`: `ExportApp(ctx, app)` plus an entry in `appExportOrder`
for app-scoped state, `ExportGlobal(ctx)` plus an entry in `globalExportOrder` for the rest. Both return
task bodies - the task's own struct, populated from the server - and the engine handles
vars-extraction and redaction afterwards. `TestExportSupportMatchesExportWiring` fails the build for
an exporter no order list names, and for a task that claims to export without implementing one.

Emit the desired `state` explicitly rather than leaning on the field's `default`, so the body is
plannable exactly as the exporter returns it.

Return the task's own struct by value, never a pointer to it or another task's type. The engine drops
any other body from the export with a warning, because the secret-lifting it applies and the
`sdk.As` accessor Go callers read bodies with both expect the value type.

When an exporter needs to say something about a particular resource - an asset it could not capture,
a resource it read back but cannot emit as a task the loader would accept - implement the reporting
form instead of logging: `ExportAppReport(ctx, app, warn)` or
`ExportGlobalReport(ctx, warn)`. The engine passes a `warn` callback wired to
`ExportReport.Warnings`, so the message is rendered and masked like every other export diagnostic
and reaches the operator with the run it belongs to.

The set those diagnostics mask against comes from the bodies the exporters return: as each one is
processed, whatever it declares sensitive - a `sensitive:"true"` field, a `SensitiveValues()`
override, or a property in a family marked `Sensitive` - is collected for masking, in every mode,
including `--redact`, where the value is written nowhere but was still read off the server. A task
that declares its secrets properly therefore needs nothing further to keep them out of its own
export warnings.

Implement the reporting form *alongside* the plain one, never instead of it. The engine asserts
`AppExporter` / `GlobalExporter` before it looks for the reporter, so a task carrying only the
reporting method exports nothing at all; `TestExportReportersAlsoImplementTheBaseExporter` catches
that. The usual shape is a shared private method with two thin entry points, as
`MaintenanceCustomPageTask` and `SchedulerK3sProfileTask` do:

```go
func (t LollipopTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
  return t.exportGlobal(ctx, func(string) {})
}

func (t LollipopTask) ExportGlobalReport(ctx context.Context, warn func(msg string)) ([]interface{}, error) {
  return t.exportGlobal(ctx, warn)
}
```

Warn *and leave the resource out* when the body you would emit is one `docket validate` refuses.
Validation rejects the whole recipe rather than the single task, so emitting it costs the operator
every other resource on the server. `dokku_scheduler_k3s_profile` is the worked example: dokku
accepts profile names its own node-sysctls helm release cannot be named after, so the exporter runs
each candidate body through its own `Validate()` - the same method `docket validate` calls - and
reports the ones it drops, naming the resource and the remedy.

## Toggle and property tasks

Most dokku plugins expose one of two shapes, and docket has a shared `Plan()` helper for each. When
your task fits one, reach for the helper instead of hand-writing `DispatchPlan` - the task becomes
mostly declaration, and the idempotency probing is handled for you.

- A **toggle** turns a plugin on or off for an app - for example `checks`, `proxy`, `domains`, and
  `maintenance`.
- A **property** stores named key/value settings you set or clear - for example `nginx`, `builder`,
  and `git`.

For both shapes the `State` field accepts only `present` and `absent`, declared with the same tag:
`default:"present" options:"present,absent"`.

### Toggle tasks

A toggle task delegates `Plan()` to `planToggle`, passing the plugin's enable and disable
subcommands and a *probe* - a function that reports whether the plugin is currently enabled. The
probe is what keeps the task idempotent: when it reports the plugin is already in the desired
position, the task is in sync and nothing runs. `present` means enabled, `absent` means disabled. A
toggle always targets an app; there is no global scope.

The two recipe keys a toggle task accepts - `app` and `state` - are declared once, in `ToggleFields`.
Do not restate them: name your task as a defined type over it, so a cross-cutting field change lands
in one place rather than in every toggle task.

```go
type ChecksToggleTask ToggleFields

// The probe reports the current position. A non-nil error (or a nil probe) is
// treated as drift, so the enable/disable command runs anyway.
func checksEnabled(ctx context.Context, tc ToggleContext) (bool, error) {
  // dokku checks:report <tc.App> --format json
  // ... return true when nothing is disabled
}

func (t ChecksToggleTask) Plan(ctx context.Context) PlanResult {
  if r, ok := gatePlan(ctx, t); !ok {
    return r
  }
  // t is what an in-sync plan observes. The closure converts the shared field
  // set back to the task's own type, so a drift plan's Observed is a
  // ChecksToggleTask too.
  return planToggle(ctx, t, func(f ToggleFields) Task { return ChecksToggleTask(f) }, t.State, t.App, "checks:enable", "checks:disable", checksEnabled)
}
```

A toggle task addressed differently - one that needs a `global` scope, say - declares its own fields
and goes on the `toggleTasksWithOwnFields` allowlist with the reason, the way property tasks use
`propertyTasksWithOwnFields`. Nothing is on it today.
`TestToggleTasksDeclareTheSharedFields` fails the build for a task that restates the shared field set
without being on it, and it decides which tasks are toggles by whether their `Plan()` reaches
`planToggle` rather than by their name - `dokku_maintenance` is a toggle that is not spelled like one.

### Property tasks

A property task declares a `PropertyTable` and delegates `Plan()` to `planProperty`. The table is
the task's single source of truth: the plugin's `:set` subcommand, plus every property the task
manages and, for each, the JSON keys that `dokku <plugin>:report --format json` emits in per-app and
global scope. An empty string for a scope means the property is not supported there, and
`planProperty` rejects that scope at plan time. `present` sets the property and requires a `value`;
`absent` clears it and must not have one - the helper enforces both.

The five recipe keys a property task accepts - `app`, `global`, `property`, `value`, `state` - are
declared once, in `PropertyFields`. Do not restate them: name your task as a defined type over it,
so a cross-cutting field change lands in one place rather than in every property task.

```go
type NginxPropertyTask PropertyFields

// Maps each property to the report JSON keys per scope. "" means unsupported.
var nginxPropertyTable = PropertyTable{
  Subcommand: "nginx:set",
  Keys: map[string]PropertyKeys{
    "client-max-body-size": {PerApp: "client-max-body-size", Global: "global-client-max-body-size"},
    "proxy-read-timeout":   {PerApp: "proxy-read-timeout", Global: "global-proxy-read-timeout"},
    // ...
  },
}

// propertyTable returns the property schema this task manages.
func (t NginxPropertyTask) propertyTable() PropertyTable {
  return nginxPropertyTable
}

// Constraints declares that the property is set for an app or globally.
func (t NginxPropertyTask) Constraints() []Constraint {
  return appOrGlobalConstraints
}

func (t NginxPropertyTask) Validate() error {
  return validatePropertyInput(t, t.Global, t.Property)
}

func (t NginxPropertyTask) Plan(ctx context.Context) PlanResult {
  if r, ok := gatePlan(ctx, t); !ok {
    return r
  }
  return planProperty(ctx, t, func(f PropertyFields) Task { return NginxPropertyTask(f) }, t.State, t.App, t.Global, t.Property, t.Value)
}
```

The shared helpers take the task rather than a subcommand and a map, so there is no way to reach
them without declaring the table, and no way to validate against one table while publishing
another. That matters because the table is not only an implementation detail: it is the real schema
of the task's otherwise free-form `property` field, and it is what fills the Properties section on
the task's reference page and the `property_schema` key in
[`docket schema`](task-catalog.md#property-tasks). The property exporters take the task the same
way, and `TestEveryPropertyTaskDeclaresPropertyTable` fails the build for a `*_property` task that
declares no table and is not explicitly exempt.

Use `SensitivePropertyFields` instead when the plugin's property values can be credentials, as
letsencrypt's `dns-provider-*` ones are. It is `PropertyFields` with `value` tagged
`sensitive:"true"`, which masks every value the task echoes, benign ones included - preferable to
leaking a secret because the per-property judgement was wrong. Masking and export are separate
questions: export decides what to lift into a vars file from the property's `PropertyKeys` entry, so
a benign value stays readable in an exported recipe.

A property task addressed differently declares its own fields and goes on the
`propertyTasksWithOwnFields` allowlist with the reason, the way `dokku_scheduler_docker_local_property`
(per-app only, so `app` is required and there is no `global`) and `dokku_service_property` (keyed by
`service` plus `name`) do. `TestPropertyTasksDeclareTheSharedFields` fails the build for a
`*_property` task that restates the shared field set without being on it.

Keep the table in sync with the plugin's `:report` output - that mapping is how `plan` and `apply`
detect drift without mutating. Some plugins take a dynamic family of properties whose names cannot
be enumerated, such as the `dns-provider-<ENV_VAR>` credentials letsencrypt and traefik accept.
Those are declared in `dynamicPropertyFamilies` in `internal/tasks/properties.go`, which is what lets
validation accept a name the table has never heard of, and is published to consumers so a linter
does not reject a legal recipe. How they plan depends on the plugin:

- The plugin reports the family (letsencrypt on 0.25.0+ and traefik on dokku 0.38.27+ emit a row per
  set property): declare it `Probeable`, and the scope keys are synthesized so the property probes
  like any mapped one. Because the row only exists once the property has a value, an absent row reads
  as unset. Note the minimum plugin version in `MinimumVersions()`.
- The plugin does not report it: leave `Probeable` false. The property skips probing and is applied
  unconditionally, and the task is `ProbePartial` with a caveat naming the family. No family declared
  today is in this state, so `runUnprobedSet`/`runUnprobedUnset` have no live caller - they are the
  other half of the contract, kept for the next plugin that takes a family it does not report.

Every family also declares `Scopes`, the same `app`/`global` vocabulary an enumerable property states
by leaving a `PropertyKeys` half empty. A mapped property has an entry to say it with and a dynamic
one does not, so the family says it instead, and `validateProperty` holds a member to it: traefik's
`traefik:set` refuses a `dns-provider-*` key outside `--global`, so an app-scoped one is rejected with
the same sentence a mapped global-only property gets rather than probing a row that cannot exist.

Mark the family `Sensitive` whenever its values are credentials, whichever of those two it is.
Whether a value is a secret and whether docket can read it back are separate questions, and
answering the first by asking the second is how traefik's `dns-provider-*` credentials reached argv
in cleartext (#457). `Sensitive` registers the desired value with the masker before it is echoed;
on a `Probeable` family it also masks the value read back, so it never reaches a drift reason.

The mirror image is a family the task refuses because another task owns it. Declare it under
`Rejected` rather than guarding inside `Plan()`:

```go
var schedulerK3sPropertyTable = PropertyTable{
  Subcommand: "scheduler-k3s:set",
  Rejected: []RejectedPropertyFamily{
    {
      Prefix:      "chart.",
      Replacement: "dokku_scheduler_k3s_chart",
      Reason:      "the scheduler-k3s:set path for chart values is deprecated in dokku",
    },
  },
  Keys: map[string]PropertyKeys{ /* ... */ },
}
```

A member is rejected with `<prefix>* properties are managed by <replacement>; <reason>`, checked
before scoping and before the key map. Leaving it out of `Keys` is not enough on its own: the user
who wrote `chart.traefik.replicas` meant it, and answering with the list of names that *are*
supported sends them looking for something that will never be on it. All three fields are required,
and `Replacement` has to name a registered task.

Declaring it on the table rather than in `Plan()` is what keeps the message honest. The constraint
check reports a rejected member alone, ahead of every declared constraint, and
`validatePropertyName` - which every property task's `Validate()` reaches through
`validatePropertyInput`, and which `gatePlan` and `planProperty` therefore both run - checks it again, so `plan`, `apply` and `validate` cannot drift apart - which is exactly what happened
while the `chart.*` guard lived in `Plan()` alone and `docket validate` reported the generic
unsupported-name error instead (#458). The family is published as `property_schema.rejected` too, so
a consumer validating a recipe offline can give the same answer.

## Authoritative collection tasks

A task whose collection field carries `identity:"collection"` owns that collection outright - the
Identity section calls the collection the resource, not part of its key. Such a task can offer two
states beyond `present` and `absent`:

- **`set`** declares the complete collection. Every entry the recipe names is written, and every
  entry stored on the server that it does not name is removed. An empty collection under `set`
  declares that the collection is empty, and plans exactly what `clear` plans.
- **`clear`** empties the collection. It takes no entries at all.

Every collection task offers all four states, so a consumer that owns a whole collection can map
create and update to `set` and destroy to `clear` without special cases. `dokku_ps_scale` is the
one exception: it offers `present`, `set` and `clear` but no `absent`, because an app always runs
some count of each process type. Its `clear` resets the app to the formation dokku gives a new app
(`ps:scale --clear`) rather than emptying it.

`present` and `absent` stay additive alongside them: `present` writes the named entries and leaves
the rest, `absent` removes the named entries and leaves the rest. That is the whole point of the
split - an additive task cannot express "and nothing else", so an entry a recipe stopped declaring
survives every apply.

Both states must route through a **single server-side whole-set command**, not a loop over the
entries. `dokku_domains` uses `domains:set` and `domains:clear`, `dokku_ports` uses `ports:set` and
`ports:clear`, the scheduler-k3s map tasks use `<plugin>:set --replace` and `<plugin>:clear`, and
the dokku-acl tasks use `acl:set-users` and `acl:set-service-users`, called with no users to clear.
Emitting one command per entry would leave the collection carrying a mixture of the old and new sets
when a run fails partway, which is the outcome these states exist to rule out.

Where dokku has no whole-set command, the task still gets the states, but its reference page says
the work is not atomic. There are three such cases today:

- `dokku_scheduler_k3s_chart` writes one `scheduler-k3s:charts:set` per changed value, through
  `planPairsReplacePerKey` and `planPairsClearPerKey`, until dokku grows a whole-chart form (#625).
- `dokku_http_auth_user` clears with one `http-auth:remove-user` per user, since the plugin has
  no command that removes them all. Its `set` is one `http-auth:import-users --replace` only when
  every user is given by hash.
- `dokku_service_expose` sets with `<service>:unexpose` and then `<service>:expose`, because
  dokku refuses to expose a service that is already exposed.

Two rules go with them, and every task carrying the states shares both:

- an empty collection under `set` plans as `clear`. Dokku's whole-set commands reject an empty
  list, so the `set` branch hands an empty collection to the task's clear plan through
  `planSetAsClear` in `internal/tasks/dispatch.go`, which keeps the apply reporting `set`, the
  state the recipe declared. `TestEmptySetPlansAsClear` holds every task offering both states to
  it.
- a collection supplied under `clear` is an error rather than something to ignore, since the command
  cannot act on it and silently discarding it would read as a removal that never happened.

`planPairsReplace` and `planPairsClear` in `internal/tasks/pairs.go` implement both states for a
`map[string]string` collection: pass the desired map, a probe, and a builder for the one command,
and they handle the two-directional diff, the mutation lines and the in-sync case. The list-valued
tasks build their commands through `dokkuArgsInputs` / `applyDokkuArgs` in `internal/tasks/domains_task.go`
instead.

Both states itemize the replacement they work out to in `PlanResult.Mutations`, and those lines
must come out in the same order every run - a probe hands its result back as a map, and a
`Mutations` slice formatted straight out of a map range makes two plans of an unchanged server
disagree, which is churn for anyone diffing the output or the [`--json`](json-output.md) stream.
Sort both sides of the diff, the entries being added as well as the ones being removed, with
`sortedSetKeys` (a `map[string]bool` set) or `sortedPairKeys` (a `map[string]string`);
`sortedPortMappings` is the same helper for `dokku_ports`. Sorting the adds costs nothing here
because the command carries the recipe's list verbatim whatever the mutation lines say - which
is not true of the additive `present` and `absent` states, where the itemized list and the
command are built from the same slice and the declared order is the order the work happens in.
An ordered collection is the exception to sorting: `dokku_buildpacks` lists its removals in the
precedence order the server reported, which is already stable and is the order that means
something. The same rule holds beyond `Mutations`: anything a map range builds that reaches the
user - a reason, an error naming one of several bad keys, an observed value - walks sorted keys
or an already-ordered source, never the map itself.

`docket export` emits an authoritative collection as `state: set`, so re-applying an export
reproduces the exact collection rather than merging into whatever the target already holds. The
tasks that gained the states later keep exporting `state: present`. For `dokku_buildpacks` and
`dokku_service_expose` that already replaces the whole list. For `dokku_resource_limit`,
`dokku_resource_reserve`, `dokku_scheduler_k3s_chart` and `dokku_http_auth_user`, `present` is
additive, so an exported recipe does not delete values that exist only on the destination.

Sometimes the server stores entries in the collection that belong to something other than the
recipe, and a whole-set command would remove them too. `dokku_config` is the case in point. The
app's env file also holds the `<ALIAS>_URL` keys a service link wrote, `NO_VHOST` from
`domains:disable`, and the git rev-env-var key dokku rewrites on every build. `config:import
--replace` would wipe all of them. `configKeptKeys` in `internal/tasks/config_task.go` draws that line. `set`
passes the kept keys through the one `config:import` with their current values, and `clear`
removes only the other keys, in one `config:unset`, because `config:clear` would take the kept keys
with it. Export leaves out the same keys, so a round trip agrees with itself. A task with an
ownership line like this should draw it in one helper that `set`, `clear` and the exporter all call,
and offer a field (config's `preserve`) for keys the task cannot recognise on its own.

## Regenerating the task docs

The per-task pages under [`docs/tasks/`](tasks/README.md) are generated from each task's `Doc()`,
`examples()`, `ExportSupport()`, `ProbeSupport()`, `MinimumVersions()` and `propertyTable()`
methods plus its struct field tags - they are not hand-edited. Each page carries a Synopsis (from
`Doc()`), a Requirements section (the Dokku floor, then each plugin line), a Runner requirements
section (when a field is tagged `runner_file:"true"`), Export support and Probe support sections, an
Identity section, a Parameters table (reflected from the field tags), a Properties table (for a task
with a `propertyTable()`), the examples, and a shared Return Values table. After adding or changing a
task, regenerate them:

```bash
make generate
```

This runs `go generate generate/docs.go`, which writes one `docs/tasks/<task>.md` per registered
task plus the `docs/tasks/README.md` index, and `go generate ./sdk/`, which rewrites
`sdk/types_gen.go` so the [public `sdk` package](embedding.md) can name the new task type. Commit
the regenerated files alongside your code - `TestGeneratedDocsAreCurrent` and
`TestGeneratedSDKIsCurrent` fail the build if you forget, and print the diff.

CI also runs `make lint` (`golangci-lint` with the linters pinned in `.golangci.yml`). Run the same
locally before sending a PR.

The generator renders those pages from `tasks.Catalog()`, the same description
[`docket schema`](task-catalog.md) emits, so a declaration you add shows up in both or in neither.

Because the examples are published as-is, they are also tested. `TestAllTaskExamplesValidate`
(`internal/tasks/main_test.go`) decodes every example offline, applies the field defaults, and runs the
task's `Validate()`, so a snippet that would fail `docket validate` cannot ship - it runs as part of
`make test`. `TestIntegrationTaskExamples` (`internal/tasks/example_integration_test.go`) then applies every
example against a live Dokku under `make test-integration`. Write examples that are actually
runnable: reference real resources (a reachable image or archive, not a placeholder URL), and if a
task needs a prerequisite the shared placeholder apps do not provide - a backing service, a deployed
app, a plugin, a cluster - or its documented value cannot apply verbatim (a secret or inline PEM),
declare it in the driver's `exampleIntegrationPolicy` rather than leaving the example unapplied.

## Opening a pull request

Every pull request needs one of these labels: `breaking-change`, `enhancement`, `bug`, `refactor`,
`documentation`, `tests`, or `dependencies`. The label decides which section of the release notes
the pull request goes in, and CI fails until one is set. Use `breaking-change` for a change that
breaks existing recipes or the `sdk` package.

## See also

- [Tasks](tasks/README.md) - the generated reference for every task
- [Task catalog](task-catalog.md) - the same declarations, published for tooling
- [Task envelope](task-envelope.md) - the cross-cutting keys every task supports
- [Command reference](command-reference.md) - how `plan` and `apply` consume `Plan()`
