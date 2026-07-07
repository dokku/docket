# dokku_scheduler_k3s_annotations

## Synopsis

Manages scheduler-k3s annotations scoped to a (process_type, resource_type) pair for a dokku application or globally

## Export support

Not supported - scheduler-k3s exposes no report for annotations, so the current state cannot be read back (docket#287, dokku/dokku#8800).

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | no |  |  | Name of the app. Required if Global is false. |
| `global` | bool | no |  |  | Flag indicating if the annotations should be applied globally |
| `process_type` | string | no |  |  | Process type to scope the annotations to. Defaults to the global process type when empty. |
| `resource_type` | string | yes |  |  | Kubernetes resource type to scope the annotations to (e.g. deployment, ingress). |
| `annotations` | dict | no |  |  | Map of annotation key to value to apply at the scope. |
| `state` | string | no | present | present, absent | Desired state of the annotations |

## Examples

### Set deployment annotations on an app's web process

```yaml
dokku_scheduler_k3s_annotations:
    app: node-js-app
    process_type: web
    resource_type: deployment
    annotations:
        prometheus.io/port: "9090"
        prometheus.io/scrape: "true"
```

### Set ingress annotations on an app at the global process scope

```yaml
dokku_scheduler_k3s_annotations:
    app: node-js-app
    resource_type: ingress
    annotations:
        nginx.ingress.kubernetes.io/rewrite-target: /
```

### Set a global deployment annotation across all apps

```yaml
dokku_scheduler_k3s_annotations:
    app: ""
    global: true
    resource_type: deployment
    annotations:
        managed-by: docket
```

### Remove specific annotations from an app's deployment

```yaml
dokku_scheduler_k3s_annotations:
    app: node-js-app
    resource_type: deployment
    annotations:
        prometheus.io/scrape: ""
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
