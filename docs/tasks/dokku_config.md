# dokku_config

Manages the configuration for a given dokku application

## set KEY=VALUE

```yaml
dokku_config:
    app: hello-world
    restart: true
    config:
        KEY: VALUE_1
```

## set KEY=VALUE without restart

```yaml
dokku_config:
    app: hello-world
    restart: false
    config:
        KEY: VALUE_1
```
