# dokku_ps_scale

Manages the process scale for a given dokku application

## Scale web and worker processes

```yaml
dokku_ps_scale:
    app: hello-world
    scale:
        web: 2
        worker: 1
    skip_deploy: false
```

## Scale web and worker processes without deploy

```yaml
dokku_ps_scale:
    app: hello-world
    scale:
        web: 4
        worker: 4
    skip_deploy: true
```
