# dokku_buildpacks

Manages the buildpacks for a given dokku application

## Add buildpacks to an app

```yaml
dokku_buildpacks:
    app: node-js-app
    buildpacks:
        - https://github.com/heroku/heroku-buildpack-nodejs.git
        - https://github.com/heroku/heroku-buildpack-nginx.git
    state: ""
```

## Remove a buildpack from an app

```yaml
dokku_buildpacks:
    app: node-js-app
    buildpacks:
        - https://github.com/heroku/heroku-buildpack-nginx.git
    state: absent
```

## Clear all buildpacks from an app

```yaml
dokku_buildpacks:
    app: node-js-app
    buildpacks: []
    state: absent
```
