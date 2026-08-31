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
| `default` | bool / float / int / string | zero value for the type | Used when no value is supplied. See [types and defaults](#types-and-defaults). |
| `description` | string | `""` | Shown in `--help` output. |
| `required` | bool | `false` | Stops `plan` / `apply` when it has no default and no supplied value; flagged offline by `validate --strict`. |
| `sensitive` | bool | `false` | Masks the value as `***` wherever docket prints it. See [Sensitive inputs](#sensitive-inputs). |
| `type` | string | `string` | One of `bool`, `float`, `int`, `string`. Controls how supplied values are coerced. |

Inputs are substituted into task bodies with the [sigil](https://github.com/gliderlabs/sigil)
template library, which wraps Go's `text/template`. Anything sigil supports is available in a task
body. Inputs themselves must not reference other variables - they are resolved first, in a separate
phase, and then injected.

## Types and defaults

`default:` is optional on every type. Omit it and the input starts at the zero value for its `type:` -
`""` for a `string`, `0` for an `int` or a `float`, `false` for a `bool`:

```yaml
---
- inputs:
    - { name: debug, type: bool }        # starts false
    - { name: replicas, type: int }      # starts 0
    - { name: app_name, default: web }
  tasks:
    - dokku_app: { app: "{{ .app_name }}" }
```

A default you do write has to be readable as the type it is declared under. `docket validate`
reports one that is not as `invalid_input_default`, and a `type:` outside the four above as
`invalid_input_type`, both naming the input and the line it sits on; `plan` and `apply` refuse the
recipe offline with the same message rather than running it.

A `bool` default is spelled `true`, `yes`, `on`, `y`, `t`, or `1` (or `false`, `no`, `off`, `n`,
`f`, `0`), in any case - `True` and `TRUE` read the same as `true`. That is the same vocabulary a
`--vars-file` uses, and it is a superset of pflag's, which is what parses a `--debug=1` typed on the
command line, so every spelling the command line accepts is also a spelling a `default:` accepts.
The reverse does not hold: pflag takes only `true`/`false`/`1`/`0`/`t`/`f`, so `--debug=yes` is not
a command line docket accepts.

Whatever the spelling, the input reaches the recipe as its declared type - a `bool` is a boolean and
an `int` is a number, not the text you wrote. So naming an input on its own is a truthiness test,
both in a [`when:`](task-envelope.md#when-run-a-task-conditionally) and in a `{{ if }}`:

```yaml
---
- inputs:
    - { name: debug, type: bool }
  tasks:
    - name: only when debugging
      when: debug
      dokku_app: { app: "web{{ if .debug }}-verbose{{ end }}" }
```

An input left at its zero value is false there, and renders as `false`, `0`, or the empty string.

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
default and no supplied value stops the run before the first task, on every type:

```text
 !     Missing flag '--deploy_token'
```

`docket validate --strict` flags exactly that case offline, as `input_missing`, so a recipe that
cannot run without a runtime override fails the lint instead of failing the deploy. Declaring a
`default:` satisfies the requirement, and so does supplying a value on the command line or through
a `--vars-file` - a value being the operative word: `--name=` types the flag and supplies nothing,
and is refused the same way passing nothing at all is.

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
- `tasks-format`
- `vars-file`
- `verbose`

`help`, `v`, and `version` are handled by the CLI framework rather than registered as flags, so
they are usable as input names.

## Sensitive inputs

An input declared `sensitive: true` holds a secret. Docket registers whatever value the input
resolves to - whichever layer [precedence](#precedence) picked, a declared `default:` included -
and replaces every occurrence of that literal with `***` in the text it prints.

```yaml
---
- inputs:
    - name: deploy_token
      description: "Token the sync reads the private repo with"
      required: true
      sensitive: true
  tasks:
    - dokku_git_sync:
        app: api
        remote: "https://x-access-token:{{ .deploy_token }}@github.com/example/api.git"
```

Masking is display-only. The task still receives the real value and still sends it to the server;
this is not encryption, and it is not storage. The recipe on disk, the `--vars-file` beside it, and
anything `docket fmt` writes all carry the value in the clear. `sensitive:` governs what docket
prints, nothing else.

What it prints is every stream docket emits: the human output of `apply`, `plan`, and `validate`,
the `--verbose` command echoes, `--json` on all three, `--list-tasks` on both the human and the JSON
path, error messages, the hints an unmatched `--play` or `--start-at-task` prints, and the
`DOKKU_TRACE` debug log. Inside those, a secret is masked wherever it landed: task names, play
names, tags, `when:` predicates and the errors they raise, and loop items at any depth, object keys
included. [JSON output](json-output.md) has the field-by-field list.

Docket's own vocabulary is never masked. The `type`, `phase`, `probe`, and `status` fields are fixed
enums rather than recipe content, and decorations like `(group)` and `[block]` are docket's words
rather than yours - an input whose value happens to be `group` does not blank them out.

A task type can also mark its own fields secret, with no input involved: `dokku_config` treats every
value in its `config:` map that way, and a field that exists to carry a credential - a
`dokku_registry_auth` password, a `dokku_certs` key - is marked in the task's definition. Those
values are registered and masked on identical terms. The `sensitive:` input property is how you
protect a value the task would otherwise have no reason to suspect, like the token in the `remote:`
above. See [Writing tasks](writing-tasks.md) for the task side.

### Masking widens rather than narrows

Masking is literal substring replacement, and docket registers every spelling it can print a value
in rather than only the one you supplied. A value carrying leading or trailing whitespace masks its
trimmed spelling too, because a loop item is trimmed on its way into a task name. A value that a
generated resource address has to quote masks its Go-escaped spelling too, so `sec"ret` masks both
itself and `sec\"ret`. Padding or punctuating a secret widens what masks; it never narrows it.

The cost of substring replacement is that a short or common value masks unrelated output as well.
`sensitive: true` is accepted on any `type`, but what gets registered is the value as substituted -
an `int` registers its digits, a `bool` registers `true` or `false` - so keep it for `string` inputs
holding real secrets. An input that resolves to an empty string registers nothing at all, and
neither does one left at the zero value its type starts from: an `int` you never supplied would
otherwise register `0` and blank out every unrelated digit in the output.
[JSON output](json-output.md#correlating-runs) lists the caveats in full, including why a task name
masked down to `***` cannot be pasted back into `--start-at-task`.

### Keep the secret out of `default:`

`--help` renders a sensitive input's default as `(default "***")` rather than the literal, but that
is the only cover a `default:` gets. A default is recipe text: it sits in the file in the clear,
survives `docket fmt`, and travels with the recipe wherever the recipe goes. Declare the input
`required: true` with no default and supply the value through a `--vars-file` or a CLI flag instead.
That is the shape [`docket export`](command-reference.md#docket-export) writes - a recipe of
`required: true, sensitive: true` inputs beside a `0600` vars-file holding the values.

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
- `bool`: native booleans, a native `1` or `0`, or a string spelled `true`/`yes`/`on`/`y`/`t`/`1`
  (or `false`/`no`/`off`/`n`/`f`/`0`) in any case. Any other number is an error rather than a
  truthiness test, so `debug: 2` is refused. A `--name=value` flag on the command line is parsed
  separately by pflag, which accepts the `true`/`false`/`1`/`0`/`t`/`f` subset of these but not
  `yes`/`on`.

A key in a vars file that does not match any declared input is a hard error, with a suggestion for
the closest real name:

```text
unknown input "appp" in --vars-file prod.yml; did you mean "app"?
```

### A vars file that holds secrets

A vars file supplying a value for an input declared `sensitive: true` is a secrets file, not a
settings file, and docket says so when it is readable by anyone but its owner:

```text
warning: --vars-file prod.yml holds sensitive input values and is readable by other users (mode 0644); chmod 600 prod.yml
```

It is a warning rather than an error - the file is yours and docket only reads it - and it goes to
stderr in every mode, so it stays out of the `--json` streams. A vars file of ordinary settings
(`app`, `replicas`) never triggers it; the `sensitive: true` declaration is what makes the mode
worth a word. `chmod 600` settles it. [`docket export`](command-reference.md#docket-export) writes
its own vars-file at `0600` for the same reason, so an exported pair never trips this; a file you
write yourself, or copy to another host, keeps whatever mode you gave it.

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
- [Command reference](command-reference.md#docket-export) - the vars-file `docket export` writes is a `--vars-file`
