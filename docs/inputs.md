# Inputs

Inputs are the variables of a recipe. They let you write one recipe and reuse it across apps or
environments by supplying different values, instead of copy-pasting the file and editing names by
hand. You declare an input once, reference it in your tasks with a `{{ .name }}` template, and
override it at run time.

## Declaring inputs

Declare inputs in an `inputs:` block and reference them in task bodies:

```yaml
---
- inputs:
    - name: name
      default: "inflector"
      description: "Name of app to be created"
      required: true
  tasks:
    - dokku_app:
        app: "{{ .name }}"
    - dokku_git_sync:
        app: "{{ .name }}"
        remote: http://github.com/cakephp/inflector.cakephp.org
```

Each input supports these properties:

| Property | Type | Default | Notes |
|----------|------|---------|-------|
| `name` | string | `""` | The variable name used in `{{ .name }}` templates. |
| `default` | bool / float / int / string | zero value for the type | Used when no value is supplied. |
| `description` | string | `""` | Shown in `--help` output. |
| `required` | bool | `false` | Flagged by `validate --strict` when it has no default and no supplied value. |
| `type` | string | `string` | One of `bool`, `float`, `int`, `string`. Controls how supplied values are coerced. |

Inputs are substituted into task bodies with the [sigil](https://github.com/gliderlabs/sigil)
template library, which wraps Go's `text/template`. Anything sigil supports is available in a task
body. Inputs themselves must not reference other variables - they are resolved first, in a separate
phase, and then injected.

## Input names

Because an input is referenced as `{{ .name }}`, its `name` must be a valid template variable: a
letter or underscore followed by letters, digits, or underscores. A name with any other character -
a hyphen is the common case (`my-app`, `db-host`) - cannot be used with `{{ .name }}` because Go's
`text/template` rejects it. Such a name is reported as `invalid_input_name` by `docket validate`
(and rejected by `plan` / `apply`) rather than failing later with a cryptic template error. Rename
the input to use underscores, for example `my_app`:

```yaml
---
- inputs:
    - name: my_app       # not my-app
      default: web
  tasks:
    - dokku_app:
        app: "{{ .my_app }}"
```

## Special characters in values

An input value is substituted into the task body as raw text before the recipe is parsed. That
means a value with a character that collides with the surrounding quotes breaks the scalar. Given
the scaffolded shape `app: "{{ .app }}"`, a value containing a double quote renders the invalid
`app: "ab"cd"`, and `docket validate` (as well as `plan` / `apply`) reports it as
`unsafe_input_value`, naming the offending input, instead of a cryptic YAML error.

The robust fix is the `dq` filter, which escapes a value for a double-quoted scalar. Use it **inside
the quotes** so it handles any value - double quotes, both quote types, even a newline - while the
recipe stays valid YAML/JSON5 for `docket validate` and `docket fmt`. The `docket init` scaffold uses
it for exactly this reason:

```yaml
---
- inputs:
    - name: motd
      default: 'say "hi"'
  tasks:
    - dokku_config:
        app: web
        config:
          MOTD: "{{ .motd | dq }}"          # -> MOTD: "say \"hi\""
    - dokku_domains:
        app: web
        domains: ["{{ .motd | dq }}.example.com"]   # works mid-string too
```

For a value that only contains one kind of quote, choosing a compatible quote style also works and
needs no filter: a single-quoted body tolerates a double quote, and a double-quoted body tolerates a
single quote.

```yaml
    - dokku_config:
        app: web
        config:
          MOTD: '{{ .motd }}'               # single quotes tolerate a " in the value
```

Note that `dq` must sit inside a double-quoted scalar. Do not leave the reference unquoted
(`app: {{ .app | dq }}`): an unquoted `{{` is not valid YAML, so `docket validate` and `docket fmt`,
which read the recipe before it is rendered, would reject the file.

## Overriding inputs

Override an input on the command line by passing its name as a flag. Omit it to use the default:

```bash
# from the same directory as the tasks.yml
docket apply --name lollipop
```

Any inputs you declare also appear in the recipe's `--help` output, so `docket apply --help` is a
quick way to see what a recipe accepts.

An input you do not supply falls back to its declared `default:`. There is no interactive prompt -
docket is meant to run unattended in scripts and CI. An input declared `required: true` with no
default and no supplied value renders as an empty string; `docket validate --strict` flags exactly
that case so a recipe that cannot run without a runtime override fails the lint instead of deploying
with a blank value.

These input names collide with docket's own command flags and cannot be used as input names.
Declaring one is reported as `reserved_input_name` by `docket validate` (and rejected by `plan` /
`apply`) rather than silently shadowing the flag:

- `accept-new-host-keys`
- `detailed-exitcode`
- `fail-fast`
- `host`
- `json`
- `list-tasks`
- `no-color`
- `play`
- `skip-tags`
- `start-at-task`
- `strict`
- `sudo`
- `tags`
- `tasks`
- `vars-file`
- `verbose`

`help`, `v`, and `version` are handled by the CLI framework rather than registered as flags, so
they are usable as input names.

## Layered values with `--vars-file`

For anything beyond a couple of overrides, keep values in a file and pass it with `--vars-file`.
This is how you manage per-environment configuration: a `prod.yml` and a `staging.yml`, each
holding the values for that environment. `apply`, `plan`, and `validate` all accept it, and the
flag is repeatable so you can layer a base file under an environment-specific one.

A vars file is a flat map of input name to value:

```yaml
# prod.yml
app: api
repo: https://github.com/example/api.git
replicas: 3
debug: false
```

JSON works the same way - any path ending in `.json` is parsed as JSON, anything else as YAML:

```json
{
  "app": "api",
  "repo": "https://github.com/example/api.git",
  "replicas": 3,
  "debug": false
}
```

Common patterns:

```bash
# Layer environment-specific values over the recipe defaults.
docket apply --tasks tasks.yml --vars-file prod.yml

# Stack a base file under a per-environment override, then override one value on the CLI.
docket plan --tasks tasks.yml \
  --vars-file base.yml --vars-file prod.yml \
  --app=api-canary
```

Values are coerced to each input's declared `type`:

- `string`: any scalar (a YAML boolean `true` becomes the string `"true"`).
- `int`: whole numbers, including numeric strings and whole-valued JSON numbers.
- `float`: floats, ints, and parseable numeric strings.
- `bool`: native booleans, or a string spelled `true`/`yes`/`on`/`y` (or `false`/`no`/`off`/`n`).
  A `--name=value` flag on the command line is parsed separately by pflag, which accepts
  `true`/`false`/`1`/`0` but not `yes`/`on`.

A key in a vars file that does not match any declared input is a hard error, with a suggestion for
the closest real name:

```text
unknown input "appp" in --vars-file prod.yml; did you mean "app"?
```

## Precedence

When the same input is set in more than one place, the highest layer wins. From lowest to highest:

| Layer | Source |
|-------|--------|
| 1 | File-level `inputs:` defaults (declared on a play with no tasks) |
| 2 | Per-play `inputs:` defaults (declared on a play that also has tasks) |
| 3 | `--vars-file <path>` (repeatable; later files override earlier ones) |
| 4 | `--name=value` CLI flags (always win) |

### Per-play inputs precedence

In a [multi-play recipe](recipes.md#multi-play-recipes), there are two kinds of input defaults:

- A **file-level input** is declared on a play that has no tasks. It is visible to every play.
- A **play-local input** is declared on a play that also has tasks. It is visible only to that
  play's tasks - not to other plays, and not to any play's `when:` (including its own).

```yaml
---
- inputs:
    - { name: env, default: prod }     # file-level: visible to every play
- name: api
  inputs:
    - { name: app, default: api }       # play-local: visible to api's tasks only
  tasks:
    - dokku_app: { app: "{{ .app }}" }
```

## See also

- [Recipes](recipes.md) - plays and multi-play structure
- [Task envelope](task-envelope.md) - using inputs in `when:` and `loop:` expressions
- [Command reference](command-reference.md#docket-validate) - `validate --strict` checks required inputs
