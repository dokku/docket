# dokku_app_clone

## Synopsis

Clones an existing dokku app to a new app

## Export support

Not supported - an imperative clone operation, not reconstructable state.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the new (target) app |
| `source_app` | string | yes |  |  | Name of the existing app to clone from |
| `skip_deploy` | bool | no |  |  | Skip deployment of the cloned app |
| `state` | string | no | present | present | Desired state of the cloned app |

## Examples

### Clone an app

```yaml
dokku_app_clone:
    app: node-js-app-staging
    source_app: node-js-app
```

### Clone an app without deploying

```yaml
dokku_app_clone:
    app: node-js-app-staging
    source_app: node-js-app
    skip_deploy: true
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
