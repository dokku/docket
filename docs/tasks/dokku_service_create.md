# dokku_service_create

## Synopsis

Creates or destroys a dokku service

## Requirements

- a dokku datastore service plugin matching the service type (e.g. dokku-postgres, dokku-redis, dokku-mysql)

## Export support

Partial - the service and the image it is running are exported; the remaining create-time options (config_options, custom_env, memory, shm_size, the networks, and the passwords) have no read command and must be re-supplied.

## Probe support

Partial - the service's existence and image are probed; config_options, custom_env, memory, shm_size, the networks, and the passwords have no read command and are not drift-detected.

## Identity

Keyed by `service` and `name`. Fields left empty are omitted from the address.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `service` | string | yes |  |  | Type of service to create (e.g. redis, postgres, mysql) |
| `name` | string | yes |  |  | Name of the service instance |
| `image` | string | no |  |  | Image to start the service with, e.g. postgis/postgis. Applied only when the service is created. |
| `image_version` | string | no |  |  | Image tag to start the service with. Applied only when the service is created. |
| `config_options` | string | no |  |  | Extra arguments to pass to the container create command. Applied only when the service is created. |
| `custom_env` | dict | no |  |  | Map of environment variables to start the service with. Applied only when the service is created. (sensitive) |
| `memory` | int | no |  |  | Container memory limit in megabytes. Applied only when the service is created. |
| `shm_size` | string | no |  |  | Shared memory size for the service container. Applied only when the service is created. |
| `initial_network` | string | no |  |  | Network to attach the service to initially. Applied only when the service is created. |
| `post_create_network` | list | no |  |  | Networks to attach the service container to after creation. Applied only when the service is created. |
| `post_start_network` | list | no |  |  | Networks to attach the service container to after start. Applied only when the service is created. |
| `password` | string | no |  |  | Override the user-level service password. Applied only when the service is created. (sensitive) |
| `root_password` | string | no |  |  | Override the root-level service password. Applied only when the service is created. (sensitive) |
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

### Create a redis service on a pinned image

```yaml
dokku_service_create:
    service: redis
    name: my-pinned-redis
    image: redis
    image_version: 7.2.5
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
