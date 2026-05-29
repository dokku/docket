# dokku_storage_mount

## Synopsis

Mounts or unmounts the storage for a given dokku application

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `host_dir` | string | yes |  |  | Host directory to mount |
| `container_dir` | string | yes |  |  | Container directory to mount |
| `state` | string | no | present | present, absent | Desired state of the storage |

## Examples

### Mount a host directory into an app

```yaml
dokku_storage_mount:
    app: node-js-app
    host_dir: /var/lib/dokku/data/storage/node-js-app
    container_dir: /app/storage
```

### Unmount a host directory from an app

```yaml
dokku_storage_mount:
    app: node-js-app
    host_dir: /var/lib/dokku/data/storage/node-js-app
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
