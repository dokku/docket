# dokku_http_auth

## Synopsis

Manages HTTP authentication for a given dokku application

## Requirements

- dokku-http-auth plugin >= 0.13.0

## Export support

Partial - the enabled state is exported; the seeded credentials are not carried on this task and come back as dokku_http_auth_user htpasswd hashes.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `username` | string | no |  |  | HTTP auth username to seed when enabling; supplied together with password |
| `password` | string | no |  |  | HTTP auth password for the seeded username; supplied together with username (sensitive) |
| `state` | string | no | present | present, absent | State of the HTTP auth |

## Examples

### Enable HTTP authentication for an app

```yaml
dokku_http_auth:
    app: hello-world
    username: admin
    password: secret
```

### Enable HTTP authentication without seeding a user

```yaml
dokku_http_auth:
    app: hello-world
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
