# dokku_proxy_property

## Synopsis

Manages the proxy configuration for a given dokku application

## Export support

Supported.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | no |  |  | Name of the app. Required if Global is false. |
| `global` | bool | no |  |  | Flag indicating if the proxy configuration should be applied globally |
| `property` | string | yes |  |  | Name of the proxy property to set |
| `value` | string | no |  |  | Value to set for the proxy property |
| `state` | string | no | present | present, absent | Desired state of the proxy configuration |

## Examples

### Setting the proxy type for an app

```yaml
dokku_proxy_property:
    app: node-js-app
    property: type
    value: nginx
```

### Setting the proxy type globally

```yaml
dokku_proxy_property:
    app: ""
    global: true
    property: type
    value: haproxy
```

### Setting the proxy port for an app

```yaml
dokku_proxy_property:
    app: node-js-app
    property: proxy-port
    value: "8080"
```

### Clearing the proxy type for an app

```yaml
dokku_proxy_property:
    app: node-js-app
    property: type
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
