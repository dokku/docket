# dokku_acl_service

## Synopsis

Manages the dokku-acl access list for a dokku service

## Requirements

- dokku-acl plugin

## Export support

Not supported - service export is not yet implemented; tracked in a follow-up issue.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `service` | string | yes |  |  | Name of the service instance |
| `type` | string | yes |  |  | Type of service (e.g. redis, postgres) |
| `users` | list | no |  |  | List of users to add or remove from the ACL |
| `state` | string | no | present | present, absent | Desired state of the ACL entries |

## Examples

### Grant users access to a redis service

```yaml
dokku_acl_service:
    service: my-redis
    type: redis
    users:
        - alice
        - bob
```

### Revoke a user's access to a redis service

```yaml
dokku_acl_service:
    service: my-redis
    type: redis
    users:
        - bob
    state: absent
```

### Clear the entire ACL for a redis service

```yaml
dokku_acl_service:
    service: my-redis
    type: redis
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
