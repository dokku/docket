# Embedding docket in Go

docket's engine is a Go package, not only a CLI. This page covers driving it from Go: reading a
server back as structured data, building tasks, and the per-invocation state every call needs.

Import path is `github.com/dokku/docket/sdk`. Everything else in the module lives under `internal/`
and cannot be imported from outside it.

> [!NOTE]
> Before the `sdk` package, the engine was imported from `github.com/dokku/docket/tasks` and
> `github.com/dokku/docket/subprocess`. Those packages are now internal. Every symbol this page
> documents has the same name in `sdk`, so moving over is a change of import path and package
> qualifier: `tasks.NewTask` and `subprocess.NewSession` both become `sdk.NewTask` and
> `sdk.NewSession`.

## The run context

Everything takes a `context.Context`, and that context carries the state a run needs. Nothing is
read from process globals, so two runs in one process can target different servers, mask different
values, and be cancelled independently.

Build that context from a `sdk.Session`. The session installs the masker and target, and it owns the
SSH connections made under it:

```go
// What gets masked in anything you render for a human.
session := sdk.NewSession(sdk.NewMasker("s3cr3t"))
defer func() { _ = session.Close() }()

// Where dokku commands go. The zero Target runs locally.
ctx := session.Context(context.Background(), sdk.Target{
    Host: "deploy@dokku.example.com",
    Sudo: true,
})
```

Every command over SSH shares one multiplexed connection per server. The session records each
server its contexts actually reached and closes those connections on `Close()`. Without it, each one
stays open for up to a minute after the last command, or for as long as
[`sdk.WithControlPersist`](#how-long-connections-stay-open) says. A context derived from a session context
still belongs to that session, so a single call can go to another server with
`sdk.ContextWithTarget(ctx, other)` and that connection is closed too.

A session is safe to use from several goroutines, and one session can hand out contexts for
different servers. Each session has its own connections, so closing one never interrupts another
that is talking to the same server. `Close()` is idempotent. Call it once the calls made under the
session have returned: a call still in flight loses its connection, and a call made after `Close()`
fails with `sdk.ErrSessionClosed`.

`NewSession(nil)` installs no masker, leaving any masker already on the parent context in place.

Every context a session hands out also shares one lock table, so tasks executed under it at once
take turns on the same app or datastore service instead of colliding on dokku's deploy lock. See
[Running tasks concurrently](#running-tasks-concurrently).

A session can also own the [version gate](#version-floors): `sdk.NewSession(masker,
sdk.WithVersionGate())` installs it on every context the session hands out. That is the shape for a
long-lived embedder that gets a new context for every call it serves - each call keeps its own
cancellation, and the server's versions are still read once per session.

### How long connections stay open

A connection stays open for 60 seconds after its last command, so commands that follow one another
reuse it instead of connecting again. `sdk.WithControlPersist(d)` sets that time for every connection
the session opens, rounded up to whole seconds:

```go
session := sdk.NewSession(nil, sdk.WithControlPersist(0))
```

Zero closes a connection as soon as no command under the session is using it: the call that finishes
the last command closes the connection before it returns. Use it when your program may never reach
`Close()` - a Terraform provider, say, whose process is killed without a shutdown hook - so no
connection to the server is left behind. Commands that overlap still share one
connection, but each command that starts after the previous one finished opens a new one. A negative
duration is read as zero.

`Close()` still closes whatever is open, whatever the setting.

### Choosing the SSH key

Without more, `ssh` picks the key itself from ssh-agent or `~/.ssh/config` on the machine running
your program. A `Target` can name it instead, along with any other `ssh` options:

```go
ctx := session.Context(context.Background(), sdk.Target{
    Host:         "deploy@dokku.example.com",
    IdentityFile: "/run/secrets/dokku_deploy",
    SSHOptions:   []string{"ConnectTimeout=10", "ProxyJump bastion.example.com"},
})
```

`IdentityFile` is passed as `-i <path> -o IdentitiesOnly=yes`, so `ssh` offers that key alone.
Each entry of `SSHOptions` is one `-o` value, in `Key=Value` or `Key Value` form. The four options
docket uses to share a connection - `ControlMaster`, `ControlPath`, `ControlPersist` and
`BatchMode` - are refused: a call that names one fails with an `ssh:` error before `ssh` runs. Set
how long connections persist with [`sdk.WithControlPersist`](#how-long-connections-stay-open) instead.
Check a caller-supplied list ahead of time with `sdk.ValidateSSHOptions`. The identity and the
options are part of what picks the shared connection, so two targets on one host with different
keys never share one.

`IdentityFile` is a path, not key material. When your program holds the key itself - a Terraform
provider block, or a secret read from a vault - write it to a file only its owner can read, keep the
file for as long as the session is open, and remove it after `Close()`:

```go
f, err := os.CreateTemp("", "dokku-key-*")
if err != nil {
    return err
}
defer os.Remove(f.Name())
if err := f.Chmod(0o600); err != nil {
    return err
}
if _, err := f.WriteString(privateKey); err != nil {
    return err
}
if err := f.Close(); err != nil {
    return err
}

session := sdk.NewSession(nil)
defer func() { _ = session.Close() }()
ctx := session.Context(context.Background(), sdk.Target{
    Host:         "deploy@dokku.example.com",
    IdentityFile: f.Name(),
})
```

Deferred calls run in reverse order, so the session closes its connections before the key file is
removed.

Cancel the context and in-flight dokku commands stop; give it a deadline and they are bounded by it.
A signal handler is the caller's business - docket's own is installed in `main.go`, not in the
engine.

## Reading a server

`sdk.ExportRecipe` enumerates the server and returns the recipe that describes it, along with the
values it had to lift out of task bodies and any warnings it collected.

```go
res, err := sdk.ExportRecipe(ctx, sdk.ExportOptions{Inline: true})
if err != nil {
    return err
}

for _, play := range res.Plays() {
    for _, task := range play.Tasks {
        cfg, ok := sdk.As[sdk.ConfigTask](task)
        if !ok {
            continue
        }
        fmt.Println(play.Name, cfg.App, cfg.Config)
    }
}
```

An export reads everything, or everything a selector names. To read the one resource a task you
already hold describes, use `sdk.ReadTask` instead - see [Reading one resource](#reading-one-resource).

`Plays()` returns the same values `MarshalRecipe` renders, so the structured view and the recipe
file can never describe different exports. Each task body is the task's own type - `dokku_config`
comes back as a `ConfigTask` - so there is no marshalling to YAML and parsing it straight back.
`MarshalRecipe` takes `sdk.FormatYAML`, `sdk.FormatJSON5` or `sdk.FormatHCL`.

Some fields of a body never hold the server's value. A field whose catalog entry carries
`WriteOnly` (`dokku_service_create`'s `Password`) is stored by the server but never returned, so it
comes back empty or as a placeholder for an input with no value - except where the task's export
caveat names a command that reads it back, as `dokku_certs` does for its PEM. A field carrying `ApplyOnly`
(`dokku_config`'s `Restart`) is not server state, so it comes back at its zero value or default.
A caller that refreshes its own state from an export should keep the value it was configured with
for both, rather than treat the difference as drift. Read the flags from `sdk.FieldSchema`, element
fields included, through `sdk.Catalog()` - see [Task catalog](task-catalog.md#fields).

A list field's `Ordered` says how to compare it. An exported list whose `Ordered` is true
(`dokku_buildpacks`'s `Buildpacks`) comes back in the server's order, which is the order that
matters. One whose `Ordered` is false may come back in any order, so compare it ignoring order rather
than element by element.

Read a body with `sdk.As`, passing the concrete value type (`sdk.ConfigTask`). It returns `false`
for any other task, so a loop over every task can ask for the one type it wants. A body is always
the value form of the type registered under `task.Type`, never a pointer, so
`sdk.As[*sdk.ConfigTask]` never matches.

### Narrowing the read

`ExportOptions` narrows what is read:

| Field | Effect |
| --- | --- |
| `Apps` | Only these apps. The leading global play is skipped unless an address asks for it. |
| `Resources` | Only these addresses, parsed by `sdk.ParseResourceSelectors`. When every address names its app or is global, only those apps are read and the server's app list is not. |
| `Inline` | Keep sensitive values in the bodies instead of lifting them into `Vars`. |
| `Redact` | Replace sensitive values with a placeholder. |
| `IgnoreVersion` | Read every exporter even when Dokku, or an installed plugin it needs, is older than its task declares. The equivalent of `docket export --ignore-version`. |

An address is `type[key=value]`, so a single global resource is readable without exporting the whole
server:

```go
sel, err := sdk.ParseResourceSelectors([]string{"dokku_plugin[name=redis]"})
res, err := sdk.ExportRecipe(ctx, sdk.ExportOptions{Resources: sel, Inline: true})
```

`sdk.IdentityAddress` renders the address of a task you hold, and `sdk.ParseIdentityAddress` splits
one back into its type and keys. To read one resource as a task rather than as a recipe, including
one export cannot reconstruct, use [`sdk.ReadAddress`](#reading-from-an-address) instead.

### Secrets

An export is the one read path with no recipe to collect a sensitive set from ahead of time: the
values needing masking are the ones its own exporters just read back. Register them before printing
anything, including the warnings.

```go
masker := sdk.NewMasker(res.SensitiveValues()...)
for _, w := range res.Report.Warnings {
    fmt.Fprintln(os.Stderr, masker.String(w))
}
```

`res.Report` also carries `MissingApps` and `MissingResources` - names that were asked for and not
found. An export that finds nothing is not an error, so check them rather than relying on `err`.
`res.Report.VersionErrors` lists the exporters skipped because the server is older than their task
declares (see [Version floors](#version-floors)); each one is also in `Warnings`.

## Building tasks

Use `sdk.NewTask` rather than a struct literal. A literal skips the `default:` tags the loader
applies, so a task with no `State` gets `""`, which is an invalid state rather than the `present`
the field documents.

```go
task, err := sdk.NewTask("dokku_app")
if err != nil {
    return err
}
app := task.(*sdk.AppTask)
app.App = "api"

plan := app.Plan(ctx)                // reports drift, never mutates
state := sdk.ExecutePlan(ctx, plan)  // applies that plan
current := state.Observed            // the app as the server now holds it
```

`app.Execute(ctx)` plans and applies in one call. `sdk.DecodeTask` is the same thing as `NewTask`
from a YAML task body, for a caller that already holds a recipe fragment.

### The state after an apply

A caller that stores the resource's state, as a Terraform provider does after every create and
update, need not read it back. The `sdk.TaskOutputState` that `Execute` and `ExecutePlan` return
carries `Observed`, the task as the server holds it once the task has run, and `Exists`, whether the
resource is there. They follow the rules `ReadTask` follows for a task that plans in sync (see
[Reading one resource](#reading-one-resource)): every field the task declares comes back as
declared, every field it leaves at its zero value comes back as the server reports it, and fields
flagged `WriteOnly` or `ApplyOnly` stay empty or at their default. `Observed` is the value type
registered for the task (`sdk.AppTask`, never a pointer), and its values are raw, so mask what you
show.

A task that was already in sync carries its plan's `Observed` and `Exists`, and nothing more is
read. After a successful apply, the plan's own read is reused when the task declares every field a
read fills, since the declaration then decides every field and the server is not probed again: a
`dokku_app`, or a `dokku_config` in state `present`, reads the server only for its plan. When the task leaves such a
field unset, the apply may have changed it - a create picks the server's defaults, such as the image
a `dokku_service_create` that pins none is created on, a destroy or a `clear` removes what the plan
read, and an in-place change such as a `dokku_storage_mount` remount resets the options it does not
pass - so the task is planned once more after the apply and `Observed` is what that plan observes.
Under `Execute`, that second plan runs under the same locks as the apply (see
[Running tasks concurrently](#running-tasks-concurrently)).

`Observed` is nil when the plan errored or the apply failed, when the task reads nothing
(`dokku_storage_ensure`), when the probe failed in a way the plan treated as drift, when the second
plan fails, and for an imperative task (`dokku_app_clone`, `dokku_git_sync`, `dokku_git_from_image`,
`dokku_git_from_archive`), which runs an operation rather than declaring a resource. A failed
second plan does not fail the task: the apply succeeded, and `sdk.ReadTask` reads the state again.

A caller that never looks at `Observed` skips it, and the second plan with it, by running the task
under `sdk.ContextWithoutObserved(ctx)`. `docket apply` does, so its output and the values a recipe
registers with `register:` carry neither field.

### Checking a task's input

`sdk.ConstraintViolations` returns every [declared field constraint](task-catalog.md#constraints) a
task breaks, without contacting a server: a field required or refused in its state, fields that
exclude or need each other, and the same for each item of its structured lists. Each `sdk.Violation`
carries the recipe key path at fault in `Field` and a `Message`. `Plan` refuses a task over the first
of them; this reports them all at once, as `docket validate` does:

```go
for _, v := range sdk.ConstraintViolations(task) {
    fmt.Printf("%s: %s\n", v.Field, v.Message)
}
```

A task's `Validate()` checks the same constraints first, then the rules that depend on a value, such
as an enum or a format. A task's `Plan()` runs the same `Validate()` before it probes, so a plan
refuses whatever validation refuses. `sdk.TaskConstraints` returns a task's declared multi-field rules.

A caller that checks a task before all its values exist, such as a Terraform provider at plan time
where an attribute comes from another resource's output, names those fields with `sdk.WithUnknown`.
Each path is a recipe key path in the form `Violation.Field` uses, including list items:

```go
violations := sdk.ConstraintViolations(task, sdk.WithUnknown("app", "users[1].hash"))
```

A rule is then reported only when the known fields decide it, so every violation returned holds
whatever the unknown values turn out to be:

- An unknown field counts as set for a rule that needs it present: its `required_when`, a list
  item's required keys, an `at_least_one_of` or `all_or_none_of` it is in, and the fields a
  `required_with` requires.
- A rule whose outcome turns on an unknown value is skipped: its `forbidden_when`, an
  `exactly_one_of` with at most one known field set, a `conflicts_with` it is in, and a
  `required_with` it is the `field` of.
- A rule the known fields decide on their own is reported as usual, such as two set fields in an
  `exactly_one_of` or a `conflicts_with`, or a set and an unset field in an `all_or_none_of`.
- An unknown `state` skips every rule scoped to a state: each field's `required_when` and
  `forbidden_when`, and every constraint that lists `states`, on the task and on its list items.
  Rules that apply in every state are still checked.
- An unknown list, such as `users`, is an unknown field, and none of its items are checked. An
  unknown item, such as `users[1]`, is skipped while the other items are checked.
- An unknown `property` skips the check that a property task's property is not one another task
  manages. A path that names no field is ignored.

`Validate()` and `Plan()` take no unknowns: they check the values the task holds.

### Masking a task

A task's own secrets are its `sensitive:"true"` fields, including those of nested items such as
`dokku_http_auth_user`'s users, the value of a property marked sensitive, and whatever the task's own
`SensitiveValues()` returns. A task's `Plan` and `Execute`, `sdk.ReadTask`, `sdk.ReadAddress` and
`sdk.ExecutePlan` add them to the masker on their context before they run anything, so a session's
masker masks them in every command line, error and stderr without being told them first:

```go
masker := sdk.NewMasker()
session := sdk.NewSession(masker)
```

A context with no masker has nothing registered with it, so `sdk.NewSession(nil)` masks nothing. A
value only known once the server is read, such as a secret property's current value, is added to the
context's masker during `Plan` too, so pass the same masker to the session rather than building a
second one.

`sdk.TaskSensitiveValues` returns the same values for a task, with empty values dropped and
duplicates removed. Call it only to mask output you render before any of those calls has run, such
as a task printed before it is planned.

### Version floors

Every task declares the oldest Dokku it runs on, and any plugins it needs, in its Requirements
section. `docket plan` and `docket apply` refuse a task whose floor the server is behind. A caller
gets the same refusal by installing the version gate once per run. Every task's `Plan`, and so its
`Execute` and `sdk.ReadTask`, then checks it before probing and returns the refusal as the plan's
error:

```go
ctx = sdk.ContextWithVersionGate(ctx)

plan := app.Plan(ctx)
var verr *sdk.VersionError
if errors.As(plan.Error, &verr) {
    // verr.TaskType, verr.Plugin, verr.Minimum and verr.Found say why.
}
```

`sdk.ExecutePlan` does not check again, since it applies a plan that was already checked.
`sdk.CheckMinimumVersions(ctx, "dokku_app", app)` returns the same `*VersionError` without
planning, for a caller that wants to check a task first. Neither refuses anything when the context
carries no gate, so skipping the check - the equivalent of `--ignore-version` - is a matter of not
installing the gate. A
plugin the task needs that is not installed at all is not a refusal: the task's own probe reports
it, and an earlier task in the same run can install it.

The gate reads `dokku version` and `dokku plugin:list` once per target - one host with or without
sudo, and with its own identity file and ssh options - and keeps the answers for as long as the
context lives, so install a fresh gate for each run rather than keeping one across runs that could
straddle a server upgrade. Calls checking the same server at once share one read, and only a
successful read is kept: a failed one is returned to the call that made it, and the next call asks
again. `dokku_plugin` drops the cached plugin list itself after it installs or uninstalls a plugin,
so a later task in the same run sees the change without anything from the caller.

A caller that builds a new context for every call it serves - a Terraform provider is called once
per resource operation, each with its own context - would get a fresh cache from each
`ContextWithVersionGate`. Give the gate to the session instead:

```go
session := sdk.NewSession(masker, sdk.WithVersionGate())
defer func() { _ = session.Close() }()

// Per call: cancellation from reqCtx, the version cache from the session.
ctx := session.Context(reqCtx, target)
current, exists, err := sdk.ReadTask(ctx, task)
```

Every context the session hands out shares one cache per target for as long as the session lives,
and `dokku_plugin`'s invalidation reaches it from any of them. Each session has its own cache, and a
session's gate replaces one already on the parent context. A session built without
`WithVersionGate` installs none, which is the way to skip the check.

A `VersionError` names no flag. `Plugin` is empty when the floor is Dokku's own. `Err` is set when
the version could not be read or parsed, and is nil when the server is simply older than `Minimum`.

`sdk.ExportRecipe` installs the gate itself, and skips an exporter whose floor the server is behind;
set `ExportOptions.IgnoreVersion` to read it anyway. That option wins over a gate already on the
context.

### Listing task types

`sdk.TaskTypes()` returns every registered task type, sorted. `sdk.Lookup` reports whether a type is
registered and returns an instance of it for reading metadata:

```go
for _, typeKey := range sdk.TaskTypes() {
    task, _ := sdk.Lookup(typeKey)
    fmt.Println(typeKey, sdk.TaskSynopsis(task))
}
```

Each `Lookup` call returns a new zero-value instance, so changing it affects nothing else. It has no
defaults applied; use `sdk.NewTask` for a task to run. `sdk.TaskDeprecation`,
`sdk.TaskImperative`, `sdk.TaskProbeSupport`, `sdk.TaskExportSupport`,
`sdk.TaskMinimumVersions` and `sdk.TaskConstraints` read the rest of a task's metadata.

`sdk.Catalog()` describes every task type in one value - synopsis, deprecation, requirements,
probe and export support, identity, fields, constraints, and the notes its reference page prints -
and is what `docket schema` emits.
`sdk.CatalogFor` narrows it to the types you name. See [Task catalog](task-catalog.md).

## Reading one resource

`sdk.ReadTask` returns the resource a task describes as it exists on the server, and whether it
exists, from a single plan. Nothing is applied.

```go
ctx = sdk.ContextWithVersionGate(ctx)

task, _ := sdk.NewTask("dokku_config")
cfg := task.(*sdk.ConfigTask)
cfg.App = "api"
cfg.Config = map[string]string{"LOG_LEVEL": "info"}

current, exists, err := sdk.ReadTask(ctx, cfg)
if err != nil {
    return err
}
observed := current.(sdk.ConfigTask) // observed.Config holds the server's LOG_LEVEL, if set
```

The returned task is the value type registered for the task (`sdk.ConfigTask`, never a pointer).
Fields whose catalog entry carries `WriteOnly` or `ApplyOnly` are left empty or at their default,
as an export leaves them (see [Reading a server](#reading-a-server)). What fills the other fields
depends on whether the task you passed plans in sync:

- When it does, the server already holds what the task declares, so every field the task sets
  comes back as you passed it, even where the server reports it in another form. A
  `dokku_ps_scale` in state `set` with `scale: {web: 2, worker: 0}` comes back with `worker: 0`
  although the server's report leaves out process types scaled to zero, and a list whose order
  does not matter comes back in your order. A field the task leaves at its zero value declares
  nothing, so it is filled from what the probe read: a `dokku_service_create` that pins no image
  comes back with the image the service runs. `exists` follows the declared state: true unless
  `State` is `absent` or `clear`, so a `set` with no member the server reports, such as
  `scale: {web: 0}`, still exists. A consumer that stores the read and diffs it against the task,
  as a Terraform refresh does, then sees a change only when `Plan` would apply one.
- When it does not, the returned task is what the probe read. Its identity keys come from the task
  you passed and every field the server reports is filled from the read. `State` is the state
  that, declared with the returned fields, would plan in sync: a property that is not set comes
  back with `State: absent` and `exists` false.

The task you pass picks what is read, so it must be valid:

- A collection in an additive state reads only the members it names. A `dokku_config` in state
  `present` or `absent` reads the keys in its `Config`. A state that owns the whole collection, such
  as `set` or `clear`, reads every member it would own, and comes back as `set`, or as `clear` when
  there are none.
- A property task needs a `Value` in state `present`. To read one known only by its identity keys,
  pass it with `State` absent.

To read every member of a collection without naming them, read it from its address instead (see
[Reading from an address](#reading-from-an-address)). `dokku_certs` is the exception to reading
everything: its plans settle drift from the certificate's fingerprint so no certificate material
leaves the server, and a read keeps to that, returning whether a certificate is installed but not
its PEM. That is why `CertContent` and `KeyContent` carry `WriteOnly`: a read leaves every
field flagged `WriteOnly` or `ApplyOnly` empty, and fills every other field the server reports.

The same values are on the plan itself, so a caller that plans anyway reads them with no extra probe.
`PlanResult.Observed` holds the returned task and `PlanResult.Exists` whether it exists, by the same
rules. They are set on every plan that read the server, in sync or not. After an apply, the state
`Execute` returns carries the same fields for the server as the apply left it (see
[The state after an apply](#the-state-after-an-apply)). `ReadTask` is `Plan` plus
these errors:

- the plan's own error, including the `*sdk.VersionError` when a gate on the context refuses the
  task, so a resource the gate skipped is never reported as absent;
- an `*sdk.ObjectMissingError` when the app or datastore service the task depends on does not
  exist on the server, whatever the task's own probe made of that - an error, an absent resource, or
  drift. It matches `sdk.ErrObjectMissing` under `errors.Is`, and its `Object` names the missing
  object by kind, recipe keys and values. A task that creates its own app or service
  (`dokku_app`, `dokku_app_clone`, `dokku_service_create`) reports `exists` false instead;
- an error wrapping `sdk.ErrStateUnreadable` when the plan carries no observed state. That is a task
  whose probe cannot read anything (`dokku_storage_ensure`), or a probe failure the plan treated as
  drift rather than as an error.

A missing parent is checked only when the read does not find the resource, so a present read costs
no extra call. A task whose input is invalid returns its own validation error, and a check that
cannot reach the server leaves the read's own error in place, so `ErrObjectMissing` always means
the server answered that the object is gone:

```go
_, exists, err := sdk.ReadAddress(ctx, "dokku_service_link[app=api,service=postgres,name=db]")
var missing *sdk.ObjectMissingError
if errors.As(err, &missing) {
    // missing.Object.Kind is sdk.ObjectApp or sdk.ObjectService; drop the resource from state
}
```

A probe registers the server values it treats as secrets with the context's masker, as it does for
any plan, but the returned task carries them raw. Mask what you show, the same way you mask a plan's
`Reason`.

### Reading from an address

`sdk.ReadAddress` reads a resource known only by its identity address - the shape `terraform import`
or a data source has - and returns the same errors as `sdk.ReadTask`. With no declared task to
return, it always returns what the probe read and whether that exists, in sync or not. It is what
[`docket read`](command-reference.md#docket-read) runs:

```go
current, exists, err := sdk.ReadAddress(ctx, "dokku_buildpacks[app=api]")
if err != nil {
    return err
}
buildpacks := current.(sdk.BuildpacksTask).Buildpacks // every buildpack the app has
```

It builds the task from the address's keys and reads every member the resource has, whatever states
the task offers. A collection comes back the way a task in state `set` or `clear` would own it, so a
resource imported from the read plans in sync:

- `dokku_config` leaves out the keys docket keeps on its own - linked-service DSNs, `NO_VHOST`, and
  the git rev env var - and `dokku_ps_scale` leaves out process types scaled to zero. An export
  leaves out the same ones.
- `dokku_storage_mount[app=api]` reads every mount in the `_default_` scope; name `process_type` to
  read another scope's, or `container_dir` to read one mount.
- `dokku_docker_options` keys on the option itself, so its address reads whether that one option is
  set.
- A `dokku_service_property` whose value dokku reports only as a fingerprint, such as
  `backup-secret-access-key`, comes back `present` with an empty `Value`.
- A resource whose app or service is missing returns the `*sdk.ObjectMissingError`, not `exists`
  false, the same as `sdk.ReadTask`.

The address must name one resource. A bare type such as `dokku_config`, a key the task does not
declare, or a missing required key is an error, raised before the server is contacted. Keys the
task treats as mutually exclusive, such as `app` and `global`, are refused by the task's own
validation. A task whose probe cannot read anything (`dokku_storage_ensure`) returns an error wrapping
`sdk.ErrStateUnreadable`, a gate on the context refuses a server that is too old with the
`*sdk.VersionError` before anything else is read, and a missing app or service returns the
`*sdk.ObjectMissingError`.

## Running tasks concurrently

The CLI runs one task at a time. An embedder need not: a Terraform provider applies up to ten
resources at once, so a `dokku_config` that restarts `api` and a `dokku_ps_scale` on `api` can arrive
together. Against the server, the second can fail on dokku's deploy lock (`api currently has a deploy
lock in place`), or have its change overwritten by the first one's restart.

A session serializes them. Every context `sdk.NewSession` hands out carries one lock table, and a
task's `Execute` takes the locks for the objects it changes before it plans and releases them once it
has applied, so no other task on the same object runs between its plan and its apply:

- An app task locks the app named by `app`.
- A datastore task locks the service named by its datastore type and its name, so a
  `dokku_service_create`, `dokku_service_property`, `dokku_service_expose` and
  `dokku_service_backup` on one service take turns.
- `dokku_service_link` changes both the app's config and the service's links, so it locks both.
  Locks are always taken app first, then service, so two tasks can never each hold one and wait on
  the other.

Tasks on different objects, or on the same name on two servers, still run at once. A task naming
neither - a server-level task such as `dokku_plugin`, or an app-or-global task with `global: true` -
locks nothing. `sdk.TaskObjects` returns what a task locks, and the catalog publishes the same as
[`objects`](task-catalog.md#objects).

A caller still waiting when its context ends gets an error wrapping the context's, and holds
nothing. A context built without a session gets the same table from `sdk.ContextWithTaskLocks`.

Only `Execute` locks. `Plan`, `ReadTask` and `ReadAddress` take nothing, so refreshing many
resources still runs in parallel, and neither does `sdk.ExecutePlan`. A caller that plans and
applies in separate steps holds the locks across both with `sdk.LockTask`:

```go
ctx, unlock, err := sdk.LockTask(ctx, task)
if err != nil {
    return err
}
defer unlock()

plan := task.Plan(ctx)
// ... show or check the plan ...
state := sdk.ExecutePlan(ctx, plan) // any second plan for state.Observed runs under the locks too
```

An `Execute` under the returned context does not wait on the locks it already holds. One that needs
a lock ordered before a held one - executing a `dokku_service_link` while holding only its service -
returns an error instead of risking a deadlock, so lock the task that changes both first.

Locks order tasks that overlap; they do not order tasks that depend on each other. A
`dokku_service_property` started alongside the `dokku_service_create` for its service may still run
first and find no service. The catalog says which task creates each object and which tasks restart
it, so a caller can order them - see [Ordering tasks](task-catalog.md#ordering-tasks).

docket does not retry a command dokku refused over its deploy lock. `dokku_app_lock` holds the same
lock on purpose, and dokku reports both the same way, so a retry could wait on a lock a person
placed deliberately. A lock held by something outside the session - a `git push`, or another docket
process - still fails the task.

## Testing without a server

`sdk.ContextWithRunner` installs a fake in place of dokku, so a test can plan and apply tasks and
assert the exact commands they issue without a server:

```go
var calls []string
ctx := sdk.ContextWithRunner(context.Background(), func(_ context.Context, in sdk.ExecCommandInput) (sdk.ExecCommandResponse, error) {
    calls = append(calls, strings.Join(in.Args, " "))
    switch strings.Join(in.Args, " ") {
    case "--quiet apps:exists api":
        // The app is absent: the command ran and exited non-zero.
        return sdk.ExecCommandResponse{ExitCode: 1}, nil
    case "--quiet apps:create api":
        // The create fails the way the real runner reports it.
        res := sdk.ExecCommandResponse{Stderr: "name is reserved", ExitCode: 1}
        return res, &sdk.ExecError{Response: res, Err: errors.New(res.Stderr), Ran: true}
    }
    return sdk.ExecCommandResponse{}, nil
})

plan := app.Plan(ctx)                // Status create, from the apps:exists answer
state := sdk.ExecutePlan(ctx, plan)  // state.Error is the *sdk.ExecError, state.Message its stderr
```

A command that ran and exited non-zero is a response carrying that `ExitCode`. A probe reads it as
its answer whether the fake returns a nil error with it or, as the real runner does, an
`*sdk.ExecError` with `Ran` set. An apply fails only on an error, so return the `*sdk.ExecError` to
fail one. Any other error is a command that could not run, and a plan reports it as an error rather
than as the resource being absent.

The fake receives every command a call makes, not only dokku's, with the arguments as they would
have run: unmasked, and without the sudo or SSH wrapping a `Target` adds. A `Target` on the context
has no effect while a fake is installed. The response's `Command` is what an apply records in
`TaskOutputState.Commands`, so set it when a test reads that. `PlanResult.Commands` is rendered by
the engine and masked by the session's masker either way.

The engine does not mask the errors and messages it returns; the caller masks what it shows. The
session's masker already holds the task's own secrets once the task is planned (see
[Masking a task](#masking-a-task)), so a test can check both the masked plan and the masked
diagnostic without registering them:

```go
masker := sdk.NewMasker()
session := sdk.NewSession(masker)
defer func() { _ = session.Close() }()
ctx := sdk.ContextWithRunner(session.Context(context.Background(), sdk.Target{}), fake)

state := sdk.ExecutePlan(ctx, cfg.Plan(ctx))
shown := masker.String(state.Message)
```

The fake travels on the context rather than in a package variable, so tests that install different
fakes can all call `t.Parallel()`.

Production code must not use `ContextWithRunner`. It exists so a test can drive a task without a
server; to change where commands actually run, set a `Target`.

## Stability

The exported identifiers in `sdk` are stable from the release they first ship in. While docket is
at 0.x, a change that breaks a caller only ships in a release that bumps the minor version, and the
release notes call it out. From 1.0 on, only in a major version. Releases are tagged `vX.Y.Z`, so
`go get github.com/dokku/docket/sdk@vX.Y.Z` pins one.

The one exception is the fields of the task types (`AppTask`, `ConfigTask` and so on). They mirror
the recipe format and change when it does: a field added, renamed or removed in a recipe is added,
renamed or removed here in the same release.

The types in `sdk` are aliases of the engine's own, so a value built through `sdk` is the value the
engine runs. Every type their exported fields and methods take or return is named in `sdk` too.
Nothing under `internal/` carries any guarantee.
