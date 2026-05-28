# dokku_ps_property

Manages the ps configuration for a given dokku application

## Setting the restart-policy value for an app

```yaml
dokku_ps_property:
    app: node-js-app
    property: restart-policy
    value: on-failure:5
```

## Setting the restart-policy value globally

```yaml
dokku_ps_property:
    app: ""
    global: true
    property: restart-policy
    value: on-failure:5
```

## Clearing the restart-policy value for an app

```yaml
dokku_ps_property:
    app: node-js-app
    property: restart-policy
```
