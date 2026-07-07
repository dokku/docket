# Roadmap

These are ideas for where docket could go, not committed features. They are recorded here so the
direction is visible, but nothing on this page is implemented yet.

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
