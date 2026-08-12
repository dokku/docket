# dokku_nginx_property

## Synopsis

Manages the nginx configuration for a given dokku application

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

`property` accepts one of the following, applied with `dokku nginx:set`. A property with no form in a scope is rejected there, matching dokku's own rejection.

| Property | Scopes | Report key (app) | Report key (global) |
| --- | --- | --- | --- |
| `access-log-format` | app, global | `access-log-format` | `global-access-log-format` |
| `access-log-path` | app, global | `access-log-path` | `global-access-log-path` |
| `bind-address-ipv4` | app, global | `bind-address-ipv4` | `global-bind-address-ipv4` |
| `bind-address-ipv6` | app, global | `bind-address-ipv6` | `global-bind-address-ipv6` |
| `client-body-timeout` | app, global | `client-body-timeout` | `global-client-body-timeout` |
| `client-header-timeout` | app, global | `client-header-timeout` | `global-client-header-timeout` |
| `client-max-body-size` | app, global | `client-max-body-size` | `global-client-max-body-size` |
| `disable-custom-config` | app, global | `disable-custom-config` | `global-disable-custom-config` |
| `error-log-path` | app, global | `error-log-path` | `global-error-log-path` |
| `hsts` | app, global | `hsts` | `global-hsts` |
| `hsts-include-subdomains` | app, global | `hsts-include-subdomains` | `global-hsts-include-subdomains` |
| `hsts-max-age` | app, global | `hsts-max-age` | `global-hsts-max-age` |
| `hsts-preload` | app, global | `hsts-preload` | `global-hsts-preload` |
| `keepalive-timeout` | app, global | `keepalive-timeout` | `global-keepalive-timeout` |
| `lingering-timeout` | app, global | `lingering-timeout` | `global-lingering-timeout` |
| `nginx-conf-sigil-path` | app, global | `nginx-conf-sigil-path` | `global-nginx-conf-sigil-path` |
| `nginx-service-command` | app, global | `nginx-service-command` | `global-nginx-service-command` |
| `proxy-buffer-size` | app, global | `proxy-buffer-size` | `global-proxy-buffer-size` |
| `proxy-buffering` | app, global | `proxy-buffering` | `global-proxy-buffering` |
| `proxy-buffers` | app, global | `proxy-buffers` | `global-proxy-buffers` |
| `proxy-busy-buffers-size` | app, global | `proxy-busy-buffers-size` | `global-proxy-busy-buffers-size` |
| `proxy-connect-timeout` | app, global | `proxy-connect-timeout` | `global-proxy-connect-timeout` |
| `proxy-keepalive` | app, global | `proxy-keepalive` | `global-proxy-keepalive` |
| `proxy-read-timeout` | app, global | `proxy-read-timeout` | `global-proxy-read-timeout` |
| `proxy-send-timeout` | app, global | `proxy-send-timeout` | `global-proxy-send-timeout` |
| `send-timeout` | app, global | `send-timeout` | `global-send-timeout` |
| `underscore-in-headers` | app, global | `underscore-in-headers` | `global-underscore-in-headers` |
| `x-forwarded-for-value` | app, global | `x-forwarded-for-value` | `global-x-forwarded-for-value` |
| `x-forwarded-port-value` | app, global | `x-forwarded-port-value` | `global-x-forwarded-port-value` |
| `x-forwarded-proto-value` | app, global | `x-forwarded-proto-value` | `global-x-forwarded-proto-value` |
| `x-forwarded-ssl` | app, global | `x-forwarded-ssl` | `global-x-forwarded-ssl` |

## Examples

### Setting the proxy read timeout for an app

```yaml
dokku_nginx_property:
    app: node-js-app
    property: proxy-read-timeout
    value: 120s
```

### Setting the client max body size for an app

```yaml
dokku_nginx_property:
    app: node-js-app
    property: client-max-body-size
    value: 50m
```

### Setting a global nginx property

```yaml
dokku_nginx_property:
    app: ""
    global: true
    property: bind-address-ipv4
    value: 0.0.0.0
```

### Clearing an nginx property

```yaml
dokku_nginx_property:
    app: node-js-app
    property: proxy-read-timeout
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
