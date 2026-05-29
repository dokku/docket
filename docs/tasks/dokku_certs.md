# dokku_certs

## Synopsis

Manages SSL certificates for a dokku app or globally. The `cert` and `key` fields are paths on the dokku server, so when running with `DOKKU_HOST` set the referenced files must already exist on the remote host - docket does not upload them.

## Requirements

- dokku-global-cert plugin (required only when global: true)

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | no |  |  | Name of the app. Required if Global is false. |
| `global` | bool | no |  |  | Flag indicating if the certificate should be applied globally via the dokku-global-cert plugin |
| `cert` | string | no |  |  | Path on the dokku server to the SSL certificate file (sensitive) |
| `key` | string | no |  |  | Path on the dokku server to the SSL certificate key file (sensitive) |
| `state` | string | no | present | present, absent | Desired state of the SSL configuration |

## Examples

### Add an SSL certificate to an app

```yaml
dokku_certs:
    app: node-js-app
    cert: /etc/nginx/ssl/node-js-app.crt
    key: /etc/nginx/ssl/node-js-app.key
```

### Remove an SSL certificate from an app

```yaml
dokku_certs:
    app: node-js-app
    state: absent
```

### Add a global SSL certificate (requires the dokku-global-cert plugin)

```yaml
dokku_certs:
    app: ""
    global: true
    cert: /etc/nginx/ssl/global.crt
    key: /etc/nginx/ssl/global.key
```

### Remove the global SSL certificate

```yaml
dokku_certs:
    app: ""
    global: true
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
