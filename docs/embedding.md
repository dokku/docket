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

`Plays()` returns the same values `MarshalRecipe` renders, so the structured view and the recipe
file can never describe different exports. Each task body is the task's own type - `dokku_config`
comes back as a `ConfigTask` - so there is no marshalling to YAML and parsing it straight back.
`MarshalRecipe` takes `sdk.FormatYAML`, `sdk.FormatJSON5` or `sdk.FormatHCL`.

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
one back into its type and keys.

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
section. `docket plan` and `docket apply` refuse a task whose floor the server is behind. `Plan`,
`Execute` and `sdk.ExecutePlan` do not check it themselves, so a caller that wants the same refusal
installs the version gate once per run and checks each task before planning it:

```go
ctx = sdk.ContextWithVersionGate(ctx)

if err := sdk.CheckMinimumVersions(ctx, "dokku_app", app); err != nil {
    var verr *sdk.VersionError
    if errors.As(err, &verr) {
        // verr.TaskType, verr.Plugin, verr.Minimum and verr.Found say why.
    }
    return err
}
plan := app.Plan(ctx)
```

`CheckMinimumVersions` returns nil when the context carries no gate, so skipping the check - the
equivalent of `--ignore-version` - is a matter of not installing the gate or not calling it. A
plugin the task needs that is not installed at all is not a refusal: the task's own probe reports
it, and an earlier task in the same run can install it.

The gate reads `dokku version` and `dokku plugin:list` once per target - one host with or without
sudo - and keeps the answers for as long as the context lives, so install a fresh gate for each run
rather than keeping one across runs that could straddle a server upgrade. `dokku_plugin` drops the
cached plugin list itself after it installs or uninstalls a plugin, so a later task in the same run
sees the change without anything from the caller.

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
