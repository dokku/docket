# dokku_app_json_property

Manages the app.json configuration for a given dokku application

## Setting the appjson-path for an app

```yaml
dokku_app_json_property:
    app: node-js-app
    property: appjson-path
    value: app.json
```

## Setting the appjson-path globally

```yaml
dokku_app_json_property:
    app: ""
    global: true
    property: appjson-path
    value: app.json
```

## Clearing the appjson-path for an app

```yaml
dokku_app_json_property:
    app: node-js-app
    property: appjson-path
```
