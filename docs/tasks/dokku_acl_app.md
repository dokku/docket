# dokku_acl_app

Manages the dokku-acl access list for a dokku application

## Grant users access to an app

```yaml
dokku_acl_app:
    app: node-js-app
    users:
        - alice
        - bob
```

## Revoke a user's access to an app

```yaml
dokku_acl_app:
    app: node-js-app
    users:
        - bob
    state: absent
```

## Clear the entire ACL for an app

```yaml
dokku_acl_app:
    app: node-js-app
    users: []
    state: absent
```
