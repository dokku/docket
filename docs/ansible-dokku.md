# Wrapping docket from ansible-dokku

[ansible-dokku](https://github.com/dokku/ansible-dokku) is a collection of hand-written Ansible
modules that each shell out to `dokku` directly. docket does the same work, so the two carry two
implementations of "read the current state, decide, mutate". This page is the contract for collapsing
that into one: what a module generates, how it hands it to docket, and how it turns what comes back
into Ansible's `changed` and `failed`.

It is written for whoever is doing that migration. If you are writing recipes by hand, you want
[recipes](recipes.md) instead.

## The shape of the integration

An Ansible module runs once per task and reports one result, so the natural unit is **one docket
invocation per module invocation**, carrying a recipe with a single play and a single task. The
module builds that recipe as JSON, pipes it to docket on stdin, and reads the result off the exit
code and the `--json` event stream.

Nothing stops a wrapper from batching a whole play into one recipe, and docket is happy to run it -
but Ansible has nowhere to put a multi-task result, so the per-task shape is what the rest of this
page assumes.

## Invoking docket

`apply`, `plan`, and `validate` all read a recipe from stdin when the path is `-`. The three calls a
wrapper needs:

| Purpose | Command |
|---------|---------|
| Normal run | `docket apply --tasks-format json5 --json --detailed-exitcode -` |
| `check_mode` | `docket plan --tasks-format json5 --json --detailed-exitcode -` |
| Argument checking, offline | `docket validate --tasks-format json5 --json -` |

Three notes on the flags:

- `--tasks-format` states the format outright. Without it docket sniffs the first non-whitespace byte
  of stdin, which would get a JSON payload right today, but a generator should not depend on a
  heuristic it does not control. `json5` is the canonical spelling and every JSON document is valid
  JSON5; `json` is accepted as a synonym.
- `--json` swaps the human formatter for the JSON-lines event stream described in
  [JSON output](json-output.md). Without it there is nothing machine-readable to parse.
- `--detailed-exitcode` makes `apply` exit `2` when something changed. Without it `apply` exits `0`
  whether or not it changed anything, and `changed` has to come out of the event stream.

For a remote server, pass `--host user@host:port` and optionally `--sudo`; see
[remote execution](remote-execution.md). docket reuses one SSH connection for the whole invocation,
which for the per-task shape means one connection per Ansible task.

Reading the recipe from stdin consumes it, so a `dokku` command that would otherwise inherit the
caller's stdin sees end-of-file. No task depends on this - every task that streams data to `dokku`
supplies it explicitly.

## The payload

A recipe is a list of plays; each play has a `tasks` list; each task entry has a `name` plus exactly
one task-type key whose value is the task's fields. For the per-task shape that is one play with one
task:

```json
[
  {
    "name": "dokku_config",
    "tasks": [
      {
        "name": "set config on api",
        "dokku_config": {
          "app": "api",
          "restart": true,
          "config": { "LOG_LEVEL": "info" },
          "state": "present"
        }
      }
    ]
  }
]
```

The play `name` and the task `name` are echoed back on every event as `play` and `name`. Set the task
`name` to the Ansible task's name so a wrapper can correlate events without tracking order.

Each task's fields are documented on its own page under [tasks](tasks/README.md). Omit a field to get
its default; do not send an explicit `null`.

### Generate a fully resolved recipe

Two rules keep a generated payload out of trouble.

**Do not emit an `inputs:` block.** Inputs exist so a human can parameterize a recipe from the command
line; they become real `--<name>` flags and add a second layer of substitution the wrapper would have
to reason about. An Ansible module already has concrete values - write them straight into the
payload.

**Escape literal `{{` in every value.** docket renders the whole recipe as text through
[sigil](https://github.com/gliderlabs/sigil) *before* parsing it, so a `{{` anywhere in the file is a
template action regardless of which field it sits in. An app config value of `{{ .Values.name }}`
would render to `<no value>`, and a value like `hello {{ world` fails the render outright:

```text
! line 1: template render error: template: tasks.yml:1: bad character U+0022 '"'
```

`docket validate` reports that as a `template_render` problem, which is the cheapest place to catch
it.

The fix is to replace every literal `{{` with ``{{ `{{` }}`` - a template action that emits two
opening braces. Everything else in the value passes through untouched:

| Intended value | Emit in the payload |
|----------------|---------------------|
| `{{ .Values.name }}` | ``{{ `{{` }} .Values.name }}`` |
| `say {{hi}} and }} alone` | ``say {{ `{{` }}hi}} and }} alone`` |

Use the backtick form, not `{{ "{{" }}`. Both work in a YAML recipe, but JSON has to backslash-escape
an inner double quote, and sigil sees the raw file bytes - `{{ \"{{\" }}` is not a valid action.
Backticks need no JSON escaping.

Related but distinct: `unsafe_input_value` and the `dq` filter in
[inputs](inputs.md#special-characters-in-values) cover the other direction, where an input's *value*
breaks the scalar it is substituted into. A wrapper that follows the two rules above never hits it.

### Envelope keys worth passing through

Several Ansible task keywords have a direct docket equivalent, so a wrapper can forward them instead
of reimplementing them. They sit next to the task-type key, not inside it. See
[task envelope](task-envelope.md) for the full set.

| Ansible | docket | Notes |
|---------|--------|-------|
| `name` | `name` | Echoed on every event. |
| `when` | `when` | Different expression language: docket uses [expr](https://expr-lang.org/), not Jinja2. |
| `changed_when` | `changed_when` | Overrides the task's own verdict. |
| `failed_when` | `failed_when` | Overrides the task's own verdict. |
| `ignore_errors` | `ignore_errors` | Emits `"ignored": true` and drops the task from the error count. |
| `loop` | `loop` | Expands to one task event per item. |
| `register` | `register` | Only useful when batching several tasks into one recipe. |
| `tags` | `tags` | Filtered with `--tags` / `--skip-tags`. |

A wrapper that keeps the one-task-per-invocation shape will usually let Ansible handle `when`, `loop`,
and `register` itself and forward only `changed_when` / `failed_when` / `ignore_errors`, which need
docket's view of the result to evaluate.

## Reading the result

### Exit codes

| Command | `0` | `1` | `2` |
|---------|-----|-----|-----|
| `apply` | Completed; may or may not have changed anything | Read error, parse error, or a task errored | - |
| `apply --detailed-exitcode` | Completed; nothing changed | Same as above (errors win) | Completed; at least one task changed |
| `plan` | Completed, regardless of drift | Read error, parse error, or a probe errored | - |
| `plan --detailed-exitcode` | Completed; no drift | Same as above (errors win) | Completed; at least one task would change |
| `validate` | No problems | At least one problem, or the recipe could not be read | - |

`--list-tasks` returns before any task runs, so it is unaffected by `--detailed-exitcode` and exits
`0` or `1`. It exits `1` for a recipe it could not load, and for a play `when:` that fails to
evaluate - that predicate is resolved against the same context a run uses, so the failure is one the
run would hit too.

### Correlating tasks across runs

`name` is the key. It is stable for the same recipe: a task the wrapper emitted without a `name:`
is named after the [resource it manages](task-envelope.md#names-and-resource-addresses), so the same
task carries the same `name` in a `plan` run and the `apply` that follows it.

A wrapper that generates recipes should still set `name:` explicitly when it has a meaningful label
of its own - a hand-written name is never rewritten, whereas two generated names that address the
same resource are disambiguated with an ordinal that shifts if a task is inserted between them.

### Deriving `changed`

There are three signals, and they agree. Pick whichever fits the wrapper:

| Source | Read it as |
|--------|------------|
| Exit code `2` from `apply --detailed-exitcode` | `changed: true` |
| `summary.changed > 0` | `changed: true` for the run |
| `task.changed` on the per-task event | `changed: true` for that task |

The exit code is the cheapest, and for the one-task-per-invocation shape it is enough on its own. Note
that `apply` on its own exits `0` either way - there is no exit code that means "changed" without the
flag.

### Deriving `failed`

Exit `1` is failure; exit `0` and `2` are not. Within the stream, a failed task carries
`"status": "error"` and an `error` message. On an `apply` task it also carries `stdout`, `stderr`,
and `exit_code` when the failure came from a `dokku` subprocess; those map onto Ansible's `msg`,
`stdout`, `stderr`, and `rc`. A `plan` task carries only `error`, so a `check_mode` failure has no
`rc` to forward.

A task whose error was swallowed by `ignore_errors` carries `"ignored": true`, does not count toward
`summary.errors`, and does not affect the exit code - the same semantics Ansible gives the keyword.

One case needs handling separately. A load-time failure - an unreadable recipe, a parse error, an
unknown task type - happens before the emitter starts, so it produces **no JSON on stdout** and a
human-readable message on stderr. A wrapper must treat "non-zero exit with empty stdout" as a failure
whose detail is on stderr, and should strip ANSI escapes from it. The cleaner path is to run
`docket validate --json` first: it reports the same class of problem as structured
[`validate_problem` events](json-output.md#validate-problems) with a stable `code` field, which is
exactly what turning a bad module argument into an Ansible failure needs. A clean recipe validates
silently and exits `0`, so any output at all is a failure.

### `check_mode`

Ansible's `check_mode` maps onto `plan`, which reads the server and reports what `apply` would do
without mutating anything. Per task, `would_change` feeds `changed` and `mutations` - an itemized list
of the operations `apply` would perform - feeds `diff`.

Some tasks cannot read their state without running the underlying command, so they report drift on
every run and never converge. Which ones is a declared per-task property rather than a fixed list:
each task's [reference page](tasks/README.md) carries a **Probe support** section, the tasks index
marks them `(never converges)`, and `--list-tasks --json` carries a `probe` field
(`unsupported` for a task that reads nothing, `partial` for one that reads only some of its fields)
plus a `probe_caveat` naming what cannot be read. A wrapper that reports "no changes needed" should
read that field rather than treating a perpetual `would_change` as a bug.

That is a different thing from a probe that fails at runtime. When the probe itself fails - no
`dokku` binary, unreachable host - the task reports `"status": "error"` and `plan` exits `1` rather
than optimistically predicting a create. When a probe is merely inconclusive - an unknown property
key, an older plugin that rejects `:report --format json` - the task emits a `warning` event and
still mutates.

### Secrets

Values from inputs declared `sensitive: true` and from task fields tagged `sensitive:"true"` are
masked as `***` everywhere in the `apply` / `plan` / `validate` output, including `commands` and
`name`. A wrapper cannot read a secret back out of those streams, which is the point - but it also
means a wrapper must not diff a returned value against the one it sent.

`--list-tasks` masks on the same terms, on both the human and the `--json` path: an interpolated
secret comes back as `***` in `name`, `when`, `tags`, and `loop_item`. No task field is ever both an
identity key and `sensitive:"true"`, so a generated `name` never embeds a task-declared secret - but
a `sensitive: true` input interpolated into an identity field still reaches it, and is masked there
too. A wrapper that resumes a run cannot feed such a name back to `--start-at-task`, which matches
on the real value; emit an explicit `name:` on any task it needs to address.

## Module mapping

Every ansible-dokku module and the docket task it maps to. The plugin column lists the third-party
dokku plugin the row needs; blank means dokku core.

| ansible-dokku module | docket task | Plugin | Notes |
|----------------------|-------------|--------|-------|
| `dokku_acl_app` | `dokku_acl_app` | dokku-acl | Direct. |
| `dokku_acl_service` | `dokku_acl_service` | dokku-acl | Direct. |
| `dokku_app` | `dokku_app` | | Direct. |
| `dokku_builder` | `dokku_builder_property` | | Direct; both wrap `builder:set`. |
| `dokku_buildpacks` | `dokku_buildpacks` | | Direct. |
| `dokku_certs` | `dokku_certs` | | docket adds `cert_content` / `key_content` for inline PEM. |
| `dokku_checks` | `dokku_checks_toggle` | | `checks:enable` / `checks:disable`. |
| `dokku_clone` | `dokku_app` and `dokku_git_sync` | | The module runs core `git:sync`, and creates the app first. Emit both tasks. `version` becomes `git_ref`. |
| `dokku_config` | `dokku_config` | | docket also supports `state: absent` (`config:unset`). |
| `dokku_docker_options` | `dokku_docker_options` | | docket adds `process_type`. |
| `dokku_domains` | `dokku_domains` and `dokku_domains_toggle` | | `state: enable` / `disable` become `dokku_domains_toggle`; the rest map onto `dokku_domains`. |
| `dokku_git_sync` | none | dokku-git-sync | See [what cannot be delegated](#what-cannot-be-delegated-yet). |
| `dokku_global_cert` | `dokku_certs` with `global: true` | dokku-global-cert | docket folds the global certificate into one task. |
| `dokku_http_auth` | `dokku_http_auth` | dokku-http-auth | Direct. |
| `dokku_image` | `dokku_git_from_image` | | `user_name` / `user_email` become `git_username` / `git_email`. |
| `dokku_letsencrypt` | `dokku_letsencrypt` | dokku-letsencrypt | Direct. |
| `dokku_network` | `dokku_network` | | Direct. |
| `dokku_network_property` | `dokku_network_property` | | docket adds `state`. |
| `dokku_ports` | `dokku_ports` | | `mappings` strings become structured `port_mappings`. |
| `dokku_proxy` | `dokku_proxy_toggle` | | `proxy:enable` / `proxy:disable`. |
| `dokku_ps_scale` | `dokku_ps_scale` | | Direct. |
| `dokku_registry` | `dokku_registry_auth` and `dokku_registry_property` | | Credentials go to `dokku_registry_auth` (`registry:login`); `image` and `server` go to `dokku_registry_property` as `image-repo` and `server`. The module still declares the old third-party `dokku-registry` plugin; docket treats `registry` as core. |
| `dokku_resource_limit` | `dokku_resource_limit` | | Direct. |
| `dokku_resource_reserve` | `dokku_resource_reserve` | | Direct. |
| `dokku_service_create` | `dokku_service_create` | datastore plugin | docket adds `state: absent`, and exposes the create-time options as task fields (`image`, `image_version`, `custom_env`, and the rest) rather than through the environment. |
| `dokku_service_link` | `dokku_service_link` | datastore plugin | Direct. |
| `dokku_storage` | `dokku_storage_mount`, `dokku_storage_entry`, `dokku_storage_ensure` | | One `dokku_storage_mount` per entry in `mounts`. Host directories go to `dokku_storage_entry`; see [host directories](#host-directories). |

The mapping is not always command-for-command. `dokku_registry` drives `registry:set <app> username`
while `dokku_registry_auth` drives `registry:login`, and `dokku_proxy` reads its current state from
`config:get <app> DOKKU_DISABLE_PROXY` while docket reads the proxy plugin's report. In both cases
the intent matches even though the wire calls differ, which is the point of delegating - docket's
side is the one that stays current with dokku.

One mapping is not a field at all on the module side. `dokku_service_create` pins a service's image
by reading `POSTGRES_IMAGE` and friends out of Ansible's `environment:` keyword; docket sends the
same overrides as flags on `<service>:create`, so a wrapper translates them into task fields:
`<SERVICE>_IMAGE` becomes `image`, `<SERVICE>_IMAGE_VERSION` becomes `image_version`,
`<SERVICE>_CUSTOM_ENV` becomes the `custom_env` map, and `<SERVICE>_CONFIG_OPTIONS` becomes
`config_options`. Flags rather than environment variables because docket may be driving the server
over SSH, where variables put in front of the local process never reach the remote shell. The
remaining create-time options (`memory`, `shm_size`, the three network fields, `password`, and
`root_password`) have no module counterpart.

`image_drift` and `restart_apps` have no counterpart either, and are not create-time options at all:
they say what docket should do when the service already exists on some other image, which is a
question the module never asks because it stops at `<service>:exists`. A wrapper that translates
`POSTGRES_IMAGE` into `image` is reproducing the module's behaviour exactly by leaving `image_drift`
alone - the default reports the mismatch and changes nothing. Set it to `upgrade` only where the
wrapper's own contract says a pin change should recreate the container.

### Host directories

`dokku_storage` does raw filesystem work alongside its `storage:mount` calls, which it gets
away with because Ansible has already connected to the dokku host and is running there.
docket may be driving that host over SSH, where a local filesystem call would touch the wrong
machine entirely, so everything it does goes through a `dokku` subcommand. That caps it at
what `storage:create` expresses, and `storage:create` is exactly what `dokku_storage_entry`
wraps.

| `dokku_storage` | docket | Notes |
|-----------------|--------|-------|
| `create_host_dir: true` | `dokku_storage_entry` | `storage:create` creates the directory. Emit one entry task per distinct host directory, ahead of the `dokku_storage_mount` tasks that reference it. |
| the host path itself | `dokku_storage_entry.path` | Omit it to get dokku's default, `/var/lib/dokku/data/storage/<name>`. |
| `user` and `group` | `dokku_storage_entry.chown` | One value, not two: dokku's helper runs `chown -R <id>:<id>`, so a module call whose `user` and `group` differ cannot be expressed. The module's `"32767"` default is `chown: herokuish`. |
| `os.chmod(host_dir, 0o777)` | **stays in the wrapper** | dokku creates the directory `0755` and has no mode flag ([dokku/dokku#8913](https://github.com/dokku/dokku/issues/8913)). |
| `destroy_host_dir: true` | **stays in the wrapper** | `storage:destroy` deregisters the entry and leaves the docker-local directory on disk; nothing in dokku removes it ([dokku/dokku#8913](https://github.com/dokku/dokku/issues/8913)). `dokku_storage_entry` with `state: absent` covers the deregistration half. |
| `os.lexists("/home/dokku/<app>")` | nothing to forward | The module stats the filesystem to decide whether the app exists. docket asks dokku, so a wrapper drops the probe rather than translating it. |

Two constraints on `chown` are worth knowing before a wrapper leans on it. dokku refuses the
flag outright when the entry sits at a non-default `path`, and asks the operator to chown that
path themselves. And it is applied when the entry is created, not on every run: an entry that
already exists is left alone, so changing `chown` in a recipe is currently a no-op
([#439](https://github.com/dokku/docket/issues/439)).

Everything else `storage:create` accepts is a field on the task - `scheduler`, `size`,
`access_mode`, `storage_class`, `namespace`, `reclaim_policy`, `annotations`, and `labels` -
none of which the module has a counterpart for.

## Required and optional fields disagree

The two field sets were defined independently, so a module that treats a field as optional cannot
assume the task does, and vice versa. Every row below is a real divergence.

One wrinkle first: in nine modules the `DOCUMENTATION` block disagrees with the `argument_spec` in the
same file, and it is the spec that Ansible enforces. Those modules are `dokku_builder`, `dokku_certs`,
`dokku_docker_options`, `dokku_domains`, `dokku_global_cert`, `dokku_network_property`, `dokku_ports`,
`dokku_registry`, and `dokku_storage`. The table is built from the spec.

| Field | ansible-dokku | docket | What a wrapper has to do |
|-------|---------------|--------|--------------------------|
| `users` | Required on `dokku_acl_app` and `dokku_acl_service` | Optional | Nothing; a stricter caller is always safe. |
| `buildpacks` | Required | Optional | Nothing. |
| `config` | Required | Optional | Nothing. |
| `domains` | Required | Optional | Nothing. |
| `app` on `dokku_certs` | Required | Optional, but exactly one of `app` or `global` must be set | Nothing for the app case; the global case goes through the same task. |
| `cert` / `key` | Optional in the spec | Optional, but `state: present` requires `cert` + `key` or `cert_content` + `key_content` | A `state: present` call with no material passes Ansible's arg spec and fails `docket validate`. Validate before applying. |
| `phase` on `dokku_docker_options` | Required in the spec, optional in the docs | Required | Nothing; docket agrees with the spec. |
| `mappings` / `port_mappings` | Optional list of `"http:80:5000"` strings | Optional list of `{scheme, host, container}` objects, but required and non-empty for `state: present`, `absent`, and `set`, and no two may share a scheme and host port under `state: present` or `set` | Parse each string and restructure it. Omit the field entirely for `state: clear`, which rejects a list rather than ignoring one. A list carrying `"http:80:5000"` and `"http:80:6000"` passes the module's argument spec and fails `docket validate`, the same reuse dokku's `ports:add` and `ports:set` refuse. |
| `username` / `password` on `dokku_registry` | Both required, even for `state: absent` | Only `server` is required; credentials are required when `state: present` | A `state: absent` call carries credentials docket does not need. Drop them. |
| `app` on `dokku_builder`, `dokku_network_property` | Required in the docs, optional in the spec | Optional, paired with `global` | Nothing. |
| `app` on `dokku_storage` | Required | `dokku_storage_mount` requires `app` and `container_dir` | Split `mounts` into one task per entry, and split each `host:container` string. |
| `user` / `group` on `dokku_storage` | Two options, both defaulting to `"32767"` | `dokku_storage_entry.chown` is one value, and rejects anything that is not an ownership preset or a uid in 0-65535 | Collapse the pair into one value, and fail the module call when they differ - dokku chowns the owner and the group to the same id. |
| `build` on `dokku_clone` | Defaults to `true` | `dokku_git_sync.build` has no default, so it is off | Send `build: true` explicitly to preserve module behavior. |
| `state` on `dokku_image`, `dokku_service_create`, `dokku_network_property` | No `state` option at all | Present on all three | Nothing; each docket default matches the module's only behavior. Note that `dokku_git_from_image.state` defaults to `deployed`, not `present`, so do not send `present`. |

Two module-side defaults are worth knowing because they are not in the argument spec at all:
`dokku_config.restart` and `dokku_ps_scale.skip_deploy` have no declared default and are driven by
identity checks in the module body, making the effective defaults `true` and `false`. docket declares
the same two defaults outright, so the behavior matches.

## What cannot be delegated yet

One module, and it is tracked, so a wrapper can keep its implementation for now and drop it when the
task lands.

- **The `dokku_git_sync` module**
  ([#414](https://github.com/dokku/docket/issues/414)). It targets the commercial `dokku-git-sync`
  plugin (`git-sync:set <app> remote`), which docket has no task for. Mind the name collision:
  docket's `dokku_git_sync` is core `git:sync`, which is what the `dokku_clone` module does. The two
  are unrelated.

Separately, two pieces of `dokku_storage` stay in the wrapper permanently rather than pending a task:
the `0o777` chmod and the `destroy_host_dir` removal, neither of which dokku exposes a command for.
See [host directories](#host-directories).

## docket tasks with no ansible-dokku module

44 of docket's 73 task types have no ansible-dokku counterpart. They are not blockers for the
migration, but they are what a wrapper gains access to for free once it delegates.

Per-plugin property tasks, each wrapping a `<plugin>:set`:

`dokku_app_json_property`, `dokku_apps_property`, `dokku_builder_dockerfile_property`,
`dokku_builder_herokuish_property`, `dokku_builder_lambda_property`, `dokku_builder_nixpacks_property`,
`dokku_builder_pack_property`, `dokku_builder_railpack_property`, `dokku_buildpacks_property`,
`dokku_builds_property`, `dokku_caddy_property`, `dokku_checks_property`, `dokku_cron_property`,
`dokku_git_property`, `dokku_haproxy_property`, `dokku_letsencrypt_property`, `dokku_logs_property`,
`dokku_nginx_property`, `dokku_openresty_property`, `dokku_proxy_property`, `dokku_ps_property`,
`dokku_scheduler_property`, `dokku_traefik_property`.

The scheduler-k3s set: `dokku_scheduler_k3s_annotations`, `dokku_scheduler_k3s_autoscaling_auth`,
`dokku_scheduler_k3s_chart`, `dokku_scheduler_k3s_labels`, `dokku_scheduler_k3s_profile`,
`dokku_scheduler_k3s_property`, plus `dokku_scheduler_docker_local_property`.

Finer-grained HTTP auth than the module's on/off switch: `dokku_http_auth_allowed_ip`,
`dokku_http_auth_domain`, `dokku_http_auth_user`.

Deploy sources and app lifecycle: `dokku_app_clone` - which runs `apps:clone` to copy one app to
another, not to be confused with the similarly-named module that syncs a git repository -
`dokku_app_lock`, `dokku_git_auth`, and `dokku_git_from_archive`.

Everything else: `dokku_maintenance`, `dokku_maintenance_custom_page`, `dokku_plugin`,
`dokku_service_backup`, `dokku_service_expose`, `dokku_service_property`, `dokku_ssh_key`.

## See also

- [Command reference](command-reference.md) - every flag named on this page
- [JSON output](json-output.md) - the event schemas and the JSON Schema files
- [Recipes](recipes.md) - the recipe format and how docket finds one
- [Task envelope](task-envelope.md) - `when`, `loop`, `register`, and error handling per task
- [Tasks](tasks/README.md) - the fields of every task type
- [Remote execution](remote-execution.md) - driving a remote server over SSH
