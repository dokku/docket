# dokku_builder_dockerfile_property

Manages the builder-dockerfile configuration for a given dokku application

## Setting the dockerfile path for an app

```yaml
dokku_builder_dockerfile_property:
    app: node-js-app
    property: dockerfile-path
    value: Dockerfile.production
```

## Setting the dockerfile path globally

```yaml
dokku_builder_dockerfile_property:
    app: ""
    global: true
    property: dockerfile-path
    value: Dockerfile
```

## Clearing the dockerfile path for an app

```yaml
dokku_builder_dockerfile_property:
    app: node-js-app
    property: dockerfile-path
```
