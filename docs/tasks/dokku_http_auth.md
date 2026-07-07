# dokku_http_auth

## Synopsis

Manages HTTP authentication for a given dokku application

## Requirements

- dokku-http-auth plugin

## Export support

Partial - credentials are not readable and become required inputs.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `username` | string | no |  |  | HTTP auth username |
| `password` | string | no |  |  | HTTP auth password (sensitive) |
| `state` | string | no | present | present, absent | State of the HTTP auth |

## Examples

### Enable HTTP authentication for an app

```yaml
dokku_http_auth:
    app: hello-world
    username: admin
    password: secret
```

### Disable HTTP authentication for an app

```yaml
dokku_http_auth:
    app: hello-world
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
