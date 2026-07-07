# dokku_docker_options

## Synopsis

Manages docker-options for a given dokku application

## Export support

Supported.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `phase` | string | yes |  | build, deploy, run | Deployment phase the option applies to |
| `process_type` | string | no |  |  | Process type the option is scoped to (deploy phase only). Empty applies to the default scope (every container). |
| `option` | string | yes |  |  | Docker option string (e.g. '-v /var/run/docker.sock:/var/run/docker.sock') |
| `state` | string | no | present | present, absent | Desired state of the docker option |

## Examples

### Mount the docker socket at deploy

```yaml
dokku_docker_options:
    app: node-js-app
    phase: deploy
    option: -v /var/run/docker.sock:/var/run/docker.sock
```

### Scope a deploy option to the web process type

```yaml
dokku_docker_options:
    app: node-js-app
    phase: deploy
    process_type: web
    option: --memory=512m
```

### Remove a docker option from the deploy phase

```yaml
dokku_docker_options:
    app: node-js-app
    phase: deploy
    option: -v /var/run/docker.sock:/var/run/docker.sock
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
