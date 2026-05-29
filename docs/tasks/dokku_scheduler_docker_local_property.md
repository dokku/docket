# dokku_scheduler_docker_local_property

## Synopsis

Manages the scheduler-docker-local configuration for a given dokku application

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `property` | string | yes |  |  | Name of the scheduler-docker-local property to set |
| `value` | string | no |  |  | Value to set for the scheduler-docker-local property |
| `state` | string | no | present | present, absent | Desired state of the scheduler-docker-local configuration |

## Examples

### Enabling the init process for an app

```yaml
dokku_scheduler_docker_local_property:
    app: node-js-app
    property: init-process
    value: "true"
```

### Setting the parallel schedule count for an app

```yaml
dokku_scheduler_docker_local_property:
    app: node-js-app
    property: parallel-schedule-count
    value: "4"
```

### Clearing the init process for an app

```yaml
dokku_scheduler_docker_local_property:
    app: node-js-app
    property: init-process
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
