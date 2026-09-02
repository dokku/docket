# dokku_letsencrypt_property

## Synopsis

Manages the letsencrypt configuration for a given dokku application

## Requirements

- dokku-letsencrypt plugin >= 0.25.0

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
| `global` | bool | no |  |  | Flag indicating if the property should be applied globally |
| `property` | string | yes |  |  | Name of the property to set |
| `value` | string | no |  |  | Value to set for the property (sensitive) |
| `state` | string | no | present | present, absent | Desired state of the property |

## Properties

`property` accepts one of the following, applied with `dokku letsencrypt:set`. A property with no form in a scope is rejected there, matching dokku's own rejection.

| Property | Scopes | Report key (app) | Report key (global) |
| --- | --- | --- | --- |
| `dns-provider` | app, global | `dns-provider` | `global-dns-provider` |
| `email` | app, global | `email` | `global-email` |
| `graceperiod` | app, global | `graceperiod` | `global-graceperiod` |
| `lego-args` | app, global | `lego-args` | `global-lego-args` |
| `lego-docker-options` | app, global | `lego-docker-options` | `global-lego-docker-options` |
| `server` | app, global | `server` | `global-server` |

Names starting with `dns-provider-` are also accepted in the app and global scopes. dokku validates them through `letsencrypt:set` rather than through its report schema, so they cannot be listed above. The plugin reports each one it has been given, so they probe for drift like any listed property. Their values are treated as secrets and masked.

## Examples

### Setting the letsencrypt email for an app

```yaml
dokku_letsencrypt_property:
    app: node-js-app
    property: email
    value: admin@example.com
```

### Setting the dns provider for an app

```yaml
dokku_letsencrypt_property:
    app: node-js-app
    property: dns-provider
    value: namecheap
```

### Setting a dns-provider-* env var globally

```yaml
dokku_letsencrypt_property:
    app: ""
    global: true
    property: dns-provider-NAMECHEAP_API_USER
    value: deploy-bot
```

### Clearing the letsencrypt email for an app

```yaml
dokku_letsencrypt_property:
    app: node-js-app
    property: email
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
