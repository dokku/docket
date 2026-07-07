# dokku_domains

## Synopsis

Manages the domains for a given dokku application or globally

## Export support

Supported.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | no |  |  | Name of the app |
| `global` | bool | no |  |  | Flag indicating if the domains should be applied globally |
| `domains` | list | no |  |  | List of domain names |
| `state` | string | no | present | present, absent, set, clear | Desired state of the domains |

## Examples

### Add domains to an app

```yaml
dokku_domains:
    app: example-app
    domains:
        - example.com
        - www.example.com
    state: ""
```

### Remove domains from an app

```yaml
dokku_domains:
    app: example-app
    domains:
        - old.example.com
    state: absent
```

### Set global domains

```yaml
dokku_domains:
    app: ""
    global: true
    domains:
        - global.example.com
    state: set
```

### Clear all domains from an app

```yaml
dokku_domains:
    app: example-app
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
