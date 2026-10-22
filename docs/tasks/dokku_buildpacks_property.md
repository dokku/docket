# dokku_buildpacks_property

## Synopsis

Manages the buildpacks configuration for a given dokku application

## Requirements

- Dokku >= 0.38.8

## Export support

Supported.

## Probe support

Supported.

## Lifecycle

Applied with state `present` and torn down with state `absent`, which refuses `value`, so a teardown resets it. See [Applying and tearing down a task](../embedding.md#applying-and-tearing-down-a-task).

## Identity

Keyed by `app`, `global`, and `property`. Fields left empty are omitted from the address.

## Objects

- The app named by `app`.

Tasks applied at once on the same object take turns; see [Running tasks concurrently](../embedding.md#running-tasks-concurrently).

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | no |  |  | Name of the app |
| `global` | bool | no |  |  | Flag indicating if the property should be applied globally |
| `property` | string | yes |  |  | Name of the property to set |
| `value` | string | no |  |  | Value to set for the property (required when state is present) (must not be set when state is absent) |
| `state` | string | no | present | present, absent | Desired state of the property |

## Constraints

- Exactly one of `app` or `global` must be set.

## Properties

`property` accepts one of the following, applied with `dokku buildpacks:set-property`. A property with no form in a scope is rejected there, matching dokku's own rejection.

| Property | Scopes | Report key (app) | Report key (global) |
| --- | --- | --- | --- |
| `stack` | app, global | `stack` | `global-stack` |

## Examples

### Setting the stack value for an app

Requires these tasks, applied first:

```yaml
- dokku_app:
      app: node-js-app
```

```yaml
dokku_buildpacks_property:
    app: node-js-app
    property: stack
    value: gliderlabs/herokuish:latest
```

### Setting the stack value globally

```yaml
dokku_buildpacks_property:
    app: ""
    global: true
    property: stack
    value: gliderlabs/herokuish:latest
```

### Clearing the stack value for an app

Requires these tasks, applied first:

```yaml
- dokku_app:
      app: node-js-app
```

```yaml
dokku_buildpacks_property:
    app: node-js-app
    property: stack
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
