# dokku_storage_ensure

## Synopsis

Ensures the storage for a given dokku application

> **Deprecated:** use dokku_storage_entry instead; dokku's storage:ensure-directory has been deprecated in favor of storage:create

## Export support

Not supported - deprecated; storage state is exported via dokku_storage_mount.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `chown` | string | no |  |  | Chown value to set |
| `state` | string | no | present | present, absent | Desired state of the storage |

## Examples

### Ensure a storage directory owned by the herokuish user

```yaml
dokku_storage_ensure:
    app: node-js-app
    chown: herokuish
```

### Ensure a storage directory owned by root

```yaml
dokku_storage_ensure:
    app: node-js-app
    chown: root
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
