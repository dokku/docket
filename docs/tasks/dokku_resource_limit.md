# dokku_resource_limit

Manages the resource limits for a given dokku application

## Set CPU and memory limits

```yaml
dokku_resource_limit:
    app: hello-world
    resources:
        cpu: "100"
        memory: "256"
    clear_before: false
```

## Set memory limit for web process type

```yaml
dokku_resource_limit:
    app: hello-world
    process_type: web
    resources:
        memory: "512"
    clear_before: false
```

## Clear all resource limits

```yaml
dokku_resource_limit:
    app: hello-world
    resources: {}
    clear_before: false
    state: absent
```
