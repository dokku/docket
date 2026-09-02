# Recipes

A recipe is the file docket reads to know what to do. This page covers the file itself: the
formats it can be written in, how docket finds it, and how a recipe is structured into plays.

## File formats

docket reads recipes in either YAML or JSON5. You can write whichever you prefer; they produce
identical results. The format is chosen from the file extension:

| Extension | Parser |
|-----------|--------|
| `.yml`, `.yaml` | YAML (`gopkg.in/yaml.v3`) |
| `.json`, `.json5` | JSON5 (a strict superset of JSON) |

YAML is the most common choice and is used throughout this documentation. JSON5 exists because it
is friendlier than plain JSON for hand-written config: it allows `// line` and `/* block */`
comments, trailing commas, and unquoted keys. Any existing JSON file is already valid JSON5, so it
parses unchanged.

The same recipe in YAML and JSON5 behaves identically - templates, conditionals, every envelope
key, and every task type work the same way. This YAML recipe:

```yaml
---
- tasks:
    - dokku_app:
        app: inflector
```

is equivalent to this JSON5 recipe:

```json5
[
  {
    // create the app
    tasks: [
      { dokku_app: { app: "inflector" } },
    ],
  },
]
```

## How docket finds your recipe

When you do not pass `--tasks`, docket looks in the current directory for these files, in order,
and uses the first one that exists:

1. `tasks.yml`
2. `tasks.yaml`
3. `tasks.json`

If none exist, the run errors and lists the names it looked for, so a typo is easy to spot. To use
a different path, pass `--tasks`; the format is detected from that path's extension (an unknown
extension is treated as YAML):

```bash
docket apply --tasks deploy/production.yml
docket apply --tasks deploy/production.json
```

### Piping a recipe in

A recipe does not have to be a file. Pass `-` and docket reads it from stdin, which is how you feed
it a recipe another tool just generated:

```bash
docket export --output - | docket apply -
docket init --output - | docket validate -
docket export --output - --format json5 | docket apply --tasks-format json5 -
```

The format is sniffed from the first non-whitespace byte - `[`, `{`, `//`, or `/*` means JSON5,
anything else means YAML. Pass `--tasks-format yaml` or `--tasks-format json5` when that guess would
be wrong, which happens with a YAML recipe written in flow style, since it opens with `[`. The same
flag overrides a misleading file extension:

```bash
docket validate --tasks recipe.txt --tasks-format json5
```

`--tasks-format` is the reading side. The writing side is `--format` on `init` and `export`, which
states the format of what they emit - necessary when the destination is `-`, since there is no
extension to infer from.

A piped recipe behaves like any other: its `inputs:` still become `--<name>` flags, and it wins over
a `tasks.yml` in the current directory.

## Plays

A recipe is a **list of plays**. A play is a named group of tasks that share settings. The
smallest recipe is a single play with a `tasks:` list - the shape you have seen so far:

```yaml
---
- tasks:
    - dokku_app: { app: api }
    - dokku_config: { app: api, config: { LOG_LEVEL: info } }
```

A play can carry these keys:

| Key | What it does |
|-----|--------------|
| `name` | A human label for the play, shown in the output. Defaults to `play #N`, except a single-play recipe uses the legacy `tasks` header. |
| `tags` | A tag list inherited by every task in the play. Combines with per-task `tags`. See [task envelope](task-envelope.md#tags). |
| `when` | A condition. When it is false, the whole play is skipped. |
| `host` | The server this play's tasks run against, as `[user@]host[:port]`. Overrides `--host` / `DOKKU_HOST` for this play. See [remote execution](remote-execution.md#a-recipe-that-spans-hosts). |
| `sudo` | Run this play's `dokku` calls as root. Overrides `--sudo`. |
| `accept_new_host_keys` | Trust an unknown SSH host key on first connect for this play. Overrides `--accept-new-host-keys`. |
| `inputs` | Variable defaults for this play. See [inputs](inputs.md). |
| `tasks` | The play's list of tasks. |

## Multi-play recipes

Because a recipe is a list, you can describe several coordinated apps or services in one file by
writing more than one play. docket runs the plays top to bottom:

```yaml
---
- name: api
  tags: [web]
  inputs:
    - { name: app, default: api }
  tasks:
    - dokku_app:    { app: "{{ .app }}" }
    - dokku_config: { app: "{{ .app }}", config: { LOG_LEVEL: info } }

- name: worker
  when: 'env != "preview"'
  inputs:
    - { name: app, default: worker }
  tasks:
    - dokku_app: { app: "{{ .app }}" }
```

Single-play recipes keep working unchanged, because a single play is just a one-element list.

### Plays on different servers

A play can name its own `host:`, which is what makes a migration or a fan-out deploy expressible as
one recipe:

```yaml
---
- name: drain the old server
  host: deploy@old.example.com
  tasks:
    - dokku_maintenance: { app: api }

- name: install on the new one
  host: deploy@new.example.com
  sudo: true
  tasks:
    - dokku_app: { app: api }
```

A play that names no `host:` runs against `--host` / `DOKKU_HOST` as before, so every recipe written
without these keys behaves exactly as it did.

The three keys override **individually**, not as a group. A play that sets `host:` and nothing else
still gets the run's `--sudo` and `--accept-new-host-keys`, which is what lets the example above
send both plays to servers that need root without repeating the flag. A play that wants the opposite
says so explicitly:

```yaml
- name: a server where dokku runs unprivileged
  host: deploy@other.example.com
  sudo: false
  tasks: ...
```

`docket validate` checks each `host:` offline, so a typo is caught before anything connects rather
than surfacing as an `ssh:` failure partway through the run. `docket plan --list-tasks` and the play
headers in `plan` and `apply` all name the host each play resolved to.

### Running one play with `--play`

To run a single play out of a larger recipe, name it with `--play`:

```bash
docket apply --tasks tasks.yml --play api
docket plan  --tasks tasks.yml --play api --tags deploy
```

`--play` composes with [`--tags` / `--skip-tags`](task-envelope.md#tags): the play filter narrows
to one play, then the tag filter applies to the tasks inside it. An unknown play name produces an
error listing the available plays.

Those listed names are masked like every other name docket prints, so a play named after a
`sensitive: true` input is safe to paste into a ticket. `--play` still matches on the real name,
which means a name that masks down to `***` cannot be copied out of the error and used verbatim -
give any play you need to select by name a spelling that carries no secret.

### How a play's `when` is evaluated

A play-level `when:` is checked against the file-level variables only: file-level input defaults,
plus any `--vars-file` and CLI overrides. A play's own `inputs:` are deliberately not visible to
its own `when:` (that would be circular), and one play's inputs are never visible to another play's
`when:`. Per-task `when:` inside the play does see the play's own inputs. See
[inputs](inputs.md#per-play-inputs-precedence) for the full precedence rules.

## Error handling across plays

By default, an error in a task aborts only the **current play**, and the next play still runs. This
keeps one broken app from blocking the rest of a multi-app recipe. If you would rather stop the
entire run on the first error, pass `--fail-fast`:

```bash
docket apply --tasks tasks.yml             # default: stop this play, continue to the next
docket apply --tasks tasks.yml --fail-fast # stop the whole run on the first error
```

When a play is skipped by its `when:`, the summary line gains a `· N play skipped` segment:

```text
==> Play: api
[ok]      dokku apps:create api
[changed] dokku git:sync api

==> Play: worker  (skipped: when "env != \"preview\"")

==> Play: web
[ok]      dokku apps:create web
[changed] dokku domains:set web

Summary: 4 tasks · 2 changed · 2 ok · 0 skipped · 0 errors · 1 play skipped  (took 5.1s)
```

## See also

- [Inputs](inputs.md) - parameterize a recipe with variables and `--vars-file`
- [Task envelope](task-envelope.md) - per-task tags, conditionals, loops, and error handling
- [Command reference](command-reference.md) - flags for `apply`, `plan`, `validate`, and `fmt`
