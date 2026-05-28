# dokku_builder_property

Manages the builder configuration for a given dokku application

## Overriding the auto-selected builder

```yaml
dokku_builder_property:
    app: node-js-app
    property: selected
    value: dockerfile
```

## Setting the builder to the default value

```yaml
dokku_builder_property:
    app: node-js-app
    property: selected
```

## Changing the build build directory

```yaml
dokku_builder_property:
    app: monorepo
    property: build-dir
    value: backend
```

## Overriding the auto-selected builder globally

```yaml
dokku_builder_property:
    app: ""
    global: true
    property: selected
    value: herokuish
```
