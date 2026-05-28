# dokku_git_sync

Syncs a git repository to a dokku application

## Sync a git repository to an app

```yaml
dokku_git_sync:
    app: hello-world
    remote: https://github.com/dokku/smoke-test-app.git
    git_ref: ""
    build: false
    build_if_changes: false
    skip_deploy_branch: false
    state: ""
```

## Sync a git repository with a specific ref and build

```yaml
dokku_git_sync:
    app: hello-world
    remote: https://github.com/dokku/smoke-test-app.git
    git_ref: main
    build: true
    build_if_changes: false
    skip_deploy_branch: false
    state: ""
```
