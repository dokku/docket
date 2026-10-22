# dokku_buildpacks

## Synopsis

Manages the buildpacks for a given dokku application

## Requirements

- Dokku >= 0.38.31

## Export support

Supported.

## Probe support

Supported.

## Lifecycle

Applied with state `set` and torn down with state `clear`, which refuses `buildpacks`, so a teardown resets it. See [Applying and tearing down a task](../embedding.md#applying-and-tearing-down-a-task).

## Identity

Keyed by `app`. Manages the whole `buildpacks` collection; entries are identified by value.

## Objects

- The app named by `app`.

Tasks applied at once on the same object take turns; see [Running tasks concurrently](../embedding.md#running-tasks-concurrently).

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `buildpacks` | list | no |  |  | List of buildpack URLs (order is significant) (required when state is present) (must not be set when state is clear) |
| `state` | string | no | present | present, absent, set, clear | Desired state of the buildpacks |

## Examples

### Add buildpacks to an app

Requires these tasks, applied first:

```yaml
- dokku_app:
      app: node-js-app
```

```yaml
dokku_buildpacks:
    app: node-js-app
    buildpacks:
        - https://github.com/heroku/heroku-buildpack-nodejs.git
        - https://github.com/heroku/heroku-buildpack-nginx.git
    state: ""
```

### Remove a buildpack from an app

Requires these tasks, applied first:

```yaml
- dokku_app:
      app: node-js-app
```

```yaml
dokku_buildpacks:
    app: node-js-app
    buildpacks:
        - https://github.com/heroku/heroku-buildpack-nginx.git
    state: absent
```

### Replace the buildpacks for an app with exactly this list

Requires these tasks, applied first:

```yaml
- dokku_app:
      app: node-js-app
```

```yaml
dokku_buildpacks:
    app: node-js-app
    buildpacks:
        - https://github.com/heroku/heroku-buildpack-nodejs.git
    state: set
```

### Clear all buildpacks from an app

Requires these tasks, applied first:

```yaml
- dokku_app:
      app: node-js-app
```

```yaml
dokku_buildpacks:
    app: node-js-app
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
