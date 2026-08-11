# dokku_http_auth_user

## Synopsis

Manages the set of HTTP auth users for a dokku application

## Requirements

- dokku-http-auth plugin >= 0.13.0

## Export support

Supported.

## Probe support

Partial - an existing user's htpasswd hash is probed; a cleartext password is not readable, so a user that already exists converges only when update_password forces it.

## Identity

Keyed by `app`. Manages the whole `users` collection; entries are identified by `username`.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `users` | list | no |  |  | List of HTTP auth users to add or remove. Each item gives the credential as exactly one of password (cleartext, applied with http-auth:add-user) or hash (an htpasswd entry, applied with http-auth:import-users); both are sensitive. Each item has: username, password, hash. |
| `update_password` | bool | no | false |  | Re-issue add-user for users that already exist so their cleartext password converges. Users given by hash converge on their own and ignore this |
| `state` | string | no | present | present, absent, set | Desired state of the HTTP auth users |

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

### Add a user from an htpasswd hash, as `docket export` emits

```yaml
dokku_http_auth_user:
    app: hello-world
    users:
        - username: deploy
          hash: $6$s0Vd6Ns8Wq2Kx1Lp$Zq8mQ0zH1pR3tY7uJ5bN2cV4dX6fG8hK0lM2nP4rS6tU8vW0xY2zA4bC6dE8fG0
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

### Replace the app's users with exactly this set

```yaml
dokku_http_auth_user:
    app: hello-world
    users:
        - username: deploy
          hash: $6$s0Vd6Ns8Wq2Kx1Lp$Zq8mQ0zH1pR3tY7uJ5bN2cV4dX6fG8hK0lM2nP4rS6tU8vW0xY2zA4bC6dE8fG0
    state: set
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
