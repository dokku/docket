# Command reference

docket has seven commands. Running `docket` with no subcommand prints the list:

| Command | What it does |
|---------|--------------|
| [`docket init`](#docket-init) | Scaffold a starter recipe. |
| [`docket validate`](#docket-validate) | Check a recipe offline, without contacting a server. |
| [`docket fmt`](#docket-fmt) | Canonically format a recipe. |
| [`docket plan`](#docket-plan) | Preview what `apply` would change. |
| [`docket apply`](#docket-apply) | Run the recipe, making changes as needed. |
| [`docket export`](#docket-export) | Read a live server and write a recipe describing it. |
| [`docket version`](#docket-version) | Print the binary's version. |

`apply`, `plan`, `validate`, and `fmt` all accept either YAML or JSON5 recipes. When `--tasks` is
omitted they probe `tasks.yml`, then `tasks.yaml`, then `tasks.json`, and use the first that exists.

## docket init

`docket init` writes a starter recipe from a built-in template. It is offline only: no server
contact and no `git` subprocess. The default scaffold ships four tasks (`dokku_app`,
`dokku_config`, `dokku_domains`, `dokku_git_sync`) in a single play with `app` and `repo` inputs,
and round-trips cleanly through `docket validate`.

The output format follows the `--output` extension: `.json` / `.json5` writes a JSON5 scaffold with
`// ...` comments, anything else writes YAML. Streaming to stdout (`--output -`) writes YAML.

```bash
# Use the current directory name as the app and remote.origin.url as the repo.
docket init

# Same scaffold in JSON5.
docket init --output tasks.json

# Stream the scaffold to stdout for piping.
docket init --output -
```

| Flag | Effect |
|------|--------|
| (default) | Write `./tasks.yml`; refuse if it already exists. |
| `--output <path>` | Write to a path; `-` writes to stdout. Format inferred from the extension. |
| `--force` | Overwrite an existing file. |
| `--name <name>` | Set the play and `app` input default (defaults to the directory name). |
| `--repo <url>` | Set the `repo` input default (defaults to `remote.origin.url` in `./.git/config`). |
| `--minimal` | A one-task example with no `inputs:` block. |

## docket validate

`docket validate` performs offline schema and template checks on a recipe without contacting any
Dokku server. It is built for CI lint jobs that should reject a broken recipe before it reaches a
deploy.

The checks cover: the file parses; the recipe shape is a list of plays; each task entry has the
right envelope keys plus exactly one task-type key; the task type is registered (with a "did you
mean" for typos); required fields decode; a task's conditional/semantic input rules hold (for
example a list that must be non-empty when `state: present`, or mutually-exclusive fields) - the same
checks `plan` and `apply` run, surfaced offline as `invalid_task_input`; sigil templates render
against the input defaults; expr predicates parse; and `.item` / `.index` are only used inside a
`loop:`.

```bash
docket validate --tasks path/to/tasks.yml
```

Exit code is `0` when no problems are found, `1` otherwise.

| Flag | Effect |
|------|--------|
| `--tasks <path>` | Use a specific recipe. |
| `--json` | Emit one JSON-lines problem per line with a stable schema, for a CI annotator. |
| `--strict` | Also flag any `required: true` input with no default and no supplied value, and verify that `--play` / `--start-at-task` references resolve to real names. |
| `--vars-file <path>` | Load input values from a YAML or JSON file (repeatable). Values here count as overrides for `--strict`. See [inputs](inputs.md#layered-values-with---vars-file). |
| `--play <name>` | (strict only) Verify the named play exists. |
| `--start-at-task <name>` | (strict only) Verify a task with this name exists; narrowed by `--play`. |

## docket fmt

`docket fmt` is a canonical formatter for recipes, in the spirit of `gofmt`. It reorders task and
play keys into a stable order, normalizes indentation to a 2-space step, and inserts blank lines
between top-level plays and task entries. It works on both YAML and JSON5, detected per file from
the extension, and both formats share the same canonical key order so a YAML recipe and its JSON5
twin lay out identically. Comments are preserved in both formats.

```bash
# Rewrite ./tasks.yml in place (probes tasks.yml -> tasks.yaml -> tasks.json with no argument).
docket fmt

# Format a JSON5 recipe in place.
docket fmt tasks.json

# CI gate: print the diff and exit 1 if anything is not canonical.
docket fmt --check --diff

# Read from stdin, write canonical to stdout (format sniffed from the first byte).
cat tasks.yml | docket fmt -
```

| Flag | Effect |
|------|--------|
| (default) | Format the file in place; a no-op when it is already canonical. |
| `--check` | Exit 1 if any file is not canonical; no writes. Composes with `--diff`. |
| `--diff` | Print a unified diff against canonical; no writes. Composes with `--check`. |
| `--color <when>` | Colorize the diff: `auto` (default), `always`, or `never`. |
| `-` | Read from stdin, write canonical to stdout. |
| `<path...>` | Format the named files; each argument is expanded as a glob. |

The diff is a standard unified diff (`--- / +++ / @@` headers) and applies with `git apply` or
`patch -p0` once colors are stripped. Before writing, `fmt` re-parses its own output and aborts if
the result does not match the input, so a formatting bug can never corrupt a recipe.

`fmt` operates on single-document recipes. An empty or comment-only file is left untouched, and a
YAML file containing more than one document (separated by `---`) is rejected rather than having its
trailing documents silently dropped.

## docket plan

`docket plan` reads each task's current state from the live server and reports what `apply` would
change, without running any mutating command. The output uses the same play header and column
layout as `apply`, with a marker set focused on drift:

| Marker | Meaning |
|--------|---------|
| `[ok]` | In sync; `apply` would change nothing. |
| `[+]` | `apply` would create new state. |
| `[~]` | `apply` would modify existing state. |
| `[-]` | `apply` would remove existing state. |
| `[!]` | The read-state probe itself errored, so drift is unknown. |

Tasks that perform several operations itemize them under the task line:

```text
==> Play: tasks
[~]       configure  (2 key(s) to set)
          - set KEY_ONE (new)
          - set KEY_TWO (was set)

Plan: 1 task(s); 1 would change, 0 in sync, 0 error(s).
```

The same probe drives `apply`: each task reads the server once, and `apply` reuses that read to
decide whether to mutate. A few tasks (notably `dokku_git_auth`, `dokku_registry_auth`, and
`dokku_storage_ensure`) cannot read their state without running the underlying command, so they
always report drift with a `(... not probed)` reason.

| Flag | Effect |
|------|--------|
| `--tasks <path>` | Use a specific recipe. |
| `--json` | Emit JSON-lines events instead of the human formatter. See [JSON output](json-output.md). |
| `--detailed-exitcode` | Exit `0` for no drift, `2` for drift, `1` on error. Errors win over drift. Mirrors `terraform plan -detailed-exitcode`. |
| `--vars-file <path>` | Load input values from a file (repeatable). See [inputs](inputs.md#layered-values-with---vars-file). |
| `--play <name>` | Plan only the named play. Composes with `--tags`. |
| `--tags <list>` | Plan only tasks whose tags intersect the list. See [task envelope](task-envelope.md#tags). |
| `--skip-tags <list>` | Skip tasks whose tags intersect the list. See [task envelope](task-envelope.md#tags). |
| `--list-tasks` | Print the resolved plan and exit without contacting the server. See [inspecting and resuming](#inspecting-and-resuming). |
| `--host <user@host:port>` | Plan against a remote server over SSH. Overrides `DOKKU_HOST`. See [remote execution](remote-execution.md). |
| `--sudo` | Wrap the remote `dokku` call in `sudo -n`. See [remote execution](remote-execution.md). |
| `--accept-new-host-keys` | Trust an unknown SSH host key on first connect. See [remote execution](remote-execution.md). |

```bash
# CI gate: fail the job if any task would change the server.
docket plan --detailed-exitcode || exit $?
```

## docket apply

`docket apply` runs every task in the recipe, mutating the live server as needed. Each task line
gets a status marker:

| Marker | Meaning |
|--------|---------|
| `[ok]` | Ran, no change. |
| `[changed]` | Ran, changed state. |
| `[skipped]` | Filtered out by tags, `when:`, or `--start-at-task`. |
| `[error]` | Errored. |

A play header precedes the task lines, and a summary closes the run:

```text
==> Play: tasks
[changed] dokku apps:create api
[ok]      dokku config:set api KEY=value

Summary: 2 tasks · 1 changed · 1 ok · 0 skipped · 0 errors  (took 0.8s)
```

On error, the message prints on a `!`-prefixed line and the run aborts with exit 1 (unless
[`--fail-fast`](recipes.md#error-handling-across-plays) is off and only the play aborts). The
summary still prints with partial counts.

| Flag | Effect |
|------|--------|
| `--tasks <path>` | Use a specific recipe. |
| `--verbose` | After each task, echo every resolved Dokku command it ran, one per `→` line. Masked against sensitive values. Ignored with `--json` (which already includes commands). |
| `--json` | Emit JSON-lines events instead of the human formatter. See [JSON output](json-output.md). |
| `--vars-file <path>` | Load input values from a file (repeatable). See [inputs](inputs.md#layered-values-with---vars-file). |
| `--play <name>` | Run only the named play. Composes with `--tags`. |
| `--tags <list>` | Run only tasks whose tags intersect the list. See [task envelope](task-envelope.md#tags). |
| `--skip-tags <list>` | Skip tasks whose tags intersect the list. See [task envelope](task-envelope.md#tags). |
| `--fail-fast` | Abort the whole run on the first error. Without it, an error aborts only the current play. |
| `--list-tasks` | Print the resolved plan and exit without running. See [inspecting and resuming](#inspecting-and-resuming). |
| `--start-at-task <name>` | Skip every task before the named one, then run from there. See [inspecting and resuming](#inspecting-and-resuming). |
| `--host <user@host:port>` | Apply against a remote server over SSH. Overrides `DOKKU_HOST`. See [remote execution](remote-execution.md). |
| `--sudo` | Wrap the remote `dokku` call in `sudo -n`. See [remote execution](remote-execution.md). |
| `--accept-new-host-keys` | Trust an unknown SSH host key on first connect. See [remote execution](remote-execution.md). |

A multi-command task renders one continuation line per invocation under `--verbose`:

```text
[changed] add buildpacks
          → dokku --quiet buildpacks:add app https://github.com/heroku/heroku-buildpack-nodejs.git
          → dokku --quiet buildpacks:add app https://github.com/heroku/heroku-buildpack-nginx.git
```

Color output respects [`NO_COLOR`](https://no-color.org/): set `NO_COLOR=1` to disable ANSI escapes.
Output is also plain automatically when piped to a non-TTY.

### Inspecting and resuming

Two flags help when a recipe grows long. `--list-tasks` previews the resolved plan without running,
and `--start-at-task` resumes a partially-applied recipe from a specific task. Both work on `apply`;
`--list-tasks` also works on `plan`.

`--list-tasks` walks the resolved plan - after `--play` / `--tags` filtering, after `loop:`
expansion, after `when:` evaluation against inputs - and prints one line per task:

```text
$ docket apply --list-tasks
==> Play: api
[0] dokku apps:create api  [tags=core]
[1] dokku git:sync api  [tags=deploy]
[2] dokku config:set api  [tags=core,deploy]
[3] dokku ports:add api  [tags=deploy]
```

A `when:` that is false against the inputs renders as `[skipped]`. A `when:` that references
`.registered.<name>` cannot be decided without running earlier tasks, so it renders as `[unknown]`
rather than guessing.

`--start-at-task <name>` takes an exact task `name:`. Earlier tasks render as `[skipped]` with a
`(before --start-at-task)` reason and do not run; the matched task and everything after it run:

```text
$ docket apply --start-at-task "dokku config:set api"
==> Play: api
[skipped] dokku apps:create api    (before --start-at-task)
[skipped] dokku git:sync api       (before --start-at-task)
[ok]      dokku config:set api
[changed] dokku ports:add api

Summary: 4 tasks · 1 changed · 1 ok · 2 skipped · 0 errors  (took 1.1s)
```

Filters apply in this order: `--start-at-task` selects first, then `--tags` / `--skip-tags`, then
per-task `when:` at execution time. The name search walks every play in source order, narrowed by
`--play`. An unmatched name exits 1 with the available names listed.

## docket export

`docket export` reads a live Dokku server and writes a recipe describing it - the inverse of
`apply`. It enumerates the apps on the server and reconstructs each one's declarative state, so you
can capture an existing server as a recipe instead of hand-writing one. This is the starting point
for a [migration](migration.md).

Because a faithful recipe would otherwise embed secrets, export writes **two files**: the recipe,
and a companion **vars-file** holding the sensitive values (every `config` value, plus any field a
task marks sensitive). The recipe references those values through per-play `inputs:` and
`{{ .name }}` templates, so the pair is applied together:

```bash
# Export the local server to tasks.yml + tasks.vars.yml.
docket export

# Export a remote server over SSH.
docket export --host deploy@dokku.example.com

# Apply the exported pair somewhere else.
docket apply --tasks tasks.yml --vars-file tasks.vars.yml
```

The correctness contract is idempotency: applying an exported pair back to the same server reports
no drift (`plan` shows every task `[ok]`).

| Flag | Effect |
|------|--------|
| `--output <path>` | Where to write the recipe (default `tasks.yml`). Pass `-` to stream a single self-contained recipe (values inlined, no vars-file) to stdout for inspection. |
| `--vars-output <path>` | Where to write the companion vars-file (default `<output-base>.vars.<ext>`, e.g. `tasks.vars.yml`). |
| `--overwrite` | Overwrite existing output files without prompting. Without it, export prompts before replacing either file, and aborts writing nothing if declined (or if stdin is not interactive). |
| `--redact` | Write placeholder values into the vars-file instead of real secrets, producing a shareable recipe plus a fill-in-the-blanks vars template. The `required` inputs mean `apply` fails loudly until the vars-file is filled in. |
| `--app <name>` | Restrict the export to the named app. Repeatable. |
| `--host <user@host:port>` | Read a remote server over SSH. Overrides `DOKKU_HOST`. See [remote execution](remote-execution.md). |
| `--sudo` | Wrap the remote `dokku` call in `sudo -n`. |
| `--accept-new-host-keys` | Trust an unknown SSH host key on first connect. |

The output format follows the `--output` extension (`.json` / `.json5` writes JSON5, anything else
YAML), and the vars-file matches. Which task types export is a per-task property: each task's
reference page carries an **Export support** section stating whether it is supported, partial (for
example a value that is lifted into the vars-file), or not exportable (write-only credentials such
as `dokku_git_auth`, or `dokku_service_property`, which no datastore plugin can read back).
Resources that cannot be read back are reported as warnings and left out of the recipe.

## docket version

`docket version` prints the binary's version and exits.

```bash
docket version
```

## See also

- [Recipes](recipes.md) - the recipe file, plays, and `--play` / `--fail-fast`
- [Task envelope](task-envelope.md) - `tags`, `when`, `loop`, and the rest
- [JSON output](json-output.md) - the `--json` event schema
- [Remote execution](remote-execution.md) - running against a remote server over SSH
