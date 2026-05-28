# dokku_domains

Manages the domains for a given dokku application or globally

## Add domains to an app

```yaml
dokku_domains:
    app: example-app
    domains:
        - example.com
        - www.example.com
    state: ""
```

## Remove domains from an app

```yaml
dokku_domains:
    app: example-app
    domains:
        - old.example.com
    state: absent
```

## Set global domains

```yaml
dokku_domains:
    app: ""
    global: true
    domains:
        - global.example.com
    state: set
```

## Clear all domains from an app

```yaml
dokku_domains:
    app: example-app
    domains: []
    state: clear
```
