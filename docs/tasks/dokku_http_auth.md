# dokku_http_auth

Manages HTTP authentication for a given dokku application

## Enable HTTP authentication for an app

```yaml
dokku_http_auth:
    app: hello-world
    username: admin
    password: secret
```

## Disable HTTP authentication for an app

```yaml
dokku_http_auth:
    app: hello-world
    state: absent
```
