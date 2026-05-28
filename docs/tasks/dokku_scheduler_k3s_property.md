# dokku_scheduler_k3s_property

Manages the scheduler-k3s configuration for a given dokku application

## Setting the deploy timeout for an app

```yaml
dokku_scheduler_k3s_property:
    app: node-js-app
    property: deploy-timeout
    value: 300s
```

## Setting the namespace for an app

```yaml
dokku_scheduler_k3s_property:
    app: node-js-app
    property: namespace
    value: production
```

## Setting the letsencrypt prod email globally

```yaml
dokku_scheduler_k3s_property:
    app: ""
    global: true
    property: letsencrypt-email-prod
    value: admin@example.com
```

## Clearing the namespace for an app

```yaml
dokku_scheduler_k3s_property:
    app: node-js-app
    property: namespace
```
