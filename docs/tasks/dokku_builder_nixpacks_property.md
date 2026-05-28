# dokku_builder_nixpacks_property

Manages the builder-nixpacks configuration for a given dokku application

## Setting the nixpacks.toml path for an app

```yaml
dokku_builder_nixpacks_property:
    app: node-js-app
    property: nixpackstoml-path
    value: config/nixpacks.toml
```

## Setting the nixpacks.toml path globally

```yaml
dokku_builder_nixpacks_property:
    app: ""
    global: true
    property: nixpackstoml-path
    value: nixpacks.toml
```

## Clearing the nixpacks.toml path for an app

```yaml
dokku_builder_nixpacks_property:
    app: node-js-app
    property: nixpackstoml-path
```
