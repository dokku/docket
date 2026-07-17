# dokku_git_property

## Synopsis

Manages the git configuration for a given dokku application

## Export support

Supported.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | no |  |  | Name of the app. Required if Global is false. |
| `global` | bool | no |  |  | Flag indicating if the git configuration should be applied globally |
| `property` | string | yes |  |  | Name of the git property to set |
| `value` | string | no |  |  | Value to set for the git property |
| `state` | string | no | present | present, absent | Desired state of the git configuration |

## Examples

### Setting the deploy branch for an app

```yaml
dokku_git_property:
    app: node-js-app
    property: deploy-branch
    value: main
```

### Keeping the .git directory during builds

```yaml
dokku_git_property:
    app: node-js-app
    property: keep-git-dir
    value: "true"
```

### Setting the rev env var globally

```yaml
dokku_git_property:
    app: ""
    global: true
    property: rev-env-var
    value: GIT_REV
```

### Clearing a git property

```yaml
dokku_git_property:
    app: node-js-app
    property: deploy-branch
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
