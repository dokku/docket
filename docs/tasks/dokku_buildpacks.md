# dokku_buildpacks

## Synopsis

Manages the buildpacks for a given dokku application

## Export support

Supported.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `buildpacks` | list | no |  |  | List of buildpack URLs |
| `state` | string | no | present | present, absent | Desired state of the buildpacks |

## Examples

### Add buildpacks to an app

```yaml
dokku_buildpacks:
    app: node-js-app
    buildpacks:
        - https://github.com/heroku/heroku-buildpack-nodejs.git
        - https://github.com/heroku/heroku-buildpack-nginx.git
    state: ""
```

### Remove a buildpack from an app

```yaml
dokku_buildpacks:
    app: node-js-app
    buildpacks:
        - https://github.com/heroku/heroku-buildpack-nginx.git
    state: absent
```

### Clear all buildpacks from an app

```yaml
dokku_buildpacks:
    app: node-js-app
    buildpacks: []
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
