# dokku_traefik_property

Manages the traefik configuration for a given dokku application

## Setting the letsencrypt email for an app

```yaml
dokku_traefik_property:
    app: node-js-app
    property: letsencrypt-email
    value: admin@example.com
```

## Setting the log level globally

```yaml
dokku_traefik_property:
    app: ""
    global: true
    property: log-level
    value: INFO
```

## Clearing the letsencrypt email for an app

```yaml
dokku_traefik_property:
    app: node-js-app
    property: letsencrypt-email
```
