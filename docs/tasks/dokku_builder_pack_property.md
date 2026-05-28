# dokku_builder_pack_property

Manages the builder-pack configuration for a given dokku application

## Setting the project.toml path for an app

```yaml
dokku_builder_pack_property:
    app: node-js-app
    property: projecttoml-path
    value: config/project.toml
```

## Setting the project.toml path globally

```yaml
dokku_builder_pack_property:
    app: ""
    global: true
    property: projecttoml-path
    value: project.toml
```

## Clearing the project.toml path for an app

```yaml
dokku_builder_pack_property:
    app: node-js-app
    property: projecttoml-path
```
