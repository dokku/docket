# dokku_network_property

Manages the network property for a given dokku application

## Associates a network after a container is created but before it is started

```yaml
dokku_network_property:
    app: hello-world
    property: attach-post-create
    value: example-network
```

## Associates the network at container creation

```yaml
dokku_network_property:
    app: hello-world
    property: initial-network
    value: example-network
```

## Setting a global network property

```yaml
dokku_network_property:
    app: ""
    global: true
    property: attach-post-create
    value: example-network
```

## Clearing a network property

```yaml
dokku_network_property:
    app: hello-world
    property: attach-post-create
```
