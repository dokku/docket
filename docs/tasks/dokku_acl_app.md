# dokku_acl_app

## Synopsis

Manages the dokku-acl access list for a dokku application

## Requirements

- dokku-acl plugin

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `users` | list | no |  |  | List of users to add or remove from the ACL |
| `state` | string | no | present | present, absent | Desired state of the ACL entries |

## Examples

### Grant users access to an app

```yaml
dokku_acl_app:
    app: node-js-app
    users:
        - alice
        - bob
```

### Revoke a user's access to an app

```yaml
dokku_acl_app:
    app: node-js-app
    users:
        - bob
    state: absent
```

### Clear the entire ACL for an app

```yaml
dokku_acl_app:
    app: node-js-app
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
