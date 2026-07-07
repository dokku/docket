# dokku_git_from_image

## Synopsis

Deploys a git repository from a docker image

## Export support

Partial - the image reference is written to the companion vars-file.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `image` | string | yes |  |  | Docker image to deploy (sensitive) |
| `build_dir` | string | no |  |  | Directory to build the git repository |
| `git_username` | string | no |  |  | Username to use for the git repository |
| `git_email` | string | no |  |  | Email to use for the git repository |
| `state` | string | no | deployed | deployed | Desired state of the git repository |

## Examples

### Deploy an app from a docker image

```yaml
dokku_git_from_image:
    app: node-js-app
    image: dokku/node-js-app:latest
```

### Deploy from an image with a build directory and git author

```yaml
dokku_git_from_image:
    app: node-js-app
    image: dokku/node-js-app:latest
    build_dir: /app
    git_username: dokku
    git_email: dokku@example.com
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
