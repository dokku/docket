# dokku_ports

## Synopsis

Manages the ports for a given dokku application

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `port_mappings` | list | yes |  |  | Port mappings to set. Each item has: scheme, host, container. |
| `state` | string | no | present | present, absent | Desired state of the ports |

## Examples

### Map http port 80 to container port 5000

```yaml
dokku_ports:
    app: node-js-app
    port_mappings:
        - scheme: http
          host: 80
          container: 5000
```

### Map both http and https ports

```yaml
dokku_ports:
    app: node-js-app
    port_mappings:
        - scheme: http
          host: 80
          container: 5000
        - scheme: https
          host: 443
          container: 5000
```

### Remove a port mapping

```yaml
dokku_ports:
    app: node-js-app
    port_mappings:
        - scheme: http
          host: 80
          container: 5000
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
