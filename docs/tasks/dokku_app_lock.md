# dokku_app_lock

## Synopsis

Locks or unlocks a dokku application from deployment

## Requirements

- Dokku >= 0.38.31

## Export support

Supported.

## Probe support

Supported.

## Lifecycle

Applied with state `present` and torn down with state `absent`. See [Applying and tearing down a task](../embedding.md#applying-and-tearing-down-a-task).

## Identity

Keyed by `app`.

## Objects

- The app named by `app`.

Tasks applied at once on the same object take turns; see [Running tasks concurrently](../embedding.md#running-tasks-concurrently).

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `state` | string | no | present | present, absent | Desired lock state |

## Examples

### Lock an app

Requires these tasks, applied first:

```yaml
- dokku_app:
      app: node-js-app
```

```yaml
dokku_app_lock:
    app: node-js-app
```

### Unlock an app

Requires these tasks, applied first:

```yaml
- dokku_app:
      app: node-js-app
```

```yaml
dokku_app_lock:
    app: node-js-app
    state: absent
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
