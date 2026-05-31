# dokku_scheduler_k3s_labels

## Synopsis

Manages scheduler-k3s labels scoped to a (process_type, resource_type) pair for a dokku application or globally

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | no |  |  | Name of the app. Required if Global is false. |
| `global` | bool | no |  |  | Flag indicating if the labels should be applied globally |
| `process_type` | string | no |  |  | Process type to scope the labels to. Defaults to the global process type when empty. |
| `resource_type` | string | yes |  |  | Kubernetes resource type to scope the labels to (e.g. deployment, ingress). |
| `labels` | dict | no |  |  | Map of label key to value to apply at the scope. |
| `state` | string | no | present | present, absent | Desired state of the labels |

## Examples

### Set deployment labels on an app's web process

```yaml
dokku_scheduler_k3s_labels:
    app: node-js-app
    process_type: web
    resource_type: deployment
    labels:
        app.kubernetes.io/component: api
        tier: edge
```

### Set ingress labels on an app at the global process scope

```yaml
dokku_scheduler_k3s_labels:
    app: node-js-app
    resource_type: ingress
    labels:
        team: platform
```

### Set a global deployment label across all apps

```yaml
dokku_scheduler_k3s_labels:
    app: ""
    global: true
    resource_type: deployment
    labels:
        managed-by: docket
```

### Remove specific labels from an app's deployment

```yaml
dokku_scheduler_k3s_labels:
    app: node-js-app
    resource_type: deployment
    labels:
        tier: ""
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
