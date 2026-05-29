# dokku_git_sync

## Synopsis

Syncs a git repository to a dokku application

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `remote` | string | yes |  |  | Git remote url to sync |
| `git_ref` | string | no |  |  | Git reference to sync |
| `build` | bool | no |  |  | Trigger an application build after syncing |
| `build_if_changes` | bool | no |  |  | Trigger a build only if changes are detected |
| `skip_deploy_branch` | bool | no |  |  | Skip automatically setting the deploy-branch property |
| `state` | string | no | present | present | Desired state of the git sync |

## Examples

### Sync a git repository to an app

```yaml
dokku_git_sync:
    app: hello-world
    remote: https://github.com/dokku/smoke-test-app.git
    git_ref: ""
    build: false
    build_if_changes: false
    skip_deploy_branch: false
    state: ""
```

### Sync a git repository with a specific ref and build

```yaml
dokku_git_sync:
    app: hello-world
    remote: https://github.com/dokku/smoke-test-app.git
    git_ref: main
    build: true
    build_if_changes: false
    skip_deploy_branch: false
    state: ""
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
