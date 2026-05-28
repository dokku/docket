# dokku_registry_property

Manages the registry configuration for a given dokku application

## Setting the image repo for an app

```yaml
dokku_registry_property:
    app: node-js-app
    property: image-repo
    value: registry.example.com/node-js-app
```

## Enabling push-on-release for an app

```yaml
dokku_registry_property:
    app: node-js-app
    property: push-on-release
    value: "true"
```

## Setting the registry server globally

```yaml
dokku_registry_property:
    app: ""
    global: true
    property: server
    value: registry.example.com
```

## Clearing the image repo for an app

```yaml
dokku_registry_property:
    app: node-js-app
    property: image-repo
```
