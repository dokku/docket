# dokku_scheduler_k3s_property

## Synopsis

Manages the scheduler-k3s configuration for a given dokku application. chart.* properties are managed by dokku_scheduler_k3s_chart and rejected here, since dokku's scheduler-k3s:set path is deprecated for chart values.

## Requirements

- Dokku >= 0.38.8

## Export support

Supported.

## Probe support

Supported.

## Lifecycle

Applied with state `present` and torn down with state `absent`, which refuses `value`, so a teardown resets it. See [Applying and tearing down a task](../embedding.md#applying-and-tearing-down-a-task).

## Identity

Keyed by `app`, `global`, and `property`. Fields left empty are omitted from the address.

## Objects

- The app named by `app`.

Tasks applied at once on the same object take turns; see [Running tasks concurrently](../embedding.md#running-tasks-concurrently).

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | no |  |  | Name of the app |
| `global` | bool | no |  |  | Flag indicating if the property should be applied globally |
| `property` | string | yes |  |  | Name of the property to set |
| `value` | string | no |  |  | Value to set for the property (required when state is present) (must not be set when state is absent) |
| `state` | string | no | present | present, absent | Desired state of the property |

## Constraints

- Exactly one of `app` or `global` must be set.

## Properties

`property` accepts one of the following, applied with `dokku scheduler-k3s:set`. A property with no form in a scope is rejected there, matching dokku's own rejection.

| Property | Scopes | Report key (app) | Report key (global) |
| --- | --- | --- | --- |
| `cert-issuer-kind` | app, global | `cert-issuer-kind` | `global-cert-issuer-kind` |
| `cert-issuer-name` | app, global | `cert-issuer-name` | `global-cert-issuer-name` |
| `deploy-timeout` | app, global | `deploy-timeout` | `global-deploy-timeout` |
| `image-pull-secrets` | app, global | `image-pull-secrets` | `global-image-pull-secrets` |
| `ingress-class` | global |  | `global-ingress-class` |
| `kube-context` | global |  | `global-kube-context` |
| `kubeconfig-path` | global |  | `global-kubeconfig-path` |
| `kustomize-root-path` | app, global | `kustomize-root-path` | `global-kustomize-root-path` |
| `letsencrypt-email-prod` | app, global | `letsencrypt-email-prod` | `global-letsencrypt-email-prod` |
| `letsencrypt-email-stag` | app, global | `letsencrypt-email-stag` | `global-letsencrypt-email-stag` |
| `letsencrypt-server` | app, global | `letsencrypt-server` | `global-letsencrypt-server` |
| `namespace` | app, global | `namespace` | `global-namespace` |
| `network-interface` | global |  | `global-network-interface` |
| `node-sysctls-image` | global |  | `global-node-sysctls-image` |
| `node-sysctls-pause-image` | global |  | `global-node-sysctls-pause-image` |
| `rollback-on-failure` | app, global | `rollback-on-failure` | `global-rollback-on-failure` |
| `shm-size` | app, global | `shm-size` | `global-shm-size` |
| `token` (sensitive) | global |  | `global-token` |

Names starting with `chart.` are rejected here - they are managed by `dokku_scheduler_k3s_chart`, since the scheduler-k3s:set path for chart values is deprecated in dokku.

## Examples

### Setting the deploy timeout for an app

Requires these tasks, applied first:

```yaml
- dokku_app:
      app: node-js-app
```

```yaml
dokku_scheduler_k3s_property:
    app: node-js-app
    property: deploy-timeout
    value: 300s
```

### Setting the namespace for an app

Requires these tasks, applied first:

```yaml
- dokku_app:
      app: node-js-app
```

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

Requires these tasks, applied first:

```yaml
- dokku_app:
      app: node-js-app
```

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
