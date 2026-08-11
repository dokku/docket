# dokku_storage_entry

## Synopsis

Creates or destroys a named storage registry entry

## Export support

Supported.

## Probe support

Partial - idempotency is keyed on the entry name; scheduler, size, and chown changes to an existing entry are not drift-detected (tracked in dokku/docket#439).

## Identity

Keyed by `name`.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `name` | string | yes |  |  | Name of the storage entry |
| `path` | string | no |  |  | Host path for the entry: an absolute path, or a docker named volume on docker-local. Defaults to the dokku storage root joined with the entry name |
| `scheduler` | string | no | docker-local | docker-local, k3s | Scheduler that backs the entry |
| `size` | string | no |  |  | Volume size (k3s scheduler; required there and rejected on docker-local) |
| `access_mode` | string | no |  | ReadWriteOnce, ReadOnlyMany, ReadWriteMany, ReadWriteOncePod | Volume access mode (k3s scheduler; rejected on docker-local) |
| `storage_class` | string | no |  |  | Storage class name (k3s scheduler; rejected on docker-local, and mutually exclusive with path) |
| `namespace` | string | no |  |  | Namespace (scheduler-dependent) |
| `chown` | string | no |  | heroku, herokuish, paketo, root, false | Ownership applied when the entry's host directory is created: an ownership preset or a numeric uid (0-65535). dokku sets the owner and the group to the same id, and refuses the value unless the entry sits at its default host path |
| `reclaim_policy` | string | no |  | Retain, Delete | Reclaim policy applied to the underlying volume (k3s scheduler) |
| `annotations` | dict | no |  |  | Map of annotations set on the underlying volume (k3s scheduler) |
| `labels` | dict | no |  |  | Map of labels set on the underlying volume (k3s scheduler) |
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
