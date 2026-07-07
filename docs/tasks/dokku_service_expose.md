# dokku_service_expose

## Synopsis

Exposes or unexposes a dokku service on host ports

## Requirements

- a dokku datastore service plugin matching the service type (e.g. dokku-postgres, dokku-redis, dokku-mysql)

## Export support

Not supported - service export is not yet implemented; tracked in a follow-up issue.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `service` | string | yes |  |  | Type of service to expose (e.g. redis, postgres, mysql) |
| `name` | string | yes |  |  | Name of the service instance |
| `ports` | list | no |  |  | Host ports to expose the service on. Required when state is present. |
| `state` | string | no | present | present, absent | Desired state of the service exposure |

## Examples

### Expose a postgres service named my-db on host port 5432

```yaml
dokku_service_expose:
    service: postgres
    name: my-db
    ports:
        - "5432"
```

### Expose a redis service named my-redis on host port 6379

```yaml
dokku_service_expose:
    service: redis
    name: my-redis
    ports:
        - "6379"
```

### Unexpose a postgres service named my-db

```yaml
dokku_service_expose:
    service: postgres
    name: my-db
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
