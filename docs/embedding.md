# Embedding docket in Go

docket's engine is a Go package, not only a CLI. This page covers the parts that are stable enough
to build on: reading a server back as structured data, building tasks, and the per-invocation state
every call needs.

Import path is `github.com/dokku/docket/tasks`.

## The run context

Everything takes a `context.Context`, and that context carries the state a run needs. Nothing is
read from process globals, so two runs in one process can target different servers, mask different
values, and be cancelled independently.

```go
ctx := context.Background()

// Where dokku commands go. The zero Target runs locally.
ctx = subprocess.ContextWithTarget(ctx, subprocess.Target{
    Host: "deploy@dokku.example.com",
    Sudo: true,
})

// What gets masked in anything you render for a human.
ctx = subprocess.ContextWithMasker(ctx, subprocess.NewMasker("s3cr3t"))
```

Cancel the context and in-flight dokku commands stop; give it a deadline and they are bounded by it.
A signal handler is the caller's business - docket's own is installed in `main.go`, not in the
engine.

## Reading a server

`tasks.ExportRecipe` enumerates the server and returns the recipe that describes it, along with the
values it had to lift out of task bodies and any warnings it collected.

```go
res, err := tasks.ExportRecipe(ctx, tasks.ExportOptions{Inline: true})
if err != nil {
    return err
}

for _, play := range res.Plays() {
    for _, task := range play.Tasks {
        cfg, ok := task.Body.(tasks.ConfigTask)
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

Bodies are values, not pointers, because that is what an exporter returns. Type-assert the concrete
type (`tasks.ConfigTask`), not the `Task` interface.

### Narrowing the read

`ExportOptions` narrows what is read:

| Field | Effect |
| --- | --- |
| `Apps` | Only these apps. The leading global play is skipped unless an address asks for it. |
| `Resources` | Only these addresses, parsed by `tasks.ParseResourceSelectors`. |
| `Inline` | Keep sensitive values in the bodies instead of lifting them into `Vars`. |
| `Redact` | Replace sensitive values with a placeholder. |

An address is `type[key=value]`, so a single global resource is readable without exporting the whole
server:

```go
sel, err := tasks.ParseResourceSelectors([]string{"dokku_plugin[name=redis]"})
res, err := tasks.ExportRecipe(ctx, tasks.ExportOptions{Resources: sel, Inline: true})
```

### Secrets

An export is the one read path with no recipe to collect a sensitive set from ahead of time: the
values needing masking are the ones its own exporters just read back. Register them before printing
anything, including the warnings.

```go
masker := subprocess.NewMasker(res.SensitiveValues()...)
for _, w := range res.Report.Warnings {
    fmt.Fprintln(os.Stderr, masker.String(w))
}
```

`res.Report` also carries `MissingApps` and `MissingResources` - names that were asked for and not
found. An export that finds nothing is not an error, so check them rather than relying on `err`.

## Building tasks

Use `tasks.NewTask` rather than a struct literal. A literal skips the `default:` tags the loader
applies, so a task with no `State` gets `""`, which is an invalid state rather than the `present`
the field documents.

```go
task, err := tasks.NewTask("dokku_app")
if err != nil {
    return err
}
app := task.(*tasks.AppTask)
app.App = "api"

plan := app.Plan(ctx)      // reports drift, never mutates
state := app.Execute(ctx)  // plans, then applies
```

`tasks.DecodeTask` is the same thing from a YAML task body, for a caller that already holds a recipe
fragment.

## What is not stable

The engine is exported, not frozen. Task struct fields follow the recipe format and change with it;
`RegisteredTasks` is a live map and writing to it is not supported. Treat anything not on this page
as internal.
