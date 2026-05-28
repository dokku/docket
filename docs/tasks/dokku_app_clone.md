# dokku_app_clone

Clones an existing dokku app to a new app

## Clone an app

```yaml
dokku_app_clone:
    app: node-js-app-staging
    source_app: node-js-app
```

## Clone an app without deploying

```yaml
dokku_app_clone:
    app: node-js-app-staging
    source_app: node-js-app
    skip_deploy: true
```
