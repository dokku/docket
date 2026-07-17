# dokku_builder_railpack_property

## Synopsis

Manages the builder-railpack configuration for a given dokku application

## Export support

Supported.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | no |  |  | Name of the app. Required if Global is false. |
| `global` | bool | no |  |  | Flag indicating if the builder-railpack configuration should be applied globally |
| `property` | string | yes |  |  | Name of the builder-railpack property to set |
| `value` | string | no |  |  | Value to set for the builder-railpack property |
| `state` | string | no | present | present, absent | Desired state of the builder-railpack configuration |

## Examples

### Setting the railpack.json path for an app

```yaml
dokku_builder_railpack_property:
    app: node-js-app
    property: railpackjson-path
    value: config/railpack.json
```

### Setting the railpack.json path globally

```yaml
dokku_builder_railpack_property:
    app: ""
    global: true
    property: railpackjson-path
    value: railpack.json
```

### Clearing the railpack.json path for an app

```yaml
dokku_builder_railpack_property:
    app: node-js-app
    property: railpackjson-path
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
