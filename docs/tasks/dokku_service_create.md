# dokku_service_create

## Synopsis

Creates or destroys a dokku service

## Requirements

- a dokku datastore service plugin matching the service type (e.g. dokku-postgres, dokku-redis, dokku-mysql)

## Export support

Supported.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `service` | string | yes |  |  | Type of service to create (e.g. redis, postgres, mysql) |
| `name` | string | yes |  |  | Name of the service instance |
| `state` | string | no | present | present, absent | Desired state of the service |

## Examples

### Create a redis service named my-redis

```yaml
dokku_service_create:
    service: redis
    name: my-redis
```

### Create a postgres service named my-db

```yaml
dokku_service_create:
    service: postgres
    name: my-db
```

### Destroy a redis service named my-redis

```yaml
dokku_service_create:
    service: redis
    name: my-redis
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
