# dokku_git_from_archive

## Synopsis

Deploys a git repository from an archive URL

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `archive_url` | string | yes |  |  | URL of the archive to deploy (sensitive) |
| `archive_type` | string | no | tar | tar, tar.gz, zip | Format of the archive |
| `git_username` | string | no |  |  | Git author username for the synthetic commit |
| `git_email` | string | no |  |  | Git author email for the synthetic commit |
| `state` | string | no | deployed | deployed | Desired state of the deployment |

## Examples

### Deploy a tar archive

```yaml
dokku_git_from_archive:
    app: node-js-app
    archive_url: https://example.com/release-1.0.0.tar
```

### Deploy a zip archive with author metadata

```yaml
dokku_git_from_archive:
    app: node-js-app
    archive_url: https://example.com/release-1.0.0.zip
    archive_type: zip
    git_username: deploy-bot
    git_email: deploy@example.com
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
