# dokku_resource_limit

## Synopsis

Manages the resource limits for a given dokku application

## Requirements

- Dokku >= 0.38.31

## Export support

Supported.

## Probe support

Supported.

## Lifecycle

Applied with state `set` and torn down with state `clear`, which refuses `resources` and `clear_before`, so a teardown resets them. See [Applying and tearing down a task](../embedding.md#applying-and-tearing-down-a-task).

## Identity

Keyed by `app` and `process_type`. Fields left empty are omitted from the address. Manages the whole `resources` collection; entries are identified by their key.

## Objects

- The app named by `app`.

Tasks applied at once on the same object take turns; see [Running tasks concurrently](../embedding.md#running-tasks-concurrently).

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `process_type` | string | no |  |  | Process type to set resource limits for |
| `resources` | dict | no |  |  | Map of resource type to quantity (required when state is present) (must not be set when state is clear) |
| `clear_before` | bool | no | false |  | ClearBefore clears all resource limits before applying new ones (must not be set when state is set or clear) (applied only, not read back) |
| `state` | string | no | present | present, absent, set, clear | Desired state of the resource limits |

## Examples

### Set CPU and memory limits

```yaml
dokku_resource_limit:
    app: hello-world
    resources:
        cpu: "100"
        memory: "256"
```

### Set memory limit for web process type

```yaml
dokku_resource_limit:
    app: hello-world
    process_type: web
    resources:
        memory: "512"
```

### Replace the resource limits with exactly these

```yaml
dokku_resource_limit:
    app: hello-world
    resources:
        memory: "256"
    state: set
```

### Clear all resource limits

```yaml
dokku_resource_limit:
    app: hello-world
    state: clear
```

## Return Values

Available after the task runs when captured with `register:`, referenced as `result.<Key>` (or `registered.<name>.<Key>`).

| Key | Returned | Type | Description |
| --- | --- | --- | --- |
| `Changed` | always | bool | Whether the task changed server state. |
| `State` | always | string | Resulting state of the resource. |
| `DesiredState` | always | string | The state the task targeted. |
| `Message` | always | string | Human-readable result message (may be empty). |
| `Commands` | when a subprocess ran | list | Resolved dokku command lines executed. |
| `Stdout` | when a subprocess ran | string | Captured stdout of the final command. |
| `Stderr` | when a subprocess ran | string | Captured stderr of the final command. |
| `ExitCode` | when a subprocess ran | int | Exit code of the final command. |
