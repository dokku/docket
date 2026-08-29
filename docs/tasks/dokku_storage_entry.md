# dokku_storage_entry

## Synopsis

Creates or destroys a named storage registry entry

## Export support

Partial - every field the task accepts is exported; an entry's directory mode is not, since the task has no mode field yet (tracked in dokku/docket#480).

## Probe support

Supported - every field is compared against what storage:list-entries records for the entry, which is the recorded chown rather than the host directory's ownership on disk; a directory chowned out of band is not detected.

## Identity

Keyed by `name`.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `name` | string | yes |  |  | Name of the storage entry |
| `path` | string | no |  |  | Host path for the entry: an absolute path, or a docker named volume on docker-local. Defaults to the dokku storage root joined with the entry name. Cannot be changed on an entry that exists; a recipe that disagrees with the recorded path is reported as an error |
| `scheduler` | string | no | docker-local | docker-local, k3s | Scheduler that backs the entry. Cannot be changed on an entry that exists; a recipe that disagrees with the recorded scheduler is reported as an error |
| `size` | string | no |  |  | Volume size (k3s scheduler; required there and rejected on docker-local). Converged on an entry that exists |
| `access_mode` | string | no |  | ReadWriteOnce, ReadOnlyMany, ReadWriteMany, ReadWriteOncePod | Volume access mode (k3s scheduler; rejected on docker-local). Cannot be changed on an entry that exists, since kubernetes cannot rebind a bound claim; a recipe that disagrees with the recorded value is reported as an error |
| `storage_class` | string | no |  |  | Storage class name (k3s scheduler; rejected on docker-local, and mutually exclusive with path). Cannot be changed on an entry that exists; a recipe that disagrees with the recorded value is reported as an error |
| `namespace` | string | no |  |  | Namespace (scheduler-dependent). Converged on an entry that exists |
| `chown` | string | no |  | heroku, herokuish, paketo, root, false | Ownership applied to the entry's host directory: an ownership preset or a numeric uid (0-65535). dokku sets the owner and the group to the same id, and refuses the value unless the entry sits at its default host path. Converged on an entry that exists, which re-runs the chown on a docker-local directory |
| `reclaim_policy` | string | no |  | Retain, Delete | Reclaim policy applied to the underlying volume (k3s scheduler). Converged on an entry that exists |
| `annotations` | dict | no |  |  | Map of annotations set on the underlying volume (k3s scheduler). Converged one key at a time on an entry that exists, so a key the recipe omits is left alone |
| `labels` | dict | no |  |  | Map of labels set on the underlying volume (k3s scheduler). Converged one key at a time on an entry that exists, so a key the recipe omits is left alone |
| `state` | string | no | present | present, absent | Desired state of the storage entry |

## Examples

### Create a docker-local storage entry owned by the herokuish user

```yaml
dokku_storage_entry:
    name: node-js-app-data
    chown: herokuish
```

### Change an entry's ownership, leaving the attributes the recipe does not name alone

```yaml
dokku_storage_entry:
    name: node-js-app-data
    chown: root
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
