# dokku_storage_entry

## Synopsis

Creates or destroys a named storage registry entry

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `name` | string | yes |  |  | Name of the storage entry |
| `path` | string | no |  |  | Host path for the entry (docker-local scheduler; defaults to the dokku storage root joined with the entry name) |
| `scheduler` | string | no | docker-local |  | Scheduler that backs the entry |
| `size` | string | no |  |  | Volume size (scheduler-dependent) |
| `access_mode` | string | no |  |  | Volume access mode (scheduler-dependent) |
| `storage_class` | string | no |  |  | Storage class name (scheduler-dependent) |
| `namespace` | string | no |  |  | Namespace (scheduler-dependent) |
| `chown` | string | no |  |  | Chown value applied when the entry's host directory is created |
| `reclaim_policy` | string | no |  |  | Reclaim policy (scheduler-dependent) |
| `state` | string | no | present | present, absent | Desired state of the storage entry |

## Examples

### Create a docker-local storage entry owned by the herokuish user

```yaml
dokku_storage_entry:
    name: node-js-app-data
    chown: herokuish
```

### Create a storage entry at an explicit host path

```yaml
dokku_storage_entry:
    name: node-js-app-data
    path: /var/lib/dokku/data/storage/node-js-app-data
```

### Destroy a storage entry

```yaml
dokku_storage_entry:
    name: node-js-app-data
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
