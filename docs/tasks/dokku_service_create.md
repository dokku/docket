# dokku_service_create

## Synopsis

Creates or destroys a dokku service

## Requirements

- Dokku >= 0.38.31
- a dokku datastore service plugin >= 2.3.1 matching the service type (dokku-datastore 0.7.1, e.g. dokku-postgres, dokku-redis, dokku-mysql)

## Export support

Partial - the service, the image it is running and its create-time options are exported; password and root_password are applied only at create and cannot be read back, so they must be re-supplied.

## Probe support

Partial - the service's existence is probed, along with the image it is running and every create-time option the recipe declares; password and root_password are applied only at create and cannot be read back, so they are not drift-detected.

## Identity

Keyed by `service` and `name`. Fields left empty are omitted from the address.

## Objects

- The datastore service named by `service` and `name`. Creates it, and destroys it in state `absent`. Can restart or redeploy it.

Tasks applied at once on the same object take turns; see [Running tasks concurrently](../embedding.md#running-tasks-concurrently).

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `service` | string | yes |  |  | Type of service to create (e.g. redis, postgres, mysql) |
| `name` | string | yes |  |  | Name of the service instance |
| `image` | string | no |  |  | Image to start the service with, e.g. postgis/postgis. Applied when the service is created; image_drift decides what happens when an existing service is running something else. (must not be set when state is absent) |
| `image_version` | string | no |  |  | Image tag to start the service with. Applied when the service is created; image_drift decides what happens when an existing service is running something else. (must not be set when state is absent) |
| `config_options` | string | no |  |  | Extra arguments to pass to the container create command. Applied when the service is created; image_drift decides what happens when an existing service has a different value. (must not be set when state is absent) |
| `custom_env` | dict | no |  |  | Map of environment variables to start the service with. Applied when the service is created; image_drift decides what happens when an existing service has a different value. (must not be set when state is absent) (sensitive) |
| `memory` | int | no |  |  | Container memory limit in megabytes. Applied when the service is created; image_drift decides what happens when an existing service has a different value. (must not be set when state is absent) |
| `shm_size` | string | no |  |  | Shared memory size for the service container. Applied when the service is created; image_drift decides what happens when an existing service has a different value. (must not be set when state is absent) |
| `initial_network` | string | no |  |  | Network to attach the service to initially. Applied when the service is created; image_drift decides what happens when an existing service has a different value. (must not be set when state is absent) |
| `post_create_network` | list | no |  |  | Networks to attach the service container to after creation. Applied when the service is created; image_drift decides what happens when an existing service has a different value. (order is significant) (must not be set when state is absent) |
| `post_start_network` | list | no |  |  | Networks to attach the service container to after start. Applied when the service is created; image_drift decides what happens when an existing service has a different value. (order is significant) (must not be set when state is absent) |
| `password` | string | no |  |  | Override the user-level service password. Applied only when the service is created. (must not be set when state is absent) (not returned when reading state) (sensitive) |
| `root_password` | string | no |  |  | Override the root-level service password. Applied only when the service is created. (must not be set when state is absent) (not returned when reading state) (sensitive) |
| `image_drift` | string | no | warn | ignore, warn, error, upgrade | What to do when the service already exists on an image other than the one pinned, or with a create-time option other than the one declared: 'ignore' it, 'warn' and leave it alone, 'error' out, or 'upgrade' the service to reconcile it. An image is reconciled with &lt;service>:upgrade, which recreates the container with no data migration across major versions; any other option with &lt;service>:set followed by a stop and a start. Both mean downtime. Options the recipe does not declare are left as the server has them. (must not be set when state is absent) (applied only, not read back) |
| `restart_apps` | bool | no |  |  | Restart the apps linked to the service after an image_drift 'upgrade' runs &lt;service>:upgrade, so they reconnect to the new container. Only valid with image_drift 'upgrade', and only acted on when the image drifted and the service has linked apps: reconciling the other options with a stop and a start leaves linked apps alone. (must not be set when state is absent) (applied only, not read back) |
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

### Reconcile a changed image pin by upgrading the service

```yaml
dokku_service_create:
    service: redis
    name: my-upgraded-redis
    image: redis
    image_version: 7.2.5
    image_drift: upgrade
```

### Reconcile a changed shared memory size with set, stop and start

```yaml
dokku_service_create:
    service: redis
    name: my-tuned-redis
    shm_size: 128m
    image_drift: upgrade
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
