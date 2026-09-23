# dokku_storage_mount

## Synopsis

Attaches, detaches or replaces storage mounts on a dokku application

## Export support

Supported.

## Probe support

Partial - the mounts list is probed in full; the single-mount form probes the mount source, container path, process type, and volume options, while its phases, subpath, readonly, and volume_chown apply at mount time and are not drift-detected.

## Identity

Keyed by `app`, `container_dir`, and `process_type`. Fields left empty are omitted from the address. Manages the whole `mounts` collection; entries are identified by `container_dir`.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `entry_name` | string | no |  |  | Named storage registry entry to attach (mutually exclusive with host_dir and mounts) |
| `host_dir` | string | no |  |  | Host directory to mount in the legacy bind-mount form (mutually exclusive with entry_name and mounts) |
| `container_dir` | string | no |  |  | Container directory to mount; required unless mounts is used |
| `phases` | list | no |  |  | Deployment phases the attachment applies to (deploy, run). Empty defers to the dokku default. |
| `process_type` | string | no |  |  | Process type the attachment applies to. With mounts, the process type whose mounts the list describes; mounts of other process types are left alone. Empty means dokku's _default_ scope. |
| `subpath` | string | no |  |  | Subpath within the entry to mount |
| `readonly` | bool | no |  |  | Mount the attachment as read-only |
| `volume_chown` | string | no |  |  | Chown option applied to the volume at mount time |
| `volume_options` | string | no |  |  | Comma-separated mount options applied to the attachment (e.g. 'Z' for SELinux, 'noexec,nosuid', NFS opts) |
| `mounts` | list | no |  |  | Attachments of the process_type scope, instead of the single-mount fields. Under state 'set' the list is the scope's complete set of mounts; 'present' adds or updates the listed mounts and 'absent' removes them, leaving the rest of the scope in place; omit for state 'clear'. Each item has: entry_name, host_dir, container_dir, phases, subpath, readonly, volume_chown, volume_options. |
| `state` | string | no | present | present, absent, set, clear | Desired state of the storage. 'set' and 'clear' require the mounts list form. |

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
    container_dir: /app/assets
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
    container_dir: /app/uploads
```

### Mount a host directory with SELinux relabeling

```yaml
dokku_storage_mount:
    app: node-js-app
    host_dir: /var/lib/dokku/data/storage/node-js-app
    container_dir: /app/shared
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

### Declare the complete set of mounts for an app, removing any other

```yaml
dokku_storage_mount:
    app: node-js-app
    mounts:
        - entry_name: node-js-app-data
          container_dir: /app/storage
        - host_dir: /var/lib/dokku/data/storage/node-js-app
          container_dir: /app/uploads
          volume_options: Z
    state: set
```

### Declare the web process's mounts, each with its own subpath

```yaml
dokku_storage_mount:
    app: node-js-app
    process_type: web
    mounts:
        - entry_name: node-js-app-data
          container_dir: /app/assets
          phases:
            - deploy
          subpath: assets
          readonly: true
        - entry_name: node-js-app-data
          container_dir: /app/cache
          subpath: cache
    state: set
```

### Add mounts to an app, leaving its other mounts in place

```yaml
dokku_storage_mount:
    app: node-js-app
    mounts:
        - entry_name: node-js-app-data
          container_dir: /app/tmp
          volume_options: noexec,nosuid
```

### Remove mounts from an app, leaving its other mounts in place

```yaml
dokku_storage_mount:
    app: node-js-app
    mounts:
        - entry_name: node-js-app-data
          container_dir: /app/tmp
    state: absent
```

### Remove every mount of the default process type

```yaml
dokku_storage_mount:
    app: node-js-app
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
