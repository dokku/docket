# dokku_http_auth_user

## Synopsis

Manages the set of HTTP auth users for a dokku application

## Requirements

- dokku-http-auth plugin

## Export support

Partial - usernames are exported; each password is not readable and becomes a required input.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `users` | list | no |  |  | List of HTTP auth users to add or remove. Each item has: username, password. |
| `update_password` | bool | no | false |  | Re-issue add-user for users that already exist so their password converges |
| `state` | string | no | present | present, absent | Desired state of the HTTP auth users |

## Examples

### Add HTTP auth users to an app

```yaml
dokku_http_auth_user:
    app: hello-world
    users:
        - username: admin
          password: secret
        - username: ops
          password: hunter2
```

### Rotate an existing user's password

```yaml
dokku_http_auth_user:
    app: hello-world
    users:
        - username: admin
          password: new-secret
    update_password: true
```

### Remove a user from an app

```yaml
dokku_http_auth_user:
    app: hello-world
    users:
        - username: ops
    state: absent
```

### Remove all HTTP auth users from an app

```yaml
dokku_http_auth_user:
    app: hello-world
    users: []
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
