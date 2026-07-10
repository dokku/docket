# dokku_service_property

## Synopsis

Manages a property for a given dokku service

## Requirements

- a dokku datastore service plugin matching the service type (e.g. dokku-postgres, dokku-redis, dokku-mysql)

## Export support

Not supported - no datastore plugin exposes a machine-readable report of the properties set via `<service>:set`, so they cannot be read back (tracked upstream in dokku/dokku-datastore#98).

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `service` | string | yes |  |  | Type of service to configure (e.g. redis, postgres, mysql) |
| `name` | string | yes |  |  | Name of the service instance |
| `property` | string | yes |  |  | Name of the property to set |
| `value` | string | no |  |  | Value to set the property to. Required when state is present. |
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
