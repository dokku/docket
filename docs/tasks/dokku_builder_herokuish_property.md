# dokku_builder_herokuish_property

Manages the builder-herokuish configuration for a given dokku application

## Allowing the herokuish builder for an app

```yaml
dokku_builder_herokuish_property:
    app: node-js-app
    property: allowed
    value: "true"
```

## Allowing the herokuish builder globally

```yaml
dokku_builder_herokuish_property:
    app: ""
    global: true
    property: allowed
    value: "true"
```

## Clearing the allowed property for an app

```yaml
dokku_builder_herokuish_property:
    app: node-js-app
    property: allowed
```
