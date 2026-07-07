# Writing tasks

This page is for contributors adding new task types to docket. If you only want to *use* tasks in a
recipe, see the [task reference](tasks/README.md) instead.

Tasks closely follow the modules available in
[`ansible-dokku`](https://github.com/dokku/ansible-dokku), so that existing task lists migrate with
minimal changes. docket adds a few of its own tasks specific to this package. A task has a name and
an execution context that maps to a single module; its fields can be templated from recipe
[inputs](inputs.md) and any function [sigil](https://github.com/gliderlabs/sigil) exposes.

## The Plan / Execute model

Every task implements two methods. `Plan()` is the canonical one: it probes the live server once,
computes the difference between the current and desired state, and returns a `PlanResult`. When the
server is not already in the desired state, `Plan()` embeds an `apply` closure that performs the
mutation. `Execute()` is always just `ExecutePlan(t.Plan())` - the shared `ExecutePlan` helper
handles the in-sync, error, and apply cases uniformly, so the mutation logic lives in exactly one
place per task.

This split is why `plan` and `apply` agree: both call the same `Plan()`. `apply` reuses the probe
to decide whether to mutate, which is what makes back-to-back applies no-ops.

## Adding a new task

Create `tasks/${TASK_NAME}_task.go`, where the task name is `lower_underscore_case`. For a task
named `lollipop`, `tasks/lollipop_task.go` would contain:

```go
package main

type LollipopTask struct {
  App   string `required:"true" yaml:"app" description:"Name of the app"`
  State State  `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the lollipop"`
}

func (t LollipopTask) Plan() PlanResult {
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
        apply: func() TaskOutputState {
          // Run the underlying dokku command. Return Changed=true on success.
          return TaskOutputState{Changed: true, State: StatePresent}
        },
      }
    },
    "absent": func() PlanResult { /* ... */ },
  })
}

func (t LollipopTask) Execute() TaskOutputState {
  return ExecutePlan(t.Plan())
}

func init() {
  RegisterTask(&LollipopTask{})
}
```

A few conventions to follow:

- The struct holds the fields the task needs. The only required field is `State`, the desired state;
  everything else is specific to the task.
- Give every field a `description:"..."` tag. The docs generator reads it (along with `required`,
  `default`, and `options`) to build the task's Parameters table, so a field without one renders an
  empty description cell. Add `,omitempty` to the `yaml` tag of optional fields so example YAML stays
  clean, and use `required:"false"` whenever a field has a `default` (a defaulted field is never
  actually required).
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
- When a task has conditional or semantic input rules that a `required:"true"` tag cannot express -
  a list that must be non-empty only when `state: present`, mutually-exclusive fields, an enum, a
  per-item requirement on a slice field - put them in the optional `Validate() error` method (the
  `InputValidator` interface) and call it as the first line of `Plan()`. See below.

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

func (t LollipopTask) Plan() PlanResult {
  if err := t.Validate(); err != nil {
    return planErr(err)
  }
  // ... probe and DispatchPlan
}
```

`Plan()` calls `Validate()` before it probes, so `plan` and `apply` still report the error. Because
`Validate()` is a pure function of the struct - it must never call a probing or mutating dokku
command - `docket validate` calls the same method offline and surfaces any error as
`invalid_task_input`, catching the mistake before a server is ever contacted. Keep the error strings
identical to what `Plan()` used to return so `plan`, `apply`, and `validate` all read the same.

## Toggle and property tasks

Most dokku plugins expose one of two shapes, and docket has a shared `Plan()` helper for each. When
your task fits one, reach for the helper instead of hand-writing `DispatchPlan` - the task becomes
mostly declaration, and the idempotency probing is handled for you.

- A **toggle** turns a plugin on or off for an app or globally - for example `checks`, `proxy`, and
  `domains`.
- A **property** stores named key/value settings you set or clear - for example `nginx`, `builder`,
  and `git`.

For both shapes the `State` field accepts only `present` and `absent`, declared with the same tag:
`default:"present" options:"present,absent"`.

### Toggle tasks

A toggle task delegates `Plan()` to `planToggle`, passing the plugin's enable and disable
subcommands and a *probe* - a function that reports whether the plugin is currently enabled. The
probe is what keeps the task idempotent: when it reports the plugin is already in the desired
position, the task is in sync and nothing runs. `present` means enabled, `absent` means disabled.
The `allowGlobal` argument controls whether `global: true` is accepted; pass `false` for plugins
that are app-only.

```go
type ChecksToggleTask struct {
  App    string `required:"true" yaml:"app"`
  Global bool   `required:"false" yaml:"global,omitempty"`
  State  State  `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// The probe reports the current position. A non-nil error (or a nil probe) is
// treated as drift, so the enable/disable command runs anyway.
func checksEnabled(ctx ToggleContext) (bool, error) {
  // dokku --quiet checks:report <app> --checks-disabled
  // ... return true when nothing is disabled
}

func (t ChecksToggleTask) Plan() PlanResult {
  return planToggle(t.State, t.App, t.Global, false, "checks:enable", "checks:disable", checksEnabled)
}
```

### Property tasks

A property task delegates `Plan()` to `planProperty`, passing the plugin's `:set` subcommand and a
`PropertyKeys` map. That map is the task's source of truth: it lists every property the task manages
and, for each, the JSON keys that `dokku <plugin>:report --format json` emits in per-app and global
scope. An empty string for a scope means the property is not supported there, and `planProperty`
rejects that scope at plan time. `present` sets the property and requires a `value`; `absent` clears
it and must not have one - the helper enforces both.

```go
type NginxPropertyTask struct {
  App      string `required:"false" yaml:"app"`
  Global   bool   `required:"false" yaml:"global,omitempty"`
  Property string `required:"true" yaml:"property"`
  Value    string `required:"false" yaml:"value,omitempty"`
  State    State  `required:"true" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// Maps each property to the report JSON keys per scope. "" means unsupported.
var nginxPropertyKeys = map[string]PropertyKeys{
  "client-max-body-size": {PerApp: "client-max-body-size", Global: "global-client-max-body-size"},
  "proxy-read-timeout":   {PerApp: "proxy-read-timeout", Global: "global-proxy-read-timeout"},
  // ...
}

func (t NginxPropertyTask) Plan() PlanResult {
  return planProperty(t.State, t.App, t.Global, t.Property, t.Value, "nginx:set", nginxPropertyKeys)
}
```

Keep the `PropertyKeys` map in sync with the plugin's `:report` output - that mapping is how `plan`
and `apply` detect drift without mutating. A property whose report key only appears after it is set
(a dynamic family such as letsencrypt's `dns-provider-*`) skips probing and is applied
unconditionally; those are recognized by `isDynamicProperty` in `tasks/properties.go`.

## Regenerating the task docs

The per-task pages under [`docs/tasks/`](tasks/README.md) are generated from each task's `Doc()`,
`Examples()`, and optional `Requirements()` methods plus its struct field tags - they are not
hand-edited. Each page carries a Synopsis (from `Doc()`), a Requirements section (when the task
implements `Requirements()`), a Parameters table (reflected from the field tags), the examples, and
a shared Return Values table. After adding or changing a task, regenerate them:

```bash
make docs
```

This runs `go generate generate/docs.go`, which writes one `docs/tasks/<task>.md` per registered
task plus the `docs/tasks/README.md` index. Commit the regenerated files alongside your code.

## See also

- [Tasks](tasks/README.md) - the generated reference for every task
- [Task envelope](task-envelope.md) - the cross-cutting keys every task supports
- [Command reference](command-reference.md) - how `plan` and `apply` consume `Plan()`
