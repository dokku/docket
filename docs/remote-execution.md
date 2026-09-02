# Remote execution

By default docket runs `dokku` commands on the local machine, so you would run it on the Dokku
server itself. Often you would rather drive a remote server from your laptop or a CI runner without
installing docket there. Set `DOKKU_HOST` (or pass `--host`) and docket routes every `dokku`
invocation through `ssh` instead:

```bash
# Apply against a remote server.
DOKKU_HOST=deploy@dokku.example.com docket apply

# Same, via the flag (which overrides the env var).
docket apply --host deploy@dokku.example.com:2222
```

The host is `[user@]host[:port]`. All invocations in one run share a single SSH connection through
OpenSSH ControlMaster multiplexing, so you pay the connection cost once.

Because docket shells out to your own `ssh` binary, everything `ssh` already knows works without
extra configuration: your `~/.ssh/config`, `ProxyJump`, ssh-agent, and `known_hosts` all apply.
You do not need to teach docket about any of it.

| Flag | Effect |
|------|--------|
| `--host <user@host:port>` | The remote host to ssh into. Overrides `DOKKU_HOST`. |
| `--sudo` | Run `dokku` as root, with passwordless sudo only. Equivalent to `DOKKU_SUDO=1`. |
| `--accept-new-host-keys` | Pass `-o StrictHostKeyChecking=accept-new` so SSH trusts an unknown host on first connect. Equivalent to `DOKKU_SSH_ACCEPT_NEW_HOST_KEYS=1`. |

`--sudo` works on both sides of the transport: with `--host` it wraps the remote invocation in
`sudo -n`, and without one it runs the local `dokku` under `sudo -n -u root`. Either way it covers
`dokku` alone - the `docker` and `curl` calls a few tasks make locally are docket's own plumbing and
are never elevated. `-n` never prompts, so the account has to have passwordless sudo already.

`--accept-new-host-keys` is convenient in CI, where seeding `known_hosts` ahead of time is awkward,
but it gives up man-in-the-middle protection on the first connection. When you can, prefer seeding
the key yourself:

```bash
ssh-keyscan dokku.example.com >> ~/.ssh/known_hosts
```

## A recipe that spans hosts

The flags above set one target for the whole run. A play can name its own instead, which is how a
migration or a fan-out deploy is written as a single recipe:

```yaml
---
- name: drain the old server
  host: deploy@old.example.com
  tasks:
    - dokku_maintenance: { app: api }

- name: install on the new one
  host: deploy@new.example.com
  tasks:
    - dokku_app: { app: api }
```

Each play opens its own multiplexed SSH connection and docket closes all of them at the end of the
run. A play that names no `host:` uses the run-wide target, and `sudo` / `accept_new_host_keys`
carry over to a play that redirects unless it overrides them. See
[recipes](recipes.md#plays-on-different-servers) for the full rules.

## Argument quoting

OpenSSH joins the words of a remote command into a single string that the remote login shell
re-parses, so docket shell-quotes each `dokku` argument before sending it. Values containing spaces
or shell metacharacters - a `start-cmd` like `npm run start`, an nginx `access-log-format` with
`$remote_addr`, or a backup schedule like `0 3 * * *` - reach the remote `dokku` verbatim, exactly
as they would when running locally. An argument that cannot be represented for a POSIX shell (one
containing a tab, newline, or null byte) is rejected with an `ssh:` error rather than sent in a
corrupted form.

## Environment variables stay local

Only the command line crosses to the server. Variables you set around docket - `FOO=bar docket
apply --host ...`, or anything exported in your shell - decorate the local `ssh` process, and
docket configures no `SendEnv`, so they reach the remote `dokku` only if your own `ssh` config
forwards them and the server opts in with `AcceptEnv`. Do not rely on that.

Values meant for the server belong in the recipe, where they become arguments docket sends
explicitly: [`dokku_config`](tasks/dokku_config.md) for an app's config, and
[`dokku_service_create`](tasks/dokku_service_create.md)'s `custom_env` for the environment a service
container starts with.

## Reading errors

Errors are categorized so you can tell which side failed. SSH-level failures (refused connection,
auth, host-key mismatch) carry an `ssh:` prefix; remote `dokku` failures carry a `dokku:` prefix:

```text
[error]   create app
          ! ssh: ssh deploy@dokku.example.com: Permission denied (publickey).
```

```text
[error]   add buildpack
          ! dokku: app foo does not exist
```

## File paths are remote

When a task references a file path - for example the `cert` and `key` fields on `dokku_certs` -
that path is interpreted on the **remote** host, not your local machine. docket does not upload
local files in this release, so any referenced file must already exist on the server. Place it there
before the run.

Some tasks offer an inline alternative that sidesteps this constraint. `dokku_certs`, for
instance, accepts `cert_content` and `key_content` strings; docket streams the PEM material to
dokku as a tarball over stdin, so the bytes never have to live as files on the remote.

## See also

- [Command reference](command-reference.md) - the commands you run over SSH
- [dokku_certs](tasks/dokku_certs.md) - a task that references server-side file paths
