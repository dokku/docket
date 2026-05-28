# dokku_builds_property

Manages the builds configuration for a given dokku application

## Setting the retention value for an app

```yaml
dokku_builds_property:
    app: node-js-app
    property: retention
    value: "50"
```

## Setting the retention value globally

```yaml
dokku_builds_property:
    app: ""
    global: true
    property: retention
    value: "50"
```

## Clearing the retention value for an app

```yaml
dokku_builds_property:
    app: node-js-app
    property: retention
```
