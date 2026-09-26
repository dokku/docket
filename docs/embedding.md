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
defer session.Close()

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
`sdk.TaskProbeSupport` and `sdk.TaskExportSupport` read the rest of a task's metadata.

`sdk.Catalog()` describes every task type in one value - synopsis, deprecation, requirements,
probe and export support, identity and fields - and is what `docket schema` emits.
`sdk.CatalogFor` narrows it to the types you name. See [Task catalog](task-catalog.md).

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
