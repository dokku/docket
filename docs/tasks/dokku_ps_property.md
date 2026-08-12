# dokku_ps_property

## Synopsis

Manages the ps configuration for a given dokku application

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
| `global` | bool | no |  |  | Flag indicating if the ps configuration should be applied globally |
| `property` | string | yes |  |  | Name of the ps property to set |
| `value` | string | no |  |  | Value to set for the ps property |
| `state` | string | no | present | present, absent | Desired state of the ps configuration |

## Properties

`property` accepts one of the following, applied with `dokku ps:set`. A property with no form in a scope is rejected there, matching dokku's own rejection.

| Property | Scopes | Report key (app) | Report key (global) |
| --- | --- | --- | --- |
| `dockerfile-start-cmd` | app | `dockerfile-start-cmd` |  |
| `procfile-path` | app, global | `procfile-path` | `global-procfile-path` |
| `restart-policy` | app, global | `restart-policy` | `global-restart-policy` |
| `skip-deploy` | app, global | `skip-deploy` | `global-skip-deploy` |
| `start-cmd` | app | `start-cmd` |  |
| `stop-timeout-seconds` | app, global | `stop-timeout-seconds` | `global-stop-timeout-seconds` |

## Examples

### Setting the restart-policy value for an app

```yaml
dokku_ps_property:
    app: node-js-app
    property: restart-policy
    value: on-failure:5
```

### Setting the restart-policy value globally

```yaml
dokku_ps_property:
    app: ""
    global: true
    property: restart-policy
    value: on-failure:5
```

### Clearing the restart-policy value for an app

```yaml
dokku_ps_property:
    app: node-js-app
    property: restart-policy
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
