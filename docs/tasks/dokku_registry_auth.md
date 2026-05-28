# dokku_registry_auth

Manages docker registry authentication for a dokku application or globally

## Log in to a registry for an app

```yaml
dokku_registry_auth:
    app: node-js-app
    server: ghcr.io
    username: deploy-bot
    password: ghp_examplepat
```

## Log in to a registry globally

```yaml
dokku_registry_auth:
    app: ""
    global: true
    server: docker.io
    username: deploy-bot
    password: examplepassword
```

## Log out from a registry for an app

```yaml
dokku_registry_auth:
    app: node-js-app
    server: ghcr.io
    state: absent
```

## Log out from a registry globally

```yaml
dokku_registry_auth:
    app: ""
    global: true
    server: docker.io
    state: absent
```
