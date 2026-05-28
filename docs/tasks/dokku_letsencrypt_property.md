# dokku_letsencrypt_property

Manages the letsencrypt configuration for a given dokku application

## Setting the letsencrypt email for an app

```yaml
dokku_letsencrypt_property:
    app: node-js-app
    property: email
    value: admin@example.com
```

## Setting the dns provider for an app

```yaml
dokku_letsencrypt_property:
    app: node-js-app
    property: dns-provider
    value: namecheap
```

## Setting a dns-provider-* env var globally

```yaml
dokku_letsencrypt_property:
    app: ""
    global: true
    property: dns-provider-NAMECHEAP_API_USER
    value: deploy-bot
```

## Clearing the letsencrypt email for an app

```yaml
dokku_letsencrypt_property:
    app: node-js-app
    property: email
```
