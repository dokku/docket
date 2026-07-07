# dokku_http_auth_domain

## Synopsis

Manages the set of domains HTTP auth is restricted to for a dokku application

## Requirements

- dokku-http-auth plugin

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `domains` | list | no |  |  | List of domains to restrict HTTP auth to |
| `state` | string | no | present | present, absent, set, clear | Desired state of the HTTP auth domain entries |

## Examples

### Restrict HTTP auth to specific domains for an app

```yaml
dokku_http_auth_domain:
    app: hello-world
    domains:
        - app.example.com
        - www.example.com
```

### Stop restricting HTTP auth to a domain for an app

```yaml
dokku_http_auth_domain:
    app: hello-world
    domains:
        - www.example.com
    state: absent
```

### Replace the set of HTTP auth domains for an app

```yaml
dokku_http_auth_domain:
    app: hello-world
    domains:
        - app.example.com
    state: set
```

### Clear all HTTP auth domains from an app

```yaml
dokku_http_auth_domain:
    app: hello-world
    domains: []
    state: clear
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
