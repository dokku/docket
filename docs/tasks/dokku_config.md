# dokku_config

## Synopsis

Manages the configuration for a given dokku application

## Export support

Partial - config values are written to the companion vars-file.

## Probe support

Supported.

## Identity

Keyed by `app`. Manages the whole `config` collection; entries are identified by their key.

## Parameters

| Parameter | Type | Required | Default | Choices | Description |
| --- | --- | --- | --- | --- | --- |
| `app` | string | yes |  |  | Name of the app |
| `restart` | bool | no | true |  | Flag indicating if the app should be restarted |
| `config` | dict | no |  |  | Map of configuration key-value pairs; omit for state 'clear' |
| `preserve` | list | no |  |  | Keys that state 'set' and 'clear' leave untouched, on top of the service-link keys, NO_VHOST, and the git rev-env-var key they always keep; only valid for state 'set' or 'clear' |
| `state` | string | no | present | present, absent, set, clear | Desired state of the configuration: 'present' sets the named keys and 'absent' unsets them, leaving every other key alone; 'set' declares the app's entire config, unsetting every key it does not name; 'clear' unsets every key. Both 'set' and 'clear' keep keys a service link wrote, NO_VHOST, the git rev-env-var key, and any key listed in 'preserve' |

## Examples

### set KEY=VALUE

```yaml
dokku_config:
    app: hello-world
    restart: true
    config:
        KEY: VALUE_1
```

### set KEY=VALUE without restart

```yaml
dokku_config:
    app: hello-world
    restart: false
    config:
        KEY: VALUE_1
```

### set the app's entire config, keeping a key written outside the recipe

```yaml
dokku_config:
    app: hello-world
    restart: true
    config:
        KEY: VALUE_1
        LOG_LEVEL: info
    preserve:
        - SECRET_KEY_BASE
    state: set
```

### clear the app's config

```yaml
dokku_config:
    app: hello-world
    restart: true
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
