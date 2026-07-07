# Roadmap

These are ideas for where docket could go, not committed features. They are recorded here so the
direction is visible, but nothing on this page is implemented yet.

- **Export a recipe from a live server.** The inverse of `apply`: `docket export` could read a
  running Dokku server and emit a recipe describing its current state, so you do not have to
  hand-write one. This is the missing half of the [migration](migration.md) story - today a
  migration starts from a recipe you already maintain or reconstruct by hand. Tasks whose state
  cannot be read back (`dokku_git_auth`, `dokku_registry_auth`, `dokku_storage_ensure`) would be
  emitted best-effort or omitted, since their values are not recoverable from the server.
- **Apply from within a repository.** docket could automatically apply a recipe found at
  `.dokku/task.yml` inside an app's own repository. In that mode, certain tasks (such as `dokku_app`
  or `dokku_git_sync`) would be added to a denylist and ignored during the run, so an app cannot
  redefine its own identity.
- **A Dokku command.** Dokku could expose a command such as `dokku app:install` that invokes docket
  to install apps from a recipe.
- **A web UI.** A web UI could let users customize a remote recipe and then call docket directly on
  the generated output.

## See also

- [Getting started](getting-started.md) - what docket does today
- [Writing tasks](writing-tasks.md) - contribute a new task type
