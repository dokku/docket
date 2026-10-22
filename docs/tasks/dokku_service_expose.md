# dokku_service_expose

## Synopsis

Exposes or unexposes a dokku service on host ports

## Requirements

- Dokku >= 0.38.31
- a dokku datastore service plugin >= 2.0.0 matching the service type (e.g. dokku-postgres, dokku-redis, dokku-mysql)

## Export support

Supported.

## Probe support

Supported.

## Lifecycle

Applied with state `set` and torn down with state `clear`, which refuses `ports`, so a teardown resets it. See [Applying and tearing down a task](../embedding.md#applying-and-tearing-down-a-task).

## Identity

Keyed by `service` and `name`. Fields left empty are omitted from the address. Manages the whole `ports` collection; entries are identified by value.

## Objects

- The datastore service named by `service` and `name`.

Tasks applied at once on the same object take turns; see [Running tasks concurrently](../embedding.md#running-tasks-concurrently).

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `service` | string | yes |  |  | Type of service to expose (e.g. redis, postgres, mysql) |
| `name` | string | yes |  |  | Name of the service instance |
| `ports` | list | no |  |  | Host ports to expose the service on (order is significant) (required when state is present) (must not be set when state is clear) |
| `state` | string | no | present | present, absent, set, clear | Desired state of the service exposure |

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

### Expose a postgres service named my-db on exactly these host ports

```yaml
dokku_service_expose:
    service: postgres
    name: my-db
    ports:
        - "5433"
    state: set
```

### Unexpose a postgres service named my-db

```yaml
dokku_service_expose:
    service: postgres
    name: my-db
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
