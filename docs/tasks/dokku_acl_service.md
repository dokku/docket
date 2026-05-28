# dokku_acl_service

Manages the dokku-acl access list for a dokku service

## Grant users access to a redis service

```yaml
dokku_acl_service:
    service: my-redis
    type: redis
    users:
        - alice
        - bob
```

## Revoke a user's access to a redis service

```yaml
dokku_acl_service:
    service: my-redis
    type: redis
    users:
        - bob
    state: absent
```

## Clear the entire ACL for a redis service

```yaml
dokku_acl_service:
    service: my-redis
    type: redis
    users: []
    state: absent
```
