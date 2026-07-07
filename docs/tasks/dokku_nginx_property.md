# dokku_nginx_property

## Synopsis

Manages the nginx configuration for a given dokku application

## Export support

Supported.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | no |  |  | Name of the app. Required if Global is false. |
| `global` | bool | no |  |  | Flag indicating if the nginx configuration should be applied globally |
| `property` | string | yes |  |  | Name of the nginx property to set |
| `value` | string | no |  |  | Value to set for the nginx property |
| `state` | string | no | present | present, absent | Desired state of the nginx configuration |

## Examples

### Setting the proxy read timeout for an app

```yaml
dokku_nginx_property:
    app: node-js-app
    property: proxy-read-timeout
    value: 120s
```

### Setting the client max body size for an app

```yaml
dokku_nginx_property:
    app: node-js-app
    property: client-max-body-size
    value: 50m
```

### Setting a global nginx property

```yaml
dokku_nginx_property:
    app: ""
    global: true
    property: bind-address-ipv4
    value: 0.0.0.0
```

### Clearing an nginx property

```yaml
dokku_nginx_property:
    app: node-js-app
    property: proxy-read-timeout
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
