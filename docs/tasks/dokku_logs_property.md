# dokku_logs_property

Manages the logs configuration for a given dokku application

## Setting the max-size value for an app

```yaml
dokku_logs_property:
    app: node-js-app
    property: max-size
    value: 100m
```

## Setting the max-size value globally

```yaml
dokku_logs_property:
    app: ""
    global: true
    property: max-size
    value: 100m
```

## Clearing the max-size value for an app

```yaml
dokku_logs_property:
    app: node-js-app
    property: max-size
```
