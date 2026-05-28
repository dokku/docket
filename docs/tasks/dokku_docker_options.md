# dokku_docker_options

Manages docker-options for a given dokku application

## Mount the docker socket at deploy

```yaml
dokku_docker_options:
    app: node-js-app
    phase: deploy
    option: -v /var/run/docker.sock:/var/run/docker.sock
```

## Remove a docker option from the deploy phase

```yaml
dokku_docker_options:
    app: node-js-app
    phase: deploy
    option: -v /var/run/docker.sock:/var/run/docker.sock
    state: absent
```
