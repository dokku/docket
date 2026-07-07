# dokku_scheduler_k3s_profile

## Synopsis

Manages a global scheduler-k3s node profile used when joining nodes to a cluster

## Export support

Supported.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `name` | string | yes |  |  | Name of the node profile. |
| `role` | string | yes |  | server, worker | Role for nodes joined with this profile. |
| `kubelet_args` | list | no |  |  | List of key=value kubelet arguments to forward to k3s. |
| `taint_scheduling` | bool | no |  |  | Whether to taint the node so only workloads that tolerate the taint schedule on it. |
| `allow_unknown_hosts` | bool | no |  |  | Whether to allow ssh connections to nodes whose host key is not yet trusted. |
| `state` | string | no | present | present, absent | Desired state of the profile. |

## Examples

### Define a worker profile with kubelet args

```yaml
dokku_scheduler_k3s_profile:
    name: edge-pool
    role: worker
    kubelet_args:
        - max-pods=64
        - eviction-hard=memory.available<5%
```

### Define a tainted server profile that accepts unknown hosts

```yaml
dokku_scheduler_k3s_profile:
    name: control-plane
    role: server
    taint_scheduling: true
    allow_unknown_hosts: true
```

### Remove a profile

```yaml
dokku_scheduler_k3s_profile:
    name: edge-pool
    role: worker
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
