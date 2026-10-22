# setup-dokku

A composite action that sets up the dokku server docket's integration tests run against, on an Ubuntu GitHub Actions runner. It installs dokku from packagecloud, runs `plugin:install-dependencies --core`, and installs plugins at the committishes docket pins. docket's own CI jobs use it, so the pins live in one place.

## Usage

From another repository, use `dokku/docket/.github/actions/setup-dokku@<tag>`, where `<tag>` is a docket release tag that ships the action. That tag's pins are the server that release was tested against. Replace `vX.Y.Z` below with the tag.

```yaml
jobs:
  acceptance:
    runs-on: ubuntu-24.04
    steps:
      - uses: actions/checkout@v7
      - uses: dokku/docket/.github/actions/setup-dokku@vX.Y.Z
        with:
          plugins: 'redis http-auth'
      - run: sudo dokku version
```

Inside docket, the jobs use the local copy with `uses: ./.github/actions/setup-dokku` after `actions/checkout`.

## Inputs

| Input | Default | Description |
| --- | --- | --- |
| `dokku-version` | `0.38.31` | The dokku package version to install. The default is the release docket's integration suite runs against. |
| `plugins` | `redis http-auth global-cert letsencrypt acl maintenance` | Plugins to install, by name from the table below, separated by commas, spaces or newlines. They install in the order given. An empty string (`plugins: ''`) installs none. An unknown name fails the step before anything is installed. |
| `docker-legacy-links` | `true` | `true` sets `DOCKER_KEEP_DEPRECATED_LEGACY_LINKS_ENV_VARS=1` on the Docker daemon and restarts it before dokku is installed. |
| `initialize-k3s` | `false` | `true` runs `dokku scheduler-k3s:initialize --server-ip 127.0.0.1` after the plugins are installed. |

## Pinned plugins

| Name | Repository | Committish |
| --- | --- | --- |
| `redis` | [dokku/dokku-redis](https://github.com/dokku/dokku-redis) | `2.3.1` |
| `http-auth` | [dokku/dokku-http-auth](https://github.com/dokku/dokku-http-auth) | `0.14.0` |
| `global-cert` | [dokku-community/dokku-global-cert](https://github.com/dokku-community/dokku-global-cert) | `0.7.0` |
| `letsencrypt` | [dokku/dokku-letsencrypt](https://github.com/dokku/dokku-letsencrypt) | `0.25.2` |
| `acl` | [dokku-community/dokku-acl](https://github.com/dokku-community/dokku-acl) | `2.0.1` |
| `maintenance` | [dokku/dokku-maintenance](https://github.com/dokku/dokku-maintenance) | `0.10.2` |

The pins are set in `PLUGIN_PINS` in `setup-dokku.sh`. `TestIntegrationDeclaredVersions` in `internal/tasks/version_integration_test.go` fails when a task declares a plugin floor newer than the version installed here, so a pin and the task floors move together.
