# HCL recipes

docket reads and writes recipes in HCL, the block-and-attribute syntax Terraform and Packer are
configured in. It is a third spelling of the same recipe, not a different feature: templates,
conditionals, every envelope key and every task type work exactly as they do in YAML and JSON5, and
`docket fmt --format hcl` converts an existing recipe into it.

This page is the mapping. [Recipes](recipes.md) covers the file formats in general, and everything
else in the documentation writes its examples in YAML.

## The shape of an HCL recipe

```hcl
# The whole file is the recipe: one `play` block per play.
play "api" {
  tags = ["web"]
  when = "env != \"preview\""

  input "app" {
    default     = "api"
    description = "Application name"
  }

  dokku_app "create the app" {
    app   = "{{ .app | dq }}"
    state = "present"
  }

  dokku_config "configure env vars" {
    app = "{{ .app | dq }}"
    config = {
      APP_ENV   = "production"
      LOG_LEVEL = "info"
    }
  }
}
```

The same recipe in YAML:

```yaml
---
- name: api
  tags: [web]
  when: 'env != "preview"'
  inputs:
    - name: app
      default: api
      description: Application name
  tasks:
    - name: create the app
      dokku_app:
        app: "{{ .app | dq }}"
        state: present
    - name: configure env vars
      dokku_config:
        app: "{{ .app | dq }}"
        config:
          APP_ENV: production
          LOG_LEVEL: info
```

## Blocks and labels

Four block types carry the recipe's structure. Everything else in a body is an attribute.

| Block | Where | What it is |
|-------|-------|------------|
| `play` | the file | one play; repeat it for a multi-play recipe |
| `input` | inside a `play` | one entry of the play's `inputs:` |
| `<task_type>` | inside a `play` | one task, named by its type |
| `task` | inside a `play` | one task in the general form, for the entries the shorthand cannot spell |
| `block` / `rescue` / `always` | inside a `task` | the clauses of a [try/catch/finally group](task-envelope.md) |

**A block's label is the entry's `name`.** `play "api"` is a play named `api`, and
`dokku_app "create the app"` is a task whose envelope name is `create the app`. The label is
optional everywhere, because a YAML recipe does not have to name a play or a task either:

```hcl
play {
  dokku_app {
    app = "api"
  }
}
```

**Names do not have to be unique.** A recipe is a list, not a map, so two tasks may share a name in
HCL exactly as they may in YAML. `--play` selecting an ambiguous play name behaves the same way in
both.

### Why the label carries the name

Ten registered task types declare a field called `name` - `dokku_service_create`, `dokku_network`,
`dokku_storage_entry` and the rest. A task block's body holds the task's fields and the envelope's
keys together, so a body `name` would be two things at once. Moving the envelope's name out to the
label settles it: **a `name` attribute inside a task block is always the task's own field.**

```hcl
dokku_service_create "make the database" {
  name    = "mydb" # the task's field
  service = "postgres"
}
```

No other envelope key collides with a field name, and a test over the task catalog keeps it that
way, so `when`, `tags`, `loop`, `register`, `changed_when`, `failed_when` and `ignore_errors` are
always the envelope's:

```hcl
dokku_app "create the app" {
  tags          = ["deploy"]
  when          = "env != \"preview\""
  ignore_errors = true
  app           = "{{ .app | dq }}"
  state         = "present"
}
```

## The general `task` form

Some task entries have no single task type to name the block after. They are written as a `task`
block, where every attribute belongs to the entry itself and the task type is a nested block:

```hcl
task "deploy" {
  block {
    dokku_app "create the app" {
      app = "api"
    }

    dokku_git_sync {
      app    = "api"
      remote = "{{ .repo | dq }}"
    }
  }

  rescue {
    dokku_maintenance {
      app   = "api"
      state = "present"
    }
  }

  always {
    dokku_maintenance {
      app   = "api"
      state = "absent"
    }
  }
}
```

`docket fmt` writes the shorthand whenever it can and the general form when it cannot, so which one
you get is decided for you. It reaches for the general form when the entry:

- is a group entry, with `block:` / `rescue:` / `always:` rather than a task type;
- has no task type, or more than one;
- has a task type whose value is not a mapping - `dokku_app:` with an empty body round-trips as
  `dokku_app = null`, which the shorthand would silently widen to `{}`;
- has a name that is not a plain string;
- has a task field named for an envelope key, which no registered task has today;
- or names a task type HCL cannot spell as a block type.

Both forms read identically, so a hand-written recipe may use either.

## Values

Attribute values are HCL literals. docket recipes are **data**: HCL's variables, functions,
operators and `${…}` templates are refused rather than evaluated, and the diagnostic points at
docket's own substitution syntax instead.

| Value | HCL |
|-------|-----|
| string | `"api"` |
| multi-line string | a `<<EOT` heredoc |
| number | `8080`, `-5`, `1.5` |
| boolean | `true`, `false` |
| absent | `null` |
| list | `["a", "b"]` |
| mapping | `{ A = "1" }`, written one key per line |

A nested mapping is always an object expression rather than a nested block, so there is exactly one
spelling for every depth below the recipe's own structure:

```hcl
dokku_config "configure env vars" {
  app = "api"
  config = {
    APP_ENV   = "production"
    LOG_LEVEL = "info"
  }
}
```

### Multi-line strings

A string containing a newline is written as a heredoc, which keeps a PEM body or an `app.json`
readable. The body sits at column zero, because `<<EOT` takes its content literally and HCL's
indented `<<-` form would strip whatever leading whitespace the value had of its own:

```hcl
dokku_certs "install the certificate" {
  app          = "api"
  cert_content = <<EOT
-----BEGIN CERTIFICATE-----
MIIB...
-----END CERTIFICATE-----
EOT
  key_content  = <<EOT
-----BEGIN PRIVATE KEY-----
MIIE...
-----END PRIVATE KEY-----
EOT
}
```

A heredoc is used only when the value carries no `{{ … }}` substitution - see below - and when no
line of it is the delimiter itself. Anything else is written as a quoted string with `\n` escapes.

## Templating

Templating works exactly as it does in the other two formats. docket renders the whole file as text
and only then parses it, so `{{ .app }}` is substituted before HCL ever sees the file, and a
label is substituted the same way an attribute is:

```hcl
dokku_app "create {{ .app | dq }}" {
  app = "{{ .app | dq }}"
}
```

HCL adds one hazard the others do not have. Inside a quoted string, HCL reads `${` as the start of
an interpolation and `%{` as the start of a control sequence, so an input value carrying either
would be read as syntax rather than as text. **`dq` handles it**: for an HCL recipe it doubles both
sigils, which is the escape HCL itself defines, on top of the JSON escaping it does everywhere.
Write `"{{ .app | dq }}"` and a value of `${literal}` lands as `${literal}`.

Writing `${` or `%{` literally in a recipe needs the same doubling, and `docket fmt` writes it for
you:

```hcl
dokku_config {
  app = "api"
  config = {
    PROMPT = "$${USER}" # the value is ${USER}
  }
}
```

A substitution inside a heredoc is refused. A heredoc processes no backslash escapes at all, so
neither `{{ .app }}` nor `{{ .app | dq }}` means there what it means in a quoted string:

```text
 !     tasks.hcl: line 3: `{{ .app }}` is in a heredoc, and rewriting it double-quoted would leave
       a recipe that no longer tolerates a double quote in the value; write it as "{{ .app | dq }}"
```

An interpolation that substitutes no value at all, such as `{{ if .debug }}`, is fine in a heredoc,
because only literal recipe text is ever inserted.

## Comments

HCL accepts `#`, `//` and `/* … */`, and docket reads all three. Canonical output is always `#`,
for two reasons: a line comment cannot be terminated early by its own text, and a recipe piped to
stdin is recognised as HCL by its first token, which a `//` would hide behind JSON5's own comment
syntax.

Comments survive a conversion in either direction, so a `# note` in HCL is a `# note` in YAML and a
`// note` in JSON5.

## Files and formats

| | How HCL spells it |
|-|-|
| Extension | `.hcl` |
| `--tasks-format` / `--format` | `hcl` |
| Default filename | `tasks.hcl`, probed after `tasks.yml`, `tasks.yaml` and `tasks.json` |
| Vars-file | a flat body of attributes, `app = "api"` |

A recipe piped to stdin is sniffed: after any comments, a body opening with an identifier followed
by `=`, `{`, or a quoted label is read as HCL. A recipe `docket fmt` wrote always is. A hand-written
one led by `//` or `/* … */` is claimed by JSON5 first and needs `--tasks-format hcl`.

An HCL vars-file is a flat mapping of input name to value, with the same comment syntax:

```hcl
# production values
app   = "inflector"
port  = 8080
debug = true
```

Unlike the JSON5 vars-file, which is plain JSON and so also valid YAML, an HCL one has to be named
`.hcl`. `docket export --format hcl --vars-output vars.yml` says so rather than letting the next
`--vars-file` find out.

## Converting an existing recipe

```bash
# To a new file, leaving the original in place.
docket fmt --format hcl --output tasks.hcl tasks.yml

# To stdout, touching nothing on disk.
cat tasks.yml | docket fmt --format hcl -

# And back again.
docket fmt --format yaml --output tasks.yml tasks.hcl
```

The conversion carries comments and every value, and normalises the same six things a YAML/JSON5
conversion does - see
[Converting between formats](command-reference.md#converting-between-formats). HCL adds one of its
own: **`${` and `%{` are doubled**, since they are syntax in HCL and text everywhere else.

## What HCL cannot carry

- **An empty recipe.** A list of no plays is an empty HCL file, which reads back as no recipe at
  all, so the conversion is refused rather than written.
- **A key that is not an HCL identifier.** HCL identifiers already allow `-` and non-ASCII, so this
  reaches only a play or envelope key with a space, a dot, or a leading digit. A key that awkward
  inside a nested mapping is fine, because an object expression may quote its keys.
- **`Infinity` and `NaN`.** JSON5 spells them; HCL has no literal for either.
- **A single-quoted, plain, or block-scalar interpolation.** Canonical HCL writes every string
  holding one as a quoted scalar, which is the position canonical JSON5 is in - see
  [Inputs](inputs.md).

## See also

- [Recipes](recipes.md) - the recipe file format, plays, and multi-app recipes
- [Inputs](inputs.md) - parameterize a recipe with variables and `--vars-file`
- [Task envelope](task-envelope.md) - per-task tags, conditionals, loops, and error handling
- [Command reference](command-reference.md#docket-fmt) - `fmt`, and converting between formats
