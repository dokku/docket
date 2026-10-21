# dokku_service_property

## Synopsis

Manages a property for a given dokku service

## Requirements

- Dokku >= 0.38.31
- a dokku datastore service plugin >= 2.3.1 matching the service type (dokku-datastore 0.7.1, e.g. dokku-postgres, dokku-redis, dokku-mysql)

## Export support

Partial - every property set on a service is exported except the ones another task owns (the networks, config-options, custom-env, memory and shm-size belong to dokku_service_create; the backup schedule, bucket, IAM flag, region, signature version, endpoint and credentials to dokku_service_backup); the backup credentials are reported only as fingerprints, so they must be re-supplied.

## Probe support

Supported.

## Identity

Keyed by `service`, `name`, and `property`. Fields left empty are omitted from the address.

## Objects

- The datastore service named by `service` and `name`.

Tasks applied at once on the same object take turns; see [Running tasks concurrently](../embedding.md#running-tasks-concurrently).

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `service` | string | yes |  |  | Type of service to configure (e.g. redis, postgres, mysql) |
| `name` | string | yes |  |  | Name of the service instance |
| `property` | string | yes |  |  | Name of the property to set. Which properties a service accepts depends on its datastore - the backup settings only on one that can be backed up, for example - and &lt;service>:set refuses the rest when the task applies. |
| `value` | string | no |  |  | Value to set the property to. Masked in output for the backup credentials and custom-env. (required when state is present) (must not be set when state is absent) |
| `state` | string | no | present | present, absent | Desired state of the property |

## Examples

### Set the restart-policy for a postgres service

```yaml
dokku_service_property:
    service: postgres
    name: my-db
    property: restart-policy
    value: always
```

### Publish an exposed postgres service directly rather than through an ambassador

```yaml
dokku_service_property:
    service: postgres
    name: my-db
    property: expose-mode
    value: direct
```

### Clear the restart-policy for a postgres service

```yaml
dokku_service_property:
    service: postgres
    name: my-db
    property: restart-policy
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
