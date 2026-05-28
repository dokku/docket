# dokku_cron_property

Manages the cron configuration for a given dokku application

## Enabling maintenance mode for an app

```yaml
dokku_cron_property:
    app: node-js-app
    property: maintenance
    value: "true"
```

## Setting the mailto address globally

```yaml
dokku_cron_property:
    app: ""
    global: true
    property: mailto
    value: ops@example.com
```

## Clearing the maintenance mode for an app

```yaml
dokku_cron_property:
    app: node-js-app
    property: maintenance
```
