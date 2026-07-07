# dokku_scheduler_k3s_chart

## Synopsis

Manages helm chart value overrides for a dokku scheduler-k3s bundled chart

## Export support

Supported.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `chart` | string | yes |  |  | Name of the helm chart whose values to set (validated by dokku against its bundled HelmCharts list). |
| `values` | dict | yes |  |  | Helm-style values for the chart. Accepts a flat map of dotted property paths or a nested tree; both coalesce to the same key/value form before being applied. In nested form, literal dots in a key segment are escaped to \. so they reach Helm as a single annotation/label key. |
| `state` | string | no | present | present, absent | Desired state of the chart values. |

## Examples

### Set chart values via a flat map of dotted paths

```yaml
dokku_scheduler_k3s_chart:
    chart: ingress-nginx
    values:
        controller.replicaCount: "3"
        controller.resources.limits.cpu: 200m
```

### Set chart values via a nested tree (Helm values.yaml style)

```yaml
dokku_scheduler_k3s_chart:
    chart: ingress-nginx
    values:
        controller:
            replicaCount: "3"
            resources:
                limits:
                    cpu: 200m
```

### Mix nested and flat; nested dotted leaves are escaped for Helm

```yaml
dokku_scheduler_k3s_chart:
    chart: traefik
    values:
        ports.web.redirectTo.port: websecure
        service:
            annotations:
                service.beta.kubernetes.io/aws-load-balancer-type: nlb
```

### Clear specific chart values

```yaml
dokku_scheduler_k3s_chart:
    chart: ingress-nginx
    values:
        controller.replicaCount: ""
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
