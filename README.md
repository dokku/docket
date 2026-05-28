# docket

A declarative way to pre-package and ship applications on Dokku.

## Installation

Install the latest release with the quick install script (Linux, macOS, or a POSIX shell on
Windows):

```bash
curl -fsSL https://raw.githubusercontent.com/dokku/docket/main/install.sh | sh
```

Or via Homebrew:

```bash
brew install dokku/repo/docket
```

Or with the Go toolchain:

```bash
go install github.com/dokku/docket@latest
```

See the [Getting started](docs/getting-started.md#installation) guide for all channels, including
prebuilt binaries and Debian/Ubuntu packages.

## Usage

Describe the desired state of your app in a `tasks.yml` recipe:

```yaml
---
- tasks:
    - dokku_app:
        app: inflector
    - dokku_git_sync:
        app: inflector
        repository: http://github.com/cakephp/inflector.cakephp.org
```

Preview what would change, then apply it:

```bash
docket plan    # show what apply would do, without changing anything
docket apply   # make the changes needed to match the recipe
```

Running `docket apply` again is a no-op when the server already matches the recipe.

## Documentation

- [Getting started](docs/getting-started.md) -- why docket, installation, and your first recipe
- [Command reference](docs/command-reference.md) -- every command and flag
- [Recipes](docs/recipes.md) -- the recipe file format, plays, and multi-app recipes
- [Inputs](docs/inputs.md) -- parameterize a recipe with variables and `--vars-file`
- [Task envelope](docs/task-envelope.md) -- tags, conditionals, loops, and error handling per task
- [Remote execution](docs/remote-execution.md) -- drive a remote Dokku server over SSH
- [JSON output](docs/json-output.md) -- the `--json` event schema for `apply` and `plan`
- [Writing tasks](docs/writing-tasks.md) -- contribute a new task type
- [Tasks](docs/tasks/README.md) -- reference for every task type

## License

[MIT](LICENSE)
