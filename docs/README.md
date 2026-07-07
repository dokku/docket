# Documentation

Complete documentation for docket, a declarative way to pre-package and ship applications on Dokku.

## Getting started

- [Getting started](getting-started.md) -- why docket, installation, and your first recipe

## Reference

- [Command reference](command-reference.md) -- every command and flag
- [Tasks](tasks/README.md) -- reference for every task type

## Guides

- [Recipes](recipes.md) -- the recipe file format, plays, and multi-app recipes
- [Inputs](inputs.md) -- parameterize a recipe with variables and `--vars-file`
- [Task envelope](task-envelope.md) -- tags, conditionals, loops, and error handling per task
- [Remote execution](remote-execution.md) -- drive a remote Dokku server over SSH
- [Migration](migration.md) -- move a Dokku setup to a new server
- [JSON output](json-output.md) -- the `--json` event schema for `apply` and `plan`
- [Writing tasks](writing-tasks.md) -- contribute a new task type
- [Roadmap](roadmap.md) -- ideas for where docket could go next
