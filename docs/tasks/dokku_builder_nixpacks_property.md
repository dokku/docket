# dokku_builder_nixpacks_property

## Synopsis

Manages the builder-nixpacks configuration for a given dokku application

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | no |  |  | Name of the app. Required if Global is false. |
| `global` | bool | no |  |  | Flag indicating if the builder-nixpacks configuration should be applied globally |
| `property` | string | yes |  |  | Name of the builder-nixpacks property to set |
| `value` | string | no |  |  | Value to set for the builder-nixpacks property |
| `state` | string | no | present | present, absent | Desired state of the builder-nixpacks configuration |

## Examples

### Setting the nixpacks.toml path for an app

```yaml
dokku_builder_nixpacks_property:
    app: node-js-app
    property: nixpackstoml-path
    value: config/nixpacks.toml
```

### Setting the nixpacks.toml path globally

```yaml
dokku_builder_nixpacks_property:
    app: ""
    global: true
    property: nixpackstoml-path
    value: nixpacks.toml
```

### Clearing the nixpacks.toml path for an app

```yaml
dokku_builder_nixpacks_property:
    app: node-js-app
    property: nixpackstoml-path
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
