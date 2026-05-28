# dokku_scheduler_docker_local_property

Manages the scheduler-docker-local configuration for a given dokku application

## Enabling the init process for an app

```yaml
dokku_scheduler_docker_local_property:
    app: node-js-app
    property: init-process
    value: "true"
```

## Setting the parallel schedule count for an app

```yaml
dokku_scheduler_docker_local_property:
    app: node-js-app
    property: parallel-schedule-count
    value: "4"
```

## Clearing the init process for an app

```yaml
dokku_scheduler_docker_local_property:
    app: node-js-app
    property: init-process
```
