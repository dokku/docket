# dokku_certs

Manages SSL certificates for a dokku app or globally. The `cert` and `key` fields are paths on the dokku server, so when running with `DOKKU_HOST` set the referenced files must already exist on the remote host - docket does not upload them.

## Add an SSL certificate to an app

```yaml
dokku_certs:
    app: node-js-app
    cert: /etc/nginx/ssl/node-js-app.crt
    key: /etc/nginx/ssl/node-js-app.key
```

## Remove an SSL certificate from an app

```yaml
dokku_certs:
    app: node-js-app
    state: absent
```

## Add a global SSL certificate (requires the dokku-global-cert plugin)

```yaml
dokku_certs:
    app: ""
    global: true
    cert: /etc/nginx/ssl/global.crt
    key: /etc/nginx/ssl/global.key
```

## Remove the global SSL certificate

```yaml
dokku_certs:
    app: ""
    global: true
    state: absent
```
