# dokku_checks_property

Manages the checks configuration for a given dokku application

## Setting the wait-to-retire value for an app

```yaml
dokku_checks_property:
    app: node-js-app
    property: wait-to-retire
    value: "60"
```

## Setting the wait-to-retire value globally

```yaml
dokku_checks_property:
    app: ""
    global: true
    property: wait-to-retire
    value: "60"
```

## Clearing the wait-to-retire value for an app

```yaml
dokku_checks_property:
    app: node-js-app
    property: wait-to-retire
```
