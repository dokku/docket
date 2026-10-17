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
stays open for up to a minute after the last command. A context derived from a session context
still belongs to that session, so a single call can go to another server with
`sdk.ContextWithTarget(ctx, other)` and that connection is closed too.

A session is safe to use from several goroutines, and one session can hand out contexts for
different servers. Each session has its own connections, so closing one never interrupts another
that is talking to the same server. `Close()` is idempotent. Call it once the calls made under the
session have returned: a call still in flight loses its connection, and a call made after `Close()`
fails with `sdk.ErrSessionClosed`.

`NewSession(nil)` installs no masker, leaving any masker already on the parent context in place.

A session can also own the [version gate](#version-floors): `sdk.NewSession(masker,
sdk.WithVersionGate())` installs it on every context the session hands out. That is the shape for a
long-lived embedder that gets a new context for every call it serves - each call keeps its own
cancellation, and the server's versions are still read once per session.

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
`BatchMode` - are refused: a call that names one fails with an `ssh:` error before `ssh` runs.
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
state := sdk.ExecutePlan(ctx, plan)  // applies that plan without probing again
```

`app.Execute(ctx)` plans and applies in one call. `sdk.DecodeTask` is the same thing as `NewTask`
from a YAML task body, for a caller that already holds a recipe fragment.

### Masking a task

`sdk.TaskSensitiveValues` returns the values the CLI masks for a task before a run: its
`sensitive:"true"` fields, including those of nested items such as `dokku_http_auth_user`'s users,
the value of a property marked sensitive, and whatever the task's own `SensitiveValues()` returns.
Build the masker from it before planning, so nothing rendered for the task carries a secret:

```go
masker := sdk.NewMasker(sdk.TaskSensitiveValues(task)...)
session := sdk.NewSession(masker)
```

Empty values are dropped and duplicates removed. A value only known once the server is read, such as
a secret property's current value, is added to the context's masker during `Plan`, so pass the same
masker to the session rather than building a second one.

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
`sdk.TaskProbeSupport`, `sdk.TaskExportSupport` and `sdk.TaskMinimumVersions` read the rest of a
task's metadata.

`sdk.Catalog()` describes every task type in one value - synopsis, deprecation, requirements,
probe and export support, identity and fields - and is what `docket schema` emits.
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

The returned task is the value type registered for the task (`sdk.ConfigTask`, never a pointer). Its
identity keys come from the task you passed and every field the server reports is filled from what
the probe read. Fields whose catalog entry carries `WriteOnly` or `ApplyOnly` are left empty or at
their default, as an export leaves them (see [Reading a server](#reading-a-server)). `State` is the
state that, declared with the returned fields, would plan in sync: a property that is not set
comes back with `State: absent` and `exists` false.

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
`PlanResult.Observed` holds the task as read and `PlanResult.Exists` whether it exists. They are set
on every plan that read the server, in sync or not. `ReadTask` is `Plan` plus these errors:

- the plan's own error, including the `*sdk.VersionError` when a gate on the context refuses the
  task, so a resource the gate skipped is never reported as absent;
- an error wrapping `sdk.ErrStateUnreadable` when the plan carries no observed state. That is a task
  whose probe cannot read anything (`dokku_storage_ensure`), or a probe failure the plan treated as
  drift rather than as an error.

A probe registers the server values it treats as secrets with the context's masker, as it does for
any plan, but the returned task carries them raw. Mask what you show, the same way you mask a plan's
`Reason`.

### Reading from an address

`sdk.ReadAddress` reads a resource known only by its identity address - the shape `terraform import`
or a data source has - and returns the same task, `exists` flag and errors as `sdk.ReadTask`:

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
- The service tasks (`dokku_service_backup`, `dokku_service_expose`, `dokku_service_link`,
  `dokku_service_property`) return an error rather than `exists` false when the service or app they
  name is missing, as their plans do.

The address must name one resource. A bare type such as `dokku_config`, a key the task does not
declare, or a missing required key is an error, raised before the server is contacted. Keys the
task treats as mutually exclusive, such as `app` and `global`, are refused by the task's own
validation. A task whose probe cannot read anything (`dokku_storage_ensure`) returns an error wrapping
`sdk.ErrStateUnreadable`, and a gate on the context refuses a server that is too old with the
`*sdk.VersionError` before anything else is read.

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

The engine does not mask the errors and messages it returns; the caller masks what it shows. Build
the masker from `sdk.TaskSensitiveValues` (see [Masking a task](#masking-a-task)), as the CLI does
before a run, and a test can check both the masked plan and the masked diagnostic:

```go
masker := sdk.NewMasker(sdk.TaskSensitiveValues(cfg)...)
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
