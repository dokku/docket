# dokku_app

Creates or destroys an app

## Create an app named hello-world

```yaml
dokku_app:
    app: hello-world
```

## Destroy the app named hello-world

```yaml
dokku_app:
    app: hello-world
    state: absent
```
