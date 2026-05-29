# dokku_proxy_toggle

## Synopsis

Enables or disables the proxy plugin for a given dokku application

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `global` | bool | no |  |  | Flag indicating if the proxy should be applied globally |
| `state` | string | no | present | present, absent | Desired state of the proxy |

## Examples

### Enable the proxy for an app

```yaml
dokku_proxy_toggle:
    app: node-js-app
```

### Disable the proxy for an app

```yaml
dokku_proxy_toggle:
    app: node-js-app
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
