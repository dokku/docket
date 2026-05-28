# dokku_buildpacks_property

Manages the buildpacks configuration for a given dokku application

## Setting the stack value for an app

```yaml
dokku_buildpacks_property:
    app: node-js-app
    property: stack
    value: gliderlabs/herokuish:latest
```

## Setting the stack value globally

```yaml
dokku_buildpacks_property:
    app: ""
    global: true
    property: stack
    value: gliderlabs/herokuish:latest
```

## Clearing the stack value for an app

```yaml
dokku_buildpacks_property:
    app: node-js-app
    property: stack
```
