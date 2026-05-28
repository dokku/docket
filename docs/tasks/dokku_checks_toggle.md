# dokku_checks_toggle

Enables or disables the checks plugin for a given dokku application

## Disable the zero downtime deployment

```yaml
dokku_checks_toggle:
    app: hello-world
    state: absent
```

## Re-enable the zero downtime deployment (enabled by default)

```yaml
dokku_checks_toggle:
    app: hello-world
    state: present
```
