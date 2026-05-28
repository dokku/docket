# dokku_letsencrypt

Enables or disables letsencrypt SSL certificates for a dokku application

## Enable letsencrypt for an app

```yaml
dokku_letsencrypt:
    app: node-js-app
```

## Disable letsencrypt for an app

```yaml
dokku_letsencrypt:
    app: node-js-app
    state: absent
```
