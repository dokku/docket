# dokku_config

## Synopsis

Manages the configuration for a given dokku application

## Export support

Partial - config values are written to the companion vars-file.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `restart` | bool | no | true |  | Flag indicating if the app should be restarted |
| `config` | dict | no |  |  | Map of configuration key-value pairs |
| `state` | string | no | present | present, absent | Desired state of the configuration |

## Examples

### set KEY=VALUE

```yaml
dokku_config:
    app: hello-world
    restart: true
    config:
        KEY: VALUE_1
```

### set KEY=VALUE without restart

```yaml
dokku_config:
    app: hello-world
    restart: false
    config:
        KEY: VALUE_1
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
