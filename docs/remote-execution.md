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
| `--sudo` | Wrap the remote `dokku` call in `sudo -n` (passwordless sudo only). Equivalent to `DOKKU_SUDO=1`. |
| `--accept-new-host-keys` | Pass `-o StrictHostKeyChecking=accept-new` so SSH trusts an unknown host on first connect. Equivalent to `DOKKU_SSH_ACCEPT_NEW_HOST_KEYS=1`. |

`--accept-new-host-keys` is convenient in CI, where seeding `known_hosts` ahead of time is awkward,
but it gives up man-in-the-middle protection on the first connection. When you can, prefer seeding
the key yourself:

```bash
ssh-keyscan dokku.example.com >> ~/.ssh/known_hosts
```

## Argument quoting

OpenSSH joins the words of a remote command into a single string that the remote login shell
re-parses, so docket shell-quotes each `dokku` argument before sending it. Values containing spaces
or shell metacharacters - a `start-cmd` like `npm run start`, an nginx `access-log-format` with
`$remote_addr`, or a backup schedule like `0 3 * * *` - reach the remote `dokku` verbatim, exactly
as they would when running locally. An argument that cannot be represented for a POSIX shell (one
containing a tab, newline, or null byte) is rejected with an `ssh:` error rather than sent in a
corrupted form.

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
