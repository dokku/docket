# dokku_scheduler_property

Manages the scheduler configuration for a given dokku application

## Selecting the scheduler for an app

```yaml
dokku_scheduler_property:
    app: node-js-app
    property: selected
    value: docker-local
```

## Selecting the scheduler globally

```yaml
dokku_scheduler_property:
    app: ""
    global: true
    property: selected
    value: docker-local
```

## Clearing the scheduler property for an app

```yaml
dokku_scheduler_property:
    app: node-js-app
    property: selected
```
