# dokku_storage_mount

## Synopsis

Attaches or detaches storage on a dokku application

## Export support

Supported.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `entry_name` | string | no |  |  | Named storage registry entry to attach (mutually exclusive with host_dir) |
| `host_dir` | string | no |  |  | Host directory to mount in the legacy bind-mount form (mutually exclusive with entry_name) |
| `container_dir` | string | yes |  |  | Container directory to mount |
| `phases` | list | no |  |  | Deployment phases the attachment applies to (deploy, run). Empty defers to the dokku default. |
| `process_type` | string | no |  |  | Process type the attachment applies to |
| `subpath` | string | no |  |  | Subpath within the entry to mount |
| `readonly` | bool | no |  |  | Mount the attachment as read-only |
| `volume_chown` | string | no |  |  | Chown option applied to the volume at mount time |
| `volume_options` | string | no |  |  | Comma-separated mount options applied to the attachment (e.g. 'Z' for SELinux, 'noexec,nosuid', NFS opts) |
| `state` | string | no | present | present, absent | Desired state of the storage |

## Examples

### Attach a named storage entry to an app

```yaml
dokku_storage_mount:
    app: node-js-app
    entry_name: node-js-app-data
    container_dir: /app/storage
```

### Attach a named entry on deploy only, read-only, for the web process

```yaml
dokku_storage_mount:
    app: node-js-app
    entry_name: node-js-app-data
    container_dir: /app/storage
    phases:
        - deploy
    process_type: web
    readonly: true
```

### Mount a host directory into an app (legacy form)

```yaml
dokku_storage_mount:
    app: node-js-app
    host_dir: /var/lib/dokku/data/storage/node-js-app
    container_dir: /app/storage
```

### Mount a host directory with SELinux relabeling

```yaml
dokku_storage_mount:
    app: node-js-app
    host_dir: /var/lib/dokku/data/storage/node-js-app
    container_dir: /app/storage
    volume_options: Z
```

### Attach a named entry with mount options

```yaml
dokku_storage_mount:
    app: node-js-app
    entry_name: node-js-app-data
    container_dir: /app/storage
    volume_options: noexec,nosuid
```

### Unmount a named entry from an app

```yaml
dokku_storage_mount:
    app: node-js-app
    entry_name: node-js-app-data
    container_dir: /app/storage
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
