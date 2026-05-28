# dokku_git_auth

Manages netrc credentials for a git host

## Configure netrc credentials for a git host

```yaml
dokku_git_auth:
    host: github.com
    username: deploy-bot
    password: ghp_examplepat
```

## Remove netrc credentials for a git host

```yaml
dokku_git_auth:
    host: github.com
    state: absent
```
