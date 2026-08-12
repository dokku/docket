# dokku_network_property

## Synopsis

Manages the network property for a given dokku application

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

`property` accepts one of the following, applied with `dokku network:set`. A property with no form in a scope is rejected there, matching dokku's own rejection.

| Property | Scopes | Report key (app) | Report key (global) |
| --- | --- | --- | --- |
| `attach-post-create` | app, global | `attach-post-create` | `global-attach-post-create` |
| `attach-post-deploy` | app, global | `attach-post-deploy` | `global-attach-post-deploy` |
| `bind-all-interfaces` | app, global | `bind-all-interfaces` | `global-bind-all-interfaces` |
| `initial-network` | app, global | `initial-network` | `global-initial-network` |
| `static-web-listener` | app | `static-web-listener` |  |
| `tld` | app, global | `tld` | `global-tld` |

## Examples

### Associates a network after a container is created but before it is started

```yaml
dokku_network_property:
    app: hello-world
    property: attach-post-create
    value: example-network
```

### Associates the network at container creation

```yaml
dokku_network_property:
    app: hello-world
    property: initial-network
    value: example-network
```

### Setting a global network property

```yaml
dokku_network_property:
    app: ""
    global: true
    property: attach-post-create
    value: example-network
```

### Clearing a network property

```yaml
dokku_network_property:
    app: hello-world
    property: attach-post-create
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
