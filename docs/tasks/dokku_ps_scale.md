# dokku_ps_scale

## Synopsis

Manages the process scale for a given dokku application

## Export support

Supported.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `scale` | dict | yes |  |  | Map of process types to quantities |
| `skip_deploy` | bool | no | false |  | Skip the corresponding deploy |
| `state` | string | no | present | present | Desired state of the process scale |

## Examples

### Scale web and worker processes

```yaml
dokku_ps_scale:
    app: hello-world
    scale:
        web: 2
        worker: 1
```

### Scale web and worker processes without deploy

```yaml
dokku_ps_scale:
    app: hello-world
    scale:
        web: 4
        worker: 4
    skip_deploy: true
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
