# dokku_http_auth_allowed_ip

## Synopsis

Manages the set of IP addresses allowed to bypass HTTP auth for a dokku application

## Requirements

- dokku-http-auth plugin

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `allowed_ips` | list | no |  |  | List of IP addresses to allow or remove |
| `state` | string | no | present | present, absent | Desired state of the allowed IP entries |

## Examples

### Allow IP addresses to bypass HTTP auth for an app

```yaml
dokku_http_auth_allowed_ip:
    app: hello-world
    allowed_ips:
        - 192.0.2.1
        - 198.51.100.0/24
```

### Remove an allowed IP address from an app

```yaml
dokku_http_auth_allowed_ip:
    app: hello-world
    allowed_ips:
        - 192.0.2.1
    state: absent
```

### Remove all allowed IP addresses from an app

```yaml
dokku_http_auth_allowed_ip:
    app: hello-world
    allowed_ips: []
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
