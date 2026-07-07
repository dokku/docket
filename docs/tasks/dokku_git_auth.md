# dokku_git_auth

## Synopsis

Manages netrc credentials for a git host

## Export support

Not supported - netrc credentials are write-only and cannot be read back.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `host` | string | yes |  |  | Git server hostname (e.g. github.com) |
| `username` | string | no |  |  | Netrc username. Required when state is present. |
| `password` | string | no |  |  | Netrc password. Required when state is present. (sensitive) |
| `state` | string | no | present | present, absent | Desired state of the netrc entry |

## Examples

### Configure netrc credentials for a git host

```yaml
dokku_git_auth:
    host: github.com
    username: deploy-bot
    password: ghp_examplepat
```

### Remove netrc credentials for a git host

```yaml
dokku_git_auth:
    host: github.com
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
