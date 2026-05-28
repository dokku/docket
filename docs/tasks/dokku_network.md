# dokku_network

Creates or destroys a Docker network

## Create a network named example-network

```yaml
dokku_network:
    name: example-network
```

## Destroy a network named example-network

```yaml
dokku_network:
    name: example-network
    state: absent
```
