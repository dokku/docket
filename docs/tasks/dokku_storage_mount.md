# dokku_storage_mount

## Synopsis

Attaches, detaches or replaces storage mounts on a dokku application

## Requirements

- Dokku >= 0.38.31

## Export support

Supported.

## Probe support

Supported.

## Lifecycle

Applied with state `set` and torn down with state `clear`, which refuses `entry_name`, `host_dir`, `container_dir`, `phases`, `subpath`, `readonly`, `volume_chown`, `volume_options`, and `mounts`, so a teardown resets them. See [Applying and tearing down a task](../embedding.md#applying-and-tearing-down-a-task).

## Identity

Keyed by `app`, `container_dir`, and `process_type`. Fields left empty are omitted from the address. Manages the whole `mounts` collection; entries are identified by `container_dir`.

## Objects

- The app named by `app`.

Tasks applied at once on the same object take turns; see [Running tasks concurrently](../embedding.md#running-tasks-concurrently).

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `entry_name` | string | no |  |  | Named storage registry entry to attach (must not be set when state is set or clear) |
| `host_dir` | string | no |  |  | Host directory to mount in the legacy bind-mount form (must not be set when state is set or clear) |
| `container_dir` | string | no |  |  | Container directory to mount (must not be set when state is set or clear) |
| `phases` | list | no |  |  | Deployment phases the attachment applies to (deploy, run). Empty defers to the dokku default. (no duplicates) (must not be set when state is set or clear) |
| `process_type` | string | no |  |  | Process type the attachment applies to. With mounts, the process type whose mounts the list describes; mounts of other process types are left alone. Empty means dokku's _default_ scope. |
| `subpath` | string | no |  |  | Subpath within the entry to mount (must not be set when state is set or clear) |
| `readonly` | bool | no |  |  | Mount the attachment as read-only (must not be set when state is set or clear) |
| `volume_chown` | string | no |  |  | Chown option applied to the volume at mount time (must not be set when state is set or clear) |
| `volume_options` | string | no |  |  | Comma-separated mount options applied to the attachment (e.g. 'Z' for SELinux, 'noexec,nosuid', NFS opts) (must not be set when state is set or clear) |
| `mounts` | list | no |  |  | Attachments of the process_type scope, instead of the single-mount fields. Under state 'set' the list is the scope's complete set of mounts; 'present' adds or updates the listed mounts and 'absent' removes them, leaving the rest of the scope in place. Each item has: entry_name, host_dir, container_dir, phases, subpath, readonly, volume_chown, volume_options. (must not be set when state is clear) |
| `state` | string | no | present | present, absent, set, clear | Desired state of the storage |

## Constraints

- `mounts` must not be set together with `entry_name`, `host_dir`, `container_dir`, `phases`, `subpath`, `readonly`, `volume_chown` or `volume_options`.
- At least one of `mounts`, `entry_name` or `host_dir` must be set when `state` is `present` or `absent`.
- `entry_name` must not be set together with `host_dir`.
- `entry_name` requires `container_dir`.
- `host_dir` requires `container_dir`.
- Each `mounts` item: exactly one of `entry_name` or `host_dir` must be set.

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
