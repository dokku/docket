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
mutation. `Execute(ctx)` is always just `ExecutePlan(ctx, t.Plan(ctx))` - the shared `ExecutePlan`
helper handles the in-sync, error, and apply cases uniformly, so the mutation logic lives in exactly
one place per task.

This split is why `plan` and `apply` agree: both call the same `Plan()`. `apply` reuses the probe
to decide whether to mutate, which is what makes back-to-back applies no-ops.

Both take a `context.Context`, and so does every helper below them. Pass it to each
`subprocess.CallExecCommand` / `subprocess.Probe` call rather than reaching for
`context.Background()`: it is what carries cancellation, so an interrupt or a caller's deadline
reaches a task that is mid-command instead of waiting for the command to finish. The `apply` closure
takes one rather than capturing the one `Plan()` ran under, because `ExecutePlan` invokes it
separately and hands it the caller's current context.

## Adding a new task

Create `tasks/${TASK_NAME}_task.go`, where the task name is `lower_underscore_case`. For a task
named `lollipop`, `tasks/lollipop_task.go` would contain:

```go
package tasks

import "context"

type LollipopTask struct {
  App     string   `required:"true" identity:"key" yaml:"app" description:"Name of the app"`
  Flavors []string `required:"false" identity:"collection" yaml:"flavors,omitempty" description:"Flavors to apply"`
  State   State    `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the lollipop"`
}

func (t LollipopTask) Plan(ctx context.Context) PlanResult {
  // DispatchPlan keeps its argument-free branch signature; the closures capture
  // ctx from the enclosing Plan.
  return DispatchPlan(t.State, map[State]func() PlanResult{
    "present": func() PlanResult {
      // Probe the server once, decide whether to mutate.
      if /* already in desired state */ {
        return PlanResult{InSync: true, Status: PlanStatusOK}
      }
      return PlanResult{
        InSync:    false,
        Status:    PlanStatusCreate, // or PlanStatusModify, PlanStatusDestroy
        Reason:    "...",
        Mutations: []string{"create lollipop"},
        apply: func(ctx context.Context) TaskOutputState {
          // Run the underlying dokku command. Return Changed=true on success.
          return TaskOutputState{Changed: true, State: StatePresent}
        },
      }
    },
    "absent": func() PlanResult { /* ... */ },
  })
}

func (t LollipopTask) Execute(ctx context.Context) TaskOutputState {
  return ExecutePlan(ctx, t.Plan(ctx))
}

func init() {
  RegisterTask(&LollipopTask{})
}
```

A few conventions to follow:

- The struct holds the fields the task needs. The only required field is `State`, the desired state;
  everything else is specific to the task.
- Give every field a `description:"..."` tag. The docs generator reads it (along with `required`,
  `default`, `options`, and `sensitive`) to build the task's Parameters table, so a field without one
  renders an empty description cell. Those same tags are what
  [`docket schema`](task-catalog.md) publishes, so a missing description is a hole in the
  machine-readable catalog as well as in the docs. Add `,omitempty` to the `yaml` tag of optional
  fields so example YAML stays clean, and use `required:"false"` whenever a field has a `default`
  (a defaulted field is never actually required).
- For a task that performs several atomic changes in one call (such as setting multiple config
  keys), populate `PlanResult.Mutations` with one entry per change, so `plan` can itemize the diff.
- `DispatchPlan` and `DispatchState` set `DesiredState` on the result automatically.
- `init()` registers the task with `RegisterTask`, which makes it usable in a recipe.
- When a task depends on a dokku plugin that is not part of dokku core (for example `dokku-acl` or
  `dokku-letsencrypt`), implement the optional `Requirements() []string` method. The generator
  renders the returned entries in a Requirements section on the task's page; tasks without the
  method simply omit the section.
- When a task type is deprecated (typically because the underlying dokku subcommand was deprecated
  or a richer replacement task exists), implement the optional `Deprecation() string` method.
  The generator renders the returned message in a Deprecated admonition on the task's page and
  appends `(deprecated)` to the task's index entry; `apply --list-tasks` marks it the same way;
  `apply` and `plan` emit a one-time `warning` line above each deprecated task's result line.
  Keep the message short and name the replacement, e.g.
  `"use dokku_storage_entry instead; storage:ensure-directory has been deprecated"`.
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
- When a task has conditional or semantic input rules that a `required:"true"` tag cannot express -
  a list that must be non-empty only when `state: present`, mutually-exclusive fields, an enum, a
  per-item requirement on a slice field - put them in the optional `Validate() error` method (the
  `InputValidator` interface) and call it as the first line of `Plan()`. See below.

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
the task manages: `dokku_storage_mount`'s `phases` narrows one mount, it is not a set of mounts.

## Validating inputs

`required:"true"` field tags are enforced offline by `docket validate` (it reports
`missing_required_field`), but they can only express "this scalar field must be present". Anything
conditional - non-empty-when-present, mutually-exclusive fields, enums, per-item checks on a slice -
has to live in code. Put it in `Validate() error` rather than inline in `Plan()`:

```go
// Validate checks inputs without contacting the server.
func (t LollipopTask) Validate() error {
  if t.State == StatePresent && len(t.Flavors) == 0 {
    return fmt.Errorf("'flavors' must not be empty for state 'present'")
  }
  return nil
}

func (t LollipopTask) Plan(ctx context.Context) PlanResult {
  if err := t.Validate(); err != nil {
    return planErr(err)
  }
  // ... probe and DispatchPlan
}
```

`Validate()` takes no context, and that is the point: it never contacts a server, so there is
nothing to cancel. `Plan()` calls it before it probes, so `plan` and `apply` still report the error.
Because it is a pure function of the struct - it must never call a probing or mutating dokku
command - `docket validate` calls the same method offline and surfaces any error as
`invalid_task_input`, catching the mistake before a server is ever contacted. Keep the error strings
identical to what `Plan()` used to return so `plan`, `apply`, and `validate` all read the same.

## Exporting a task

A task that declares itself exportable also implements one of two methods, and is listed in the
matching order slice in `tasks/export.go`: `ExportApp(ctx, app)` plus an entry in `appExportOrder`
for app-scoped state, `ExportGlobal(ctx)` plus an entry in `globalExportOrder` for the rest. Both return
task bodies - the task's own struct, populated from the server - and the engine handles
vars-extraction and redaction afterwards. `TestExportSupportMatchesExportWiring` fails the build for
an exporter no order list names, and for a task that claims to export without implementing one.

Emit the desired `state` explicitly rather than leaning on the field's `default`, so the body is
plannable exactly as the exporter returns it.

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
  return planToggle(ctx, t.State, t.App, "checks:enable", "checks:disable", checksEnabled)
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

// PropertyTable returns the property schema this task manages.
func (t NginxPropertyTask) PropertyTable() PropertyTable {
  return nginxPropertyTable
}

func (t NginxPropertyTask) Validate() error {
  return validatePropertyInput(t, t.State, t.App, t.Global, t.Property, t.Value)
}

func (t NginxPropertyTask) Plan(ctx context.Context) PlanResult {
  return planProperty(ctx, t, t.State, t.App, t.Global, t.Property, t.Value)
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
Those are declared in `dynamicPropertyFamilies` in `tasks/properties.go`, which is what lets
validation accept a name the table has never heard of, and is published to consumers so a linter
does not reject a legal recipe. How they plan depends on the plugin:

- The plugin reports the family (letsencrypt on 0.25.0+ emits a row per set property): declare it
  `Probeable`, and the scope keys are synthesized so the property probes like any mapped one.
  Because the row only exists once the property has a value, an absent row reads as unset. Note the
  minimum plugin version in `Requirements()`.
- The plugin does not report it: leave `Probeable` false. The property skips probing and is applied
  unconditionally, and the task is `ProbePartial` with a caveat naming the family.

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

Declaring it on the table rather than in `Plan()` is what keeps the message honest.
`validatePropertyInput` is the one function `planProperty` and every property task's `Validate()`
both call, so `plan`, `apply` and `validate` cannot drift apart - which is exactly what happened
while the `chart.*` guard lived in `Plan()` alone and `docket validate` reported the generic
unsupported-name error instead (#458). The family is published as `property_schema.rejected` too, so
a consumer validating a recipe offline can give the same answer.

## Regenerating the task docs

The per-task pages under [`docs/tasks/`](tasks/README.md) are generated from each task's `Doc()`,
`Examples()`, `ExportSupport()`, `ProbeSupport()`, optional `Requirements()` and `PropertyTable()`
methods plus its struct field tags - they are not hand-edited. Each page carries a Synopsis (from
`Doc()`), a Requirements section (when the task implements `Requirements()`), Export support and
Probe support sections, an Identity section, a Parameters table (reflected from the field tags), a
Properties table (for a task with a `PropertyTable()`), the examples, and a shared Return Values
table. After adding or changing a task, regenerate them:

```bash
make docs
```

This runs `go generate generate/docs.go`, which writes one `docs/tasks/<task>.md` per registered
task plus the `docs/tasks/README.md` index. Commit the regenerated files alongside your code -
`TestGeneratedDocsAreCurrent` fails the build if you forget, and prints the diff.

The generator renders those pages from `tasks.Catalog()`, the same description
[`docket schema`](task-catalog.md) emits, so a declaration you add shows up in both or in neither.

Because the examples are published as-is, they are also tested. `TestAllTaskExamplesValidate`
(`tasks/main_test.go`) decodes every example offline, applies the field defaults, and runs the
task's `Validate()`, so a snippet that would fail `docket validate` cannot ship - it runs as part of
`make test`. `TestIntegrationTaskExamples` (`tasks/example_integration_test.go`) then applies every
example against a live Dokku under `make test-integration`. Write examples that are actually
runnable: reference real resources (a reachable image or archive, not a placeholder URL), and if a
task needs a prerequisite the shared placeholder apps do not provide - a backing service, a deployed
app, a plugin, a cluster - or its documented value cannot apply verbatim (a secret or inline PEM),
declare it in the driver's `exampleIntegrationPolicy` rather than leaving the example unapplied.

## See also

- [Tasks](tasks/README.md) - the generated reference for every task
- [Task catalog](task-catalog.md) - the same declarations, published for tooling
- [Task envelope](task-envelope.md) - the cross-cutting keys every task supports
- [Command reference](command-reference.md) - how `plan` and `apply` consume `Plan()`
