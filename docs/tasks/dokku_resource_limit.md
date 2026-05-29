# dokku_resource_limit

## Synopsis

Manages the resource limits for a given dokku application

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `process_type` | string | no |  |  | Process type to set resource limits for |
| `resources` | dict | no |  |  | Map of resource type to quantity |
| `clear_before` | bool | no | false |  | ClearBefore clears all resource limits before applying new ones |
| `state` | string | no | present | present, absent | Desired state of the resource limits |

## Examples

### Set CPU and memory limits

```yaml
dokku_resource_limit:
    app: hello-world
    resources:
        cpu: "100"
        memory: "256"
    clear_before: false
```

### Set memory limit for web process type

```yaml
dokku_resource_limit:
    app: hello-world
    process_type: web
    resources:
        memory: "512"
    clear_before: false
```

### Clear all resource limits

```yaml
dokku_resource_limit:
    app: hello-world
    resources: {}
    clear_before: false
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
