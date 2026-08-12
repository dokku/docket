# dokku_scheduler_k3s_property

## Synopsis

Manages the scheduler-k3s configuration for a given dokku application. chart.* properties are managed by dokku_scheduler_k3s_chart and rejected here, since dokku's scheduler-k3s:set path is deprecated for chart values.

## Export support

Supported.

## Probe support

Supported.

## Identity

Keyed by `app`, `global`, and `property`. Fields left empty are omitted from the address.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | no |  |  | Name of the app. Required if Global is false. |
| `global` | bool | no |  |  | Flag indicating if the property should be applied globally |
| `property` | string | yes |  |  | Name of the property to set |
| `value` | string | no |  |  | Value to set for the property |
| `state` | string | no | present | present, absent | Desired state of the property |

## Properties

`property` accepts one of the following, applied with `dokku scheduler-k3s:set`. A property with no form in a scope is rejected there, matching dokku's own rejection.

| Property | Scopes | Report key (app) | Report key (global) |
| --- | --- | --- | --- |
| `deploy-timeout` | app, global | `deploy-timeout` | `global-deploy-timeout` |
| `image-pull-secrets` | app, global | `image-pull-secrets` | `global-image-pull-secrets` |
| `ingress-class` | global |  | `global-ingress-class` |
| `kube-context` | global |  | `global-kube-context` |
| `kubeconfig-path` | global |  | `global-kubeconfig-path` |
| `kustomize-root-path` | app, global | `kustomize-root-path` | `global-kustomize-root-path` |
| `letsencrypt-email-prod` | global |  | `global-letsencrypt-email-prod` |
| `letsencrypt-email-stag` | global |  | `global-letsencrypt-email-stag` |
| `letsencrypt-server` | app, global | `letsencrypt-server` | `global-letsencrypt-server` |
| `namespace` | app, global | `namespace` | `global-namespace` |
| `network-interface` | global |  | `global-network-interface` |
| `rollback-on-failure` | app, global | `rollback-on-failure` | `global-rollback-on-failure` |
| `shm-size` | app, global | `shm-size` | `global-shm-size` |
| `token` (sensitive) | global |  | `global-token` |

Names starting with `chart.` are rejected here - they are managed by `dokku_scheduler_k3s_chart`, since the scheduler-k3s:set path for chart values is deprecated in dokku.

## Examples

### Setting the deploy timeout for an app

```yaml
dokku_scheduler_k3s_property:
    app: node-js-app
    property: deploy-timeout
    value: 300s
```

### Setting the namespace for an app

```yaml
dokku_scheduler_k3s_property:
    app: node-js-app
    property: namespace
    value: production
```

### Setting the letsencrypt prod email globally

```yaml
dokku_scheduler_k3s_property:
    app: ""
    global: true
    property: letsencrypt-email-prod
    value: admin@example.com
```

### Clearing the namespace for an app

```yaml
dokku_scheduler_k3s_property:
    app: node-js-app
    property: namespace
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
