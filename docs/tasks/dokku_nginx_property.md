# dokku_nginx_property

Manages the nginx configuration for a given dokku application

## Setting the proxy read timeout for an app

```yaml
dokku_nginx_property:
    app: node-js-app
    property: proxy-read-timeout
    value: 120s
```

## Setting the client max body size for an app

```yaml
dokku_nginx_property:
    app: node-js-app
    property: client-max-body-size
    value: 50m
```

## Setting a global nginx property

```yaml
dokku_nginx_property:
    app: ""
    global: true
    property: bind-address-ipv4
    value: 0.0.0.0
```

## Clearing an nginx property

```yaml
dokku_nginx_property:
    app: node-js-app
    property: proxy-read-timeout
```
