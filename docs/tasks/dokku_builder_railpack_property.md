# dokku_builder_railpack_property

Manages the builder-railpack configuration for a given dokku application

## Setting the railpack.json path for an app

```yaml
dokku_builder_railpack_property:
    app: node-js-app
    property: railpackjson-path
    value: config/railpack.json
```

## Setting the railpack.json path globally

```yaml
dokku_builder_railpack_property:
    app: ""
    global: true
    property: railpackjson-path
    value: railpack.json
```

## Clearing the railpack.json path for an app

```yaml
dokku_builder_railpack_property:
    app: node-js-app
    property: railpackjson-path
```
