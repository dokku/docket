# dokku_app_lock

Locks or unlocks a dokku application from deployment

## Lock an app

```yaml
dokku_app_lock:
    app: node-js-app
```

## Unlock an app

```yaml
dokku_app_lock:
    app: node-js-app
    state: absent
```
