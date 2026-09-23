# dokku_registry_auth

## Synopsis

Manages docker registry authentication for a dokku application or globally

## Export support

Partial - registry:report names the servers a credential exists for, so the logins a server needs are reconstructed; the username and password behind each one are not readable and are lifted into required inputs the caller supplies before apply.

## Probe support

Supported.

## Identity

Keyed by `app`, `global`, and `server`. Fields left empty are omitted from the address.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | no |  |  | Name of the app. Required if Global is false. |
| `global` | bool | no |  |  | Flag indicating if the registry credential should be applied globally |
| `server` | string | yes |  |  | Docker registry hostname (e.g. docker.io, ghcr.io) |
| `username` | string | no |  |  | Registry username (required when state is present) |
| `password` | string | no |  |  | Registry password (required when state is present) (sensitive) |
| `state` | string | no | present | present, absent | Desired state of the registry credential |

## Examples

### Log in to a registry for an app

```yaml
dokku_registry_auth:
    app: node-js-app
    server: ghcr.io
    username: deploy-bot
    password: ghp_examplepat
```

### Log in to a registry globally

```yaml
dokku_registry_auth:
    app: ""
    global: true
    server: docker.io
    username: deploy-bot
    password: examplepassword
```

### Log out from a registry for an app

```yaml
dokku_registry_auth:
    app: node-js-app
    server: ghcr.io
    state: absent
```

### Log out from a registry globally

```yaml
dokku_registry_auth:
    app: ""
    global: true
    server: docker.io
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
