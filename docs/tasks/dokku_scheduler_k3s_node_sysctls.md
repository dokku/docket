# dokku_scheduler_k3s_node_sysctls

## Synopsis

Manages the scheduler-k3s node-level kernel sysctls applied to every node without a node profile

## Export support

Supported.

## Probe support

Supported.

## Identity

Keyed by `global`. Manages the whole `sysctls` collection; entries are identified by their key.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `global` | bool | no |  |  | Scope the sysctls to every node without a node profile. Must be true: node profile scopes are not manageable until dokku exposes a profile's stored sysctl map. |
| `sysctls` | dict | no |  |  | Map of sysctl name to value to apply to the scope; omit for state 'clear'. Removing a sysctl stops it being applied to new nodes but does not revert it on nodes already carrying it until they reboot. Under state 'present' a value must not begin with '-', which dokku's flag parser would read as a flag; state 'set' has no such limit. |
| `state` | string | no | present | present, absent, set, clear | Desired state of the sysctls. 'set' declares the complete map for the scope, removing any sysctl the recipe does not name; 'clear' empties it. |

## Examples

### Set node sysctls for every unprofiled node

```yaml
dokku_scheduler_k3s_node_sysctls:
    global: true
    sysctls:
        vm.max_map_count: "262144"
        vm.swappiness: "10"
```

### Remove a specific node sysctl

```yaml
dokku_scheduler_k3s_node_sysctls:
    global: true
    sysctls:
        vm.swappiness: ""
    state: absent
```

### Replace every node sysctl

```yaml
dokku_scheduler_k3s_node_sysctls:
    global: true
    sysctls:
        vm.max_map_count: "262144"
    state: set
```

### Clear every node sysctl

```yaml
dokku_scheduler_k3s_node_sysctls:
    global: true
    state: clear
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
