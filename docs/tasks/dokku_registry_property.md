# dokku_registry_property

## Synopsis

Manages the registry configuration for a given dokku application

## Export support

Supported.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | no |  |  | Name of the app. Required if Global is false. |
| `global` | bool | no |  |  | Flag indicating if the registry configuration should be applied globally |
| `property` | string | yes |  |  | Name of the registry property to set |
| `value` | string | no |  |  | Value to set for the registry property |
| `state` | string | no | present | present, absent | Desired state of the registry configuration |

## Examples

### Setting the image repo for an app

```yaml
dokku_registry_property:
    app: node-js-app
    property: image-repo
    value: registry.example.com/node-js-app
```

### Enabling push-on-release for an app

```yaml
dokku_registry_property:
    app: node-js-app
    property: push-on-release
    value: "true"
```

### Setting the registry server globally

```yaml
dokku_registry_property:
    app: ""
    global: true
    property: server
    value: registry.example.com
```

### Clearing the image repo for an app

```yaml
dokku_registry_property:
    app: node-js-app
    property: image-repo
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
