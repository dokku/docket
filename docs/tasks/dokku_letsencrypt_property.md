# dokku_letsencrypt_property

## Synopsis

Manages the letsencrypt configuration for a given dokku application

## Requirements

- dokku-letsencrypt plugin

## Export support

Supported.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | no |  |  | Name of the app. Required if Global is false. |
| `global` | bool | no |  |  | Flag indicating if the letsencrypt configuration should be applied globally |
| `property` | string | yes |  |  | Name of the letsencrypt property to set |
| `value` | string | no |  |  | Value to set for the letsencrypt property (sensitive) |
| `state` | string | no | present | present, absent | Desired state of the letsencrypt configuration |

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
