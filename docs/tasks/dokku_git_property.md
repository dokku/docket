# dokku_git_property

Manages the git configuration for a given dokku application

## Setting the deploy branch for an app

```yaml
dokku_git_property:
    app: node-js-app
    property: deploy-branch
    value: main
```

## Keeping the .git directory during builds

```yaml
dokku_git_property:
    app: node-js-app
    property: keep-git-dir
    value: "true"
```

## Setting the rev env var globally

```yaml
dokku_git_property:
    app: ""
    global: true
    property: rev-env-var
    value: GIT_REV
```

## Clearing a git property

```yaml
dokku_git_property:
    app: node-js-app
    property: deploy-branch
```
