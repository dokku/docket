# dokku_service_create

Creates or destroys a dokku service

## Create a redis service named my-redis

```yaml
dokku_service_create:
    service: redis
    name: my-redis
```

## Create a postgres service named my-db

```yaml
dokku_service_create:
    service: postgres
    name: my-db
```

## Destroy a redis service named my-redis

```yaml
dokku_service_create:
    service: redis
    name: my-redis
    state: absent
```
