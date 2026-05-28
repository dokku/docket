# dokku_service_link

Links or unlinks a dokku service to an app

## Link a redis service named my-redis to my-app

```yaml
dokku_service_link:
    app: my-app
    service: redis
    name: my-redis
```

## Link a postgres service named my-db to my-app

```yaml
dokku_service_link:
    app: my-app
    service: postgres
    name: my-db
```

## Unlink a redis service named my-redis from my-app

```yaml
dokku_service_link:
    app: my-app
    service: redis
    name: my-redis
    state: absent
```
