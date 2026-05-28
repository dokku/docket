# dokku_caddy_property

Manages the caddy configuration for a given dokku application

## Enabling internal TLS for an app

```yaml
dokku_caddy_property:
    app: node-js-app
    property: tls-internal
    value: "true"
```

## Setting the letsencrypt email globally

```yaml
dokku_caddy_property:
    app: ""
    global: true
    property: letsencrypt-email
    value: admin@example.com
```

## Clearing internal TLS for an app

```yaml
dokku_caddy_property:
    app: node-js-app
    property: tls-internal
```
