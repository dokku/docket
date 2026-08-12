# dokku_traefik_property

## Synopsis

Manages the traefik configuration for a given dokku application

## Export support

Supported.

## Probe support

Partial - the mapped properties are probed; the dynamic `dns-provider-*` family has no report key and plans as drift on every run.

## Identity

Keyed by `app`, `global`, and `property`. Fields left empty are omitted from the address.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | no |  |  | Name of the app. Required if Global is false. |
| `global` | bool | no |  |  | Flag indicating if the traefik configuration should be applied globally |
| `property` | string | yes |  |  | Name of the traefik property to set |
| `value` | string | no |  |  | Value to set for the traefik property |
| `state` | string | no | present | present, absent | Desired state of the traefik configuration |

## Properties

`property` accepts one of the following, applied with `dokku traefik:set`. A property with no form in a scope is rejected there, matching dokku's own rejection.

| Property | Scopes | Report key (app) | Report key (global) |
| --- | --- | --- | --- |
| `api-enabled` | global |  | `global-api-enabled` |
| `api-entry-point` | global |  | `global-api-entry-point` |
| `api-entry-point-address` | global |  | `global-api-entry-point-address` |
| `api-vhost` | global |  | `global-api-vhost` |
| `basic-auth-password` (sensitive) | global |  | `global-basic-auth-password` |
| `basic-auth-username` | global |  | `global-basic-auth-username` |
| `challenge-mode` | global |  | `global-challenge-mode` |
| `dashboard-enabled` | global |  | `global-dashboard-enabled` |
| `dns-provider` | global |  | `global-dns-provider` |
| `http-entry-point` | global |  | `global-http-entry-point` |
| `https-entry-point` | global |  | `global-https-entry-point` |
| `image` | global |  | `global-image` |
| `letsencrypt-email` | global |  | `global-letsencrypt-email` |
| `letsencrypt-server` | global |  | `global-letsencrypt-server` |
| `log-level` | global |  | `global-log-level` |

Names starting with `dns-provider-` are also accepted. dokku validates them through `traefik:set` rather than through its report schema, so they cannot be listed above. The plugin does not report them, so they are applied on every run and never converge. Their values are treated as secrets and masked.

## Examples

### Setting the letsencrypt email globally

```yaml
dokku_traefik_property:
    app: ""
    global: true
    property: letsencrypt-email
    value: admin@example.com
```

### Setting the log level globally

```yaml
dokku_traefik_property:
    app: ""
    global: true
    property: log-level
    value: INFO
```

### Clearing the letsencrypt email globally

```yaml
dokku_traefik_property:
    app: ""
    global: true
    property: letsencrypt-email
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
