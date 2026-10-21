# dokku_git_from_image

## Synopsis

Deploys a git repository from a docker image

> **Imperative:** this task runs an operation rather than declaring a resource. It has nothing to undo, so it offers no `absent` or `clear` state.

## Requirements

- Dokku >= 0.38.31

## Export support

Partial - the image reference is written to the companion vars-file.

## Probe support

Partial - the recorded image reference is probed; the image digest is not, so a mutable tag that moved plans as in sync.

## Identity

Keyed by `app`.

## Objects

- The app named by `app`. Can restart or redeploy it.

Tasks applied at once on the same object take turns; see [Running tasks concurrently](../embedding.md#running-tasks-concurrently).

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `image` | string | yes |  |  | Docker image to deploy (sensitive) |
| `build_dir` | string | no |  |  | Directory to build the git repository (applied only, not read back) |
| `git_username` | string | no |  |  | Username to use for the git repository (applied only, not read back) |
| `git_email` | string | no |  |  | Email to use for the git repository (applied only, not read back) |
| `state` | string | no | deployed | deployed | Desired state of the git repository |

## Constraints

- `git_username` and `git_email` must be set together or not at all.

## Examples

### Deploy an app from a docker image

```yaml
dokku_git_from_image:
    app: node-js-app
    image: dokku/smoke-test-app:dockerfile
```

### Deploy from an image with git author metadata

```yaml
dokku_git_from_image:
    app: node-js-app
    image: dokku/smoke-test-app:dockerfile
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
