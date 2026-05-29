# dokku_plugin

## Synopsis

Installs or uninstalls a third-party dokku plugin. Installation is a root-level operation, so this task must run over the SSH transport (where dokku wraps privilege server-side) or as root locally. Idempotency is by plugin name only - a changed `url` or `committish` on an already-installed plugin is not detected.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `name` | string | yes |  |  | Plugin name as it appears in plugin:list |
| `url` | string | no |  |  | Git URL to install the plugin from. Required when state is present. |
| `committish` | string | no |  |  | Optional git ref (branch, tag, or commit) to install |
| `state` | string | no | present | present, absent | Desired state of the plugin |

## Examples

### Install a plugin from a git URL

```yaml
dokku_plugin:
    name: redis
    url: https://github.com/dokku/dokku-redis.git
```

### Install a plugin pinned to a committish

```yaml
dokku_plugin:
    name: letsencrypt
    url: https://github.com/dokku/dokku-letsencrypt.git
    committish: 0.25.0
```

### Uninstall a plugin

```yaml
dokku_plugin:
    name: redis
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
