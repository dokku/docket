# dokku_proxy_property

Manages the proxy configuration for a given dokku application

## Setting the proxy type for an app

```yaml
dokku_proxy_property:
    app: node-js-app
    property: type
    value: nginx
```

## Setting the proxy type globally

```yaml
dokku_proxy_property:
    app: ""
    global: true
    property: type
    value: haproxy
```

## Setting the proxy port for an app

```yaml
dokku_proxy_property:
    app: node-js-app
    property: proxy-port
    value: "8080"
```

## Clearing the proxy type for an app

```yaml
dokku_proxy_property:
    app: node-js-app
    property: type
```
