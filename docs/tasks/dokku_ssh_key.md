# dokku_ssh_key

## Synopsis

Manages an SSH public key for git push access via dokku's ssh-keys plugin

## Requirements

- Dokku >= 0.38.31

## Export support

Supported.

## Probe support

Supported.

## Lifecycle

Applied with state `present` and torn down with state `absent`. See [Applying and tearing down a task](../embedding.md#applying-and-tearing-down-a-task).

## Identity

Keyed by `name`.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `name` | string | yes |  |  | Name that identifies the key in dokku |
| `key` | string | no |  |  | Public key contents (required when state is present) |
| `state` | string | no | present | present, absent | Desired state of the SSH key |

## Examples

### Add a deploy key

```yaml
dokku_ssh_key:
    name: deploy-bot
    key: ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAINioKrRalhe/VF8s43pjp8jpl6LGwv6tF0F5FvKPjUer deploy-bot
```

### Remove a key by name

```yaml
dokku_ssh_key:
    name: deploy-bot
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
