# dokku_maintenance_custom_page

## Synopsis

Installs or removes a custom maintenance page for a dokku application.

## Requirements

- dokku-maintenance plugin

## Export support

Partial - the custom page is detected but its content may not be reconstructable and becomes a required input.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `content` | string | no |  |  | Inline HTML stored as maintenance.html on the app. Mutually exclusive with tarball; one is required when state is present. |
| `tarball` | string | no |  |  | Path on the machine running docket to a tar archive containing at least maintenance.html. Mutually exclusive with content; one is required when state is present. |
| `state` | string | no | present | present, absent | Desired state of the custom maintenance page |

## Examples

### Set a custom maintenance page from inline HTML

```yaml
dokku_maintenance_custom_page:
    app: node-js-app
    content: |
        <html><body><h1>Down for maintenance</h1></body></html>
```

### Set a custom maintenance page from a tarball (supports extra assets)

```yaml
dokku_maintenance_custom_page:
    app: node-js-app
    tarball: /etc/dokku/maintenance/node-js-app.tar
```

### Remove the custom maintenance page, resetting to the default

```yaml
dokku_maintenance_custom_page:
    app: node-js-app
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
