# dokku_acl_service

## Synopsis

Manages the dokku-acl access list for a dokku service

## Requirements

- Dokku >= 0.38.31
- dokku-acl plugin >= 2.0.1

## Export support

Supported.

## Probe support

Supported.

## Lifecycle

Applied with state `set` and torn down with state `clear`, which refuses `users`, so a teardown resets it. See [Applying and tearing down a task](../embedding.md#applying-and-tearing-down-a-task).

## Identity

Keyed by `service` and `type`. Fields left empty are omitted from the address. Manages the whole `users` collection; entries are identified by value.

## Objects

- The datastore service named by `type` and `service`.

Tasks applied at once on the same object take turns; see [Running tasks concurrently](../embedding.md#running-tasks-concurrently).

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `service` | string | yes |  |  | Name of the service instance |
| `type` | string | yes |  |  | Type of service (e.g. redis, postgres) |
| `users` | list | no |  |  | List of users to add, remove, or set on the ACL (required when state is present or absent) (must not be set when state is clear) |
| `state` | string | no | present | present, absent, set, clear | Desired state of the ACL entries |

## Examples

### Grant users access to a redis service

Requires these tasks, applied first:

```yaml
- dokku_plugin:
      name: acl
      url: https://github.com/dokku-community/dokku-acl.git
- dokku_plugin:
      name: redis
      url: https://github.com/dokku/dokku-redis.git
- dokku_service_create:
      service: redis
      name: my-redis
```

```yaml
dokku_acl_service:
    service: my-redis
    type: redis
    users:
        - alice
        - bob
```

### Revoke a user's access to a redis service

Requires these tasks, applied first:

```yaml
- dokku_plugin:
      name: acl
      url: https://github.com/dokku-community/dokku-acl.git
- dokku_plugin:
      name: redis
      url: https://github.com/dokku/dokku-redis.git
- dokku_service_create:
      service: redis
      name: my-redis
```

```yaml
dokku_acl_service:
    service: my-redis
    type: redis
    users:
        - bob
    state: absent
```

### Replace the users with access to a redis service

Requires these tasks, applied first:

```yaml
- dokku_plugin:
      name: acl
      url: https://github.com/dokku-community/dokku-acl.git
- dokku_plugin:
      name: redis
      url: https://github.com/dokku/dokku-redis.git
- dokku_service_create:
      service: redis
      name: my-redis
```

```yaml
dokku_acl_service:
    service: my-redis
    type: redis
    users:
        - alice
    state: set
```

### Clear the entire ACL for a redis service

Requires these tasks, applied first:

```yaml
- dokku_plugin:
      name: acl
      url: https://github.com/dokku-community/dokku-acl.git
- dokku_plugin:
      name: redis
      url: https://github.com/dokku/dokku-redis.git
- dokku_service_create:
      service: redis
      name: my-redis
```

```yaml
dokku_acl_service:
    service: my-redis
    type: redis
    state: clear
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
