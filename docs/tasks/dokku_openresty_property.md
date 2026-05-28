# dokku_openresty_property

Manages the openresty configuration for a given dokku application

## Setting the proxy read timeout for an app

```yaml
dokku_openresty_property:
    app: node-js-app
    property: proxy-read-timeout
    value: 120s
```

## Setting the client max body size for an app

```yaml
dokku_openresty_property:
    app: node-js-app
    property: client-max-body-size
    value: 50m
```

## Setting a global openresty property

```yaml
dokku_openresty_property:
    app: ""
    global: true
    property: bind-address-ipv4
    value: 0.0.0.0
```

## Clearing an openresty property

```yaml
dokku_openresty_property:
    app: node-js-app
    property: proxy-read-timeout
```
