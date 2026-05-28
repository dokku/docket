# dokku_resource_reserve

Manages the resource reservations for a given dokku application

## Set CPU and memory reservations

```yaml
dokku_resource_reserve:
    app: hello-world
    resources:
        cpu: "100"
        memory: "256"
    clear_before: false
```

## Set memory reservation for web process type

```yaml
dokku_resource_reserve:
    app: hello-world
    process_type: web
    resources:
        memory: "512"
    clear_before: false
```

## Clear all resource reservations

```yaml
dokku_resource_reserve:
    app: hello-world
    resources: {}
    clear_before: false
    state: absent
```
