# Getting started

## Why docket

If you manage a Dokku server today, you probably create and configure apps by running a
sequence of `dokku` commands by hand: `apps:create`, `config:set`, `domains:set`, `git:sync`,
and so on. That works, but the steps live in your shell history or a README, they are easy to
run out of order, and there is no safe way to ask "what would change if I ran these again?"

docket is a *declarative* way to describe a Dokku app. Instead of writing the commands that
change the server, you write a file that describes the state the server should be in - which
apps exist, what their config is, what domains they answer on - and docket figures out the
commands needed to get there. This is the same idea behind tools like Ansible and Terraform,
applied to Dokku. Two properties make it useful:

- **Desired state, not steps.** You declare the end result. docket reads the live server,
  compares it to your file, and only runs the commands required to close the gap.
- **Idempotent.** Running the same file twice is safe. The first run makes changes; the second
  run sees the server already matches and does nothing. There is no "already exists" error to
  work around.

docket grew out of [`ansible-dokku`](https://github.com/dokku/ansible-dokku) and exposes the
same modules, but as a single Go binary with no Ansible installation required. If you already
have `ansible-dokku` task lists, the task names will look familiar.

A docket file is called a **recipe**. The smallest useful recipe creates an app and deploys
code into it:

```yaml
---
- tasks:
    - dokku_app:
        app: inflector
    - dokku_git_sync:
        app: inflector
        remote: http://github.com/cakephp/inflector.cakephp.org
```

## Prerequisites

- **Dokku >= 0.38.14.**
- **dokku-letsencrypt >= 0.25.0.**

## Installation

After installing, confirm docket is on your `PATH`:

```bash
docket version
```

### Quick install (Linux, macOS, Windows)

The install script downloads the latest release binary and installs it onto your `PATH`:

```bash
curl -fsSL https://raw.githubusercontent.com/dokku/docket/main/install.sh | sh
```

Pin a version or change the destination with environment variables:

```bash
VERSION=0.1.0 curl -fsSL https://raw.githubusercontent.com/dokku/docket/main/install.sh | sh
BIN_DIR="$HOME/.local/bin" curl -fsSL https://raw.githubusercontent.com/dokku/docket/main/install.sh | sh
```

On Linux and macOS the script installs to `/usr/local/bin` (using `sudo` only if that directory
is not writable). On Windows, run the script from a POSIX shell - Git Bash, MSYS2, Cygwin, or
WSL - and it installs `docket.exe` to `$HOME/bin`.

### Homebrew (macOS, Linux)

```bash
brew install dokku/repo/docket
```

### Go

If you have a Go toolchain, install straight from source:

```bash
go install github.com/dokku/docket@latest
```

### Binary download

Every release attaches a prebuilt binary per platform to
[GitHub Releases](https://github.com/dokku/docket/releases). The assets are named
`docket-<os>-<arch>`, for example `docket-linux-amd64`, `docket-darwin-arm64`, or
`docket-windows-amd64.exe`. Download the one for your platform, make it executable, and move it
onto your `PATH`:

```bash
curl -fsSL -o docket https://github.com/dokku/docket/releases/latest/download/docket-linux-amd64
chmod +x docket
sudo mv docket /usr/local/bin/docket
```

### Debian / Ubuntu

Each release also ships `.deb` packages. Install one directly from the release:

```bash
curl -fsSL -O https://github.com/dokku/docket/releases/latest/download/docket_0.1.0_amd64.deb
sudo dpkg -i docket_0.1.0_amd64.deb
```

Or add the Dokku packagecloud repository once and install with `apt` so you get upgrades:

```bash
curl -fsSL https://packagecloud.io/install/repositories/dokku/dokku/script.deb.sh | sudo bash
sudo apt-get install docket
```

## Your first recipe

Create a file named `tasks.yml` in your project. docket picks this name up automatically:

```yaml
---
- tasks:
    - dokku_app:
        app: inflector
    - dokku_git_sync:
        app: inflector
        remote: http://github.com/cakephp/inflector.cakephp.org
```

If you would rather start from a generated template, run `docket init` and it writes a starter
`tasks.yml` for you. See the [command reference](command-reference.md#docket-init) for its flags.

## Applying the recipe

`docket apply` runs the recipe against your Dokku server, making only the changes needed. Run it
from the same directory as the file:

```bash
docket apply
```

Each task prints a status marker, and a summary line closes the run:

```text
==> Play: tasks
[changed] dokku apps:create inflector
[changed] dokku git:sync inflector

Summary: 2 tasks · 2 changed · 0 ok · 0 skipped · 0 errors  (took 4.1s)
```

Run it a second time and docket reports that nothing changed, because the server already matches
the recipe:

```text
==> Play: tasks
[ok]      dokku apps:create inflector
[ok]      dokku git:sync inflector

Summary: 2 tasks · 0 changed · 2 ok · 0 skipped · 0 errors  (took 0.6s)
```

## Previewing with plan

Before changing anything, you can preview what `apply` would do. `docket plan` reads the live
server and reports the differences without running any mutating command - the same idea as
`terraform plan` or `git diff`:

```bash
docket plan
```

```text
==> Play: tasks
[+]       dokku apps:create inflector
[+]       dokku git:sync inflector

Plan: 2 task(s); 2 would change, 0 in sync, 0 error(s).
```

This is the safest way to understand a recipe you did not write, or to gate a deploy in CI.

## Next steps

- [Command reference](command-reference.md) - every command and flag
- [Recipes](recipes.md) - the recipe file format, plays, and multi-app recipes
- [Inputs](inputs.md) - parameterize a recipe with variables
- [Task envelope](task-envelope.md) - tags, conditionals, loops, and error handling per task
- [Remote execution](remote-execution.md) - drive a remote Dokku server over SSH
- [Tasks](tasks/README.md) - reference for every task type
