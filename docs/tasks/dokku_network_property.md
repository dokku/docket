# dokku_network_property

## Synopsis

Manages the network property for a given dokku application

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | no |  |  | Name of the app. Required if Global is false. |
| `global` | bool | no |  |  | Flag indicating if the network property should be applied globally |
| `property` | string | yes |  |  | Name of the network property to set |
| `value` | string | no |  |  | Value of the network property to set |
| `state` | string | no | present | present, absent | Desired state of the network property |

## Examples

### Associates a network after a container is created but before it is started

```yaml
dokku_network_property:
    app: hello-world
    property: attach-post-create
    value: example-network
```

### Associates the network at container creation

```yaml
dokku_network_property:
    app: hello-world
    property: initial-network
    value: example-network
```

### Setting a global network property

```yaml
dokku_network_property:
    app: ""
    global: true
    property: attach-post-create
    value: example-network
```

### Clearing a network property

```yaml
dokku_network_property:
    app: hello-world
    property: attach-post-create
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
