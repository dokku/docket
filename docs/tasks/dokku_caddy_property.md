# dokku_caddy_property

## Synopsis

Manages the caddy configuration for a given dokku application

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
| `global` | bool | no |  |  | Flag indicating if the caddy configuration should be applied globally |
| `property` | string | yes |  |  | Name of the caddy property to set |
| `value` | string | no |  |  | Value to set for the caddy property |
| `state` | string | no | present | present, absent | Desired state of the caddy configuration |

## Properties

`property` accepts one of the following, applied with `dokku caddy:set`. A property with no form in a scope is rejected there, matching dokku's own rejection.

| Property | Scopes | Report key (app) | Report key (global) |
| --- | --- | --- | --- |
| `image` | global |  | `global-image` |
| `letsencrypt-email` | global |  | `global-letsencrypt-email` |
| `letsencrypt-server` | global |  | `global-letsencrypt-server` |
| `log-level` | global |  | `global-log-level` |
| `polling-interval` | global |  | `global-polling-interval` |
| `tls-internal` | app, global | `tls-internal` | `global-tls-internal` |

## Examples

### Enabling internal TLS for an app

```yaml
dokku_caddy_property:
    app: node-js-app
    property: tls-internal
    value: "true"
```

### Setting the letsencrypt email globally

```yaml
dokku_caddy_property:
    app: ""
    global: true
    property: letsencrypt-email
    value: admin@example.com
```

### Clearing internal TLS for an app

```yaml
dokku_caddy_property:
    app: node-js-app
    property: tls-internal
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
