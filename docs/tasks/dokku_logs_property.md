# dokku_logs_property

## Synopsis

Manages the logs configuration for a given dokku application

## Export support

Supported.

## Probe support

Supported.

## Identity

Keyed by `app`, `global`, and `property`. Fields left empty are omitted from the address.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | no |  |  | Name of the app. Required if Global is false. |
| `global` | bool | no |  |  | Flag indicating if the logs configuration should be applied globally |
| `property` | string | yes |  |  | Name of the logs property to set |
| `value` | string | no |  |  | Value to set for the logs property |
| `state` | string | no | present | present, absent | Desired state of the logs configuration |

## Properties

`property` accepts one of the following, applied with `dokku logs:set`. A property with no form in a scope is rejected there, matching dokku's own rejection.

| Property | Scopes | Report key (app) | Report key (global) |
| --- | --- | --- | --- |
| `app-label-alias` | app, global | `app-label-alias` | `global-app-label-alias` |
| `max-size` | app, global | `max-size` | `global-max-size` |
| `vector-image` | global |  | `global-vector-image` |
| `vector-networks` | global |  | `global-vector-networks` |
| `vector-sink` | app, global | `vector-sink` | `global-vector-sink` |

## Examples

### Setting the max-size value for an app

```yaml
dokku_logs_property:
    app: node-js-app
    property: max-size
    value: 100m
```

### Setting the max-size value globally

```yaml
dokku_logs_property:
    app: ""
    global: true
    property: max-size
    value: 100m
```

### Clearing the max-size value for an app

```yaml
dokku_logs_property:
    app: node-js-app
    property: max-size
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
