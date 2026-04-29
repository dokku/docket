# docket

> Note: this is a heavy work in progress. YMMV.

A method to pre-package and ship applications on Dokku.

## Background

While Ansible is all well and good, having something native to Dokku for shipping applications is awesome. The `docket` package allows users to specify exactly what it means to be an app, while allowing for some minimal customization.

This package provides the above functionality by exposing the modules from `ansible-dokku` within a single Golang binary. Users of `ansible-dokku` based task lists should be able to use their existing tasks with minimal changes, while organizations can decide to expose apps in easy to use methods for their users.

## Building

```shell
go build
```

## Usage

Create a `tasks.yml` file:

```yaml
---
- tasks:
    - dokku_app:
        app: inflector
    - dokku_sync:
        app: inflector
        repository: http://github.com/cakephp/inflector.cakephp.org
```

JSON5 is also supported: write the same recipe as `tasks.json` and pass `--tasks tasks.json`, or just drop the file in the working directory and docket will pick it up automatically when `tasks.yml` / `tasks.yaml` are absent. See "Task file formats" below for the dispatch rules.

Run it:

```shell
# from the same directory as the tasks.yml (or tasks.json)
docket apply
```

Running `docket` with no subcommand prints the available commands. Use `docket init` to scaffold a starter task file, `docket apply` to execute a task file, `docket fmt` to canonically format a task file, `docket plan` to preview the changes a task file would make without mutating any state, `docket validate` to check a task file's schema and templates without contacting the server, or `docket version` to print the binary's version. All five commands accept either YAML or JSON5 surface syntax.

### Task file formats

Docket reads task files in either YAML or JSON5. Format is selected by file extension:

| Extension | Parser |
|-----------|--------|
| `.yml`, `.yaml` | `gopkg.in/yaml.v3` |
| `.json`, `.json5` | titanous JSON5 (a strict superset of JSON) |

JSON5 adds three things over plain JSON that are useful in a recipe: `// line` and `/* block */` comments, trailing commas in arrays and objects, and unquoted keys when they are valid identifiers. Existing JSON files parse unchanged.

When `--tasks` is omitted, docket probes the working directory in this order: `tasks.yml`, `tasks.yaml`, `tasks.json`. The first one that exists wins. If none are present the run errors with the candidate list so the typo is obvious. With `--tasks <path>`, format is detected from the path's extension; unknown extensions default to YAML so a path like `recipe.txt` keeps its pre-#218 behaviour.

The same JSON5 recipe in YAML and in JSON5 produces an identical play / task structure - sigil `{{ .var }}` templates, expr predicates, every envelope key, and every task type behave the same way. `docket fmt` round-trips comments in both formats.

### Multi-play recipes

A docket recipe is a list of plays. Each play has its own `name`, optional `tags`, optional `when:`, optional `inputs:`, and a `tasks:` list. The executor walks every play in source order, so a single recipe can describe multiple coordinated apps or services in one file. The examples below use YAML; JSON5 recipes have the same shape (a top-level array of objects) - see "Task file formats" above.

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

Single-play recipes - the legacy shape - keep working unchanged because they are already a one-element list.

| Key | Status | What it does |
|-----|--------|--------------|
| `name` | active | Human label for the play. Auto-generated as `play #N` when omitted, except for single-play files which use the legacy `tasks` header. |
| `tags` | active | Tag list inherited by every task in the play (additive with per-task `tags`). |
| `when` | active | expr expression. Falsy renders the play as `(skipped: when "...")` and the play's tasks are not executed. |
| `inputs` | active | Per-play input defaults. Override file-level defaults within their play; CLI `--name=value` and `--vars-file` always win. |
| `tasks` | active | The play's task list (existing per-task envelope). |

#### Per-play `inputs:` precedence

Per-play inputs slot into layer 2 of the precedence chain from `--vars-file`:

| Layer | Source |
|-------|--------|
| 1 | File-level `inputs:` defaults (declared on a play with no tasks). |
| 2 | Per-play `inputs:` defaults (declared on a play that also has tasks). |
| 3 | `--vars-file <path>` (repeatable; later files override earlier). |
| 4 | `--name=value` CLI flags (always win). |

A play-local input default is visible to that play's tasks; it is *not* visible to any play's `when:` predicate (including its own) and *not* visible to other plays' tasks. A file-level input - one declared on an inputs-only play - is visible to every play.

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

#### `--play <name>` filter

Run only one play from the recipe by name:

```shell
docket apply --tasks tasks.yml --play api
docket plan  --tasks tasks.yml --play api --tags deploy
```

`--play` composes with `--tags` / `--skip-tags`: the play filter narrows to one play, then the tag filter applies to the tasks inside it. An unknown `--play` name produces a clear error listing the available plays.

#### Per-play `when:` scoping

A play-level `when:` is evaluated against the file-level merged context only - file-level input defaults, plus `--vars-file` and CLI overrides. The play's own `inputs:` are intentionally not visible to its own `when:` (the spec calls this circular). Sibling plays' play-local inputs are also not visible. Per-task `when:` inside the play does see the play's own inputs.

#### Error semantics: abort the play, not the run

By default, an error in a task aborts the *current play* and the next play still runs. Use `--fail-fast` to opt back into the legacy "abort the entire run on first error" behaviour:

```shell
docket apply --tasks tasks.yml             # default: bail current play, continue next
docket apply --tasks tasks.yml --fail-fast # legacy: abort the entire run
```

The summary line gains a `· N play skipped` segment when one or more plays were skipped (by `when:` or by a per-task `when:` predicate at the play level):

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

### Task envelope

Each task entry in `tasks.yml` admits a small set of cross-cutting envelope keys alongside the single `dokku_*` task-type key. Body templating uses [sigil](https://github.com/gliderlabs/sigil) (`{{ .input }}` substitutions) and envelope predicates use [expr-lang/expr](https://github.com/expr-lang/expr) so the two languages live in clearly separate positions.

| Key | Status | What it does |
|-----|--------|--------------|
| `name` | active | Human label for the task. Auto-generated when omitted. |
| `tags` | active | Tag list filtered by `--tags` / `--skip-tags` on `apply` and `plan`. |
| `when` | active | expr expression. Falsy renders the task as `[skipped]`. |
| `loop` | active | List literal, or expr returning a list. Expands one entry into N with `.item` / `.index` available in the body. |
| `register` | active | Bind the post-override task result for downstream tasks. |
| `changed_when` | active | expr override for the task's "changed" verdict. |
| `failed_when` | active | expr override for the task's "failed" verdict. |
| `ignore_errors` | active | Continue on task failure (apply only). |
| `block` | active | Try clause: list of child task entries that run in order; the first error triggers `rescue`. |
| `rescue` | active | Catch clause: list of child task entries that run on the first uncaught error in `block`. |
| `always` | active | Finally clause: list of child task entries that run unconditionally after `block` / `rescue`. |

Unknown envelope keys are rejected at parse time with a "did you mean" suggestion against the closest valid key.

#### Tags and `--tags` / `--skip-tags`

Tags are a small free-form set on each task:

```yaml
- tasks:
    - name: deploy api
      tags: [api, deploy]
      dokku_app:
        app: api
    - name: deploy worker
      tags: [worker, deploy]
      dokku_app:
        app: worker
```

`--tags foo,bar` keeps tasks whose tag set intersects `{foo, bar}`. Untagged tasks are excluded. `--skip-tags foo,bar` drops tasks whose tag set intersects `{foo, bar}`; untagged tasks are kept. Specifying both intersects "kept by `--tags`" with "not filtered by `--skip-tags`":

```shell
docket plan  --tasks tasks.yml --tags api          # only the api task
docket apply --tasks tasks.yml --skip-tags worker  # everything except worker
```

#### `when:` per-task conditional

`when:` is an expr expression evaluated per-task at execution time. Falsy results render as `[skipped]` in the apply / plan output and contribute to the new "skipped" summary count:

```yaml
- inputs:
    - name: env
      default: staging
  tasks:
    - name: enable letsencrypt
      when: 'env == "prod"'
      dokku_letsencrypt:
        app: api
        state: enabled
```

The expression context today carries the file-level inputs plus, for loop expansions, `.item` and `.index`. Inside `changed_when:` / `failed_when:` the same context also includes `.result` (the just-finished task's `TaskOutputState`). Once any task has registered, every subsequent envelope predicate (including `when:`) sees `.registered.<name>`. Other context keys (`.timestamp`, `.host`, `.play.name`) are reserved for follow-on issues.

#### `loop:` per-task iteration

`loop:` expands one envelope into N before execution. The value is either a list literal:

```yaml
- tasks:
    - name: deploy
      loop: [api, worker, web]
      dokku_app:
        app: "{{ .item }}"
```

or an expr expression that returns a list:

```yaml
- tasks:
    - name: deploy
      loop: 'apps where length(name) > 0'
      dokku_app:
        app: "{{ .item.name }}"
```

Each iteration renders the body with `.item` (the iterator value) and `.index` (zero-based) injected. Expanded envelope names are suffixed with `(item=<value>)` to keep them unique; complex items fall back to `(item=#<index>)`. `.item` / `.index` references outside a `loop:` body are rejected at parse time so a stray reference does not silently render to an empty value.

`when:` interacts with `loop:`: the predicate is evaluated per expansion, so `loop: [a, b, c]` plus `when: 'item != "b"'` runs only the `a` and `c` expansions.

#### `register:` cross-task data flow

`register: <name>` snapshots the just-finished task's post-override result into a run-wide map keyed by `<name>`. Every subsequent envelope predicate sees the snapshot at `.registered.<name>`:

```yaml
- tasks:
    - name: ensure app
      register: app_result
      dokku_app:
        app: api
    - name: stamp first deploy
      when: 'registered.app_result.Changed'
      dokku_config:
        app: api
        config:
          FIRST_RUN_FLAG: "true"
```

`registered.<name>` exposes the same fields a task's `TaskOutputState` carries: `.Changed`, `.Error`, `.State`, `.DesiredState`, `.Stdout`, `.Stderr`, `.ExitCode`, `.Commands`, `.Message`. Comparisons like `registered.foo.Error != nil` and `registered.foo.Stderr contains "..."` work directly. Reused names are rejected at parse time with `register_duplicate`; the registered map is shared across plays in one `docket apply` / `docket plan` run.

When `register:` is used with `loop:`, the registered value carries an additional `.Results` list of per-iteration `TaskOutputState`s in source order. The embedded fields aggregate Ansible-style: `.Changed` is true if any iteration changed, `.Error` is the first non-nil iteration error, and the rest mirror the last iteration's values:

```yaml
- tasks:
    - name: each
      loop: [api, worker, web]
      register: deploys
      dokku_app:
        app: "{{ .item }}"
    - name: any-changed
      when: 'registered.deploys.Changed'
      dokku_config:
        app: api
        config:
          LAST_DEPLOY_TS: "now"
    - name: first-iteration-only
      when: 'registered.deploys.Results[0].Changed'
      dokku_config:
        app: api
        config:
          API_FIRST_DEPLOY: "true"
```

#### `changed_when:` and `failed_when:` per-task overrides

`changed_when:` and `failed_when:` are expr predicates that override the task's self-reported verdict. Both evaluate against `.result` (the just-finished `TaskOutputState`) plus the regular context (`.registered`, file-level inputs, loop vars). Phase ordering is `failed_when` → `changed_when` → `register` snapshot, so `register` sees the post-override values.

`failed_when` matches Ansible: a truthy result marks the task as failed (installing a synthetic error if none was reported), and a falsy result fully clears the failure verdict (Error and the state-mismatch path). It is the standard "this exit code is fine" idiom for idempotent operations:

```yaml
- name: try removing legacy mount
  register: unmount
  failed_when: 'result.Error != nil and not (result.Stderr contains "not mounted")'
  dokku_storage_mount:
    app: api
    state: absent
    mount: /old/path:/var/data

- name: log only if real failure
  when: 'registered.unmount.Error != nil'
  dokku_config:
    app: api
    config:
      LAST_UNMOUNT_ATTEMPT_FAILED: "true"
```

`changed_when` rewrites the `Changed` flag based on truthiness. `changed_when: 'false'` silences a self-reported-changed task; `changed_when: 'true'` makes an in-sync task render as changed.

#### `ignore_errors:` continue past failures

`ignore_errors: true` suppresses the fatal-exit decision when a task errors. The task still appears as `[error]` in the human output (with an `(ignored)` marker) and `status: "error"` in JSON (with `"ignored": true`), but the run does not abort and the error does not count toward the summary. `ignore_errors` is consulted after `failed_when`, so a `failed_when`-cleared task is not re-flagged:

```yaml
- tasks:
    - name: try the optional path
      ignore_errors: true
      dokku_storage_mount:
        app: api
        state: absent
        mount: /old/path:/var/data
    - name: continues regardless
      dokku_config:
        app: api
        config:
          LAST_RUN_TS: "now"
```

`ignore_errors` is meaningful only for `apply`. `plan` never aborts a run, so the flag is a no-op there.

#### `block:` / `rescue:` / `always:` structured error handling

A task entry that carries `block:` becomes a *group entry*: try/catch/finally over a list of nested task entries. The wrapping envelope (`name`, `tags`, `when`, `loop`, `register`, `changed_when`, `failed_when`, `ignore_errors`) applies to the whole group; children execute in order under the group's umbrella.

```yaml
- tasks:
    - name: deploy with rollback
      block:
        - dokku_app_clone:    { source_app: api, app: api-candidate }
        - dokku_git_sync:     { app: api-candidate, repository: "{{ .repo }}" }
        - dokku_checks_toggle: { app: api-candidate, state: enabled }
      rescue:
        - dokku_app: { app: api-candidate, state: absent }
      always:
        - dokku_config:
            app: api
            config:
              LAST_DEPLOY_ATTEMPT: "now"
```

Execution rules:

- `block:` children run in source order. The first child whose post-override verdict is failed and whose own `ignore_errors` is false stops the block.
- When `rescue:` is non-empty, a stopped block triggers every `rescue:` child in order. Rescue children run with the failing block child's `TaskOutputState` bound under `.failed_task`, so a rescue can branch on the actual cause: `when: 'failed_task.Stderr contains "..."'` is the standard idiom.
- `always:` children run unconditionally after `block:` / `rescue:`, even if rescue itself errored.
- The group's own envelope predicates (`failed_when`, `changed_when`, `register`, `ignore_errors`) apply to the synthesized group outcome - `register: <name>` snapshots the post-rescue, post-always result; `ignore_errors: true` on the group swallows any residual error after rescue + always.
- `ignore_errors: true` on a *child* of `block:` swallows that child's error and execution continues; it does NOT trigger `rescue:`. Rescue is the "handle" path; `ignore_errors` is the "swallow" path.
- `loop:` on a group runs the entire group once per item, with `.item` / `.index` shared across every nested child.
- Groups nest: a group entry can appear inside another group's `block:` / `rescue:` / `always:` lists.

`plan` reports drift for `block:` children unconditionally. `rescue:` and `always:` children plan only when at least one block child reports drift or a probe error, since `Plan()` cannot fail the way `Execute()` can. Group children render with a `[block]` / `[rescue]` / `[always]` prefix on their event line; the group's own summary line carries a `(group)` annotation.

### Scaffolding with `init`

`docket init` writes a starter task file from an embedded template. It is offline only: no Dokku server contact, no `git` subprocess. The default scaffold ships four tasks (`dokku_app`, `dokku_config`, `dokku_domains`, `dokku_git_sync`) wrapped in a single play with `app` and `repo` inputs, and round-trips cleanly through `docket validate`.

The output format is inferred from the `--output` extension: `tasks.json` / `tasks.json5` writes a JSON5 scaffold (with `// ...` comments demonstrating the comment syntax), anything else writes the YAML scaffold. Stdout (`--output -`) defaults to YAML.

```shell
# Use cwd basename as the app and remote.origin.url from ./.git/config as the repo
docket init

# Same scaffold, JSON5 surface syntax
docket init --output tasks.json

# Stream the rendered scaffold to stdout for piping
docket init --output -
```

The flags are:

| Flag | Effect |
|------|--------|
| (default) | Write `./tasks.yml`; refuse if the file exists |
| `--output <path>` | Write to a specific path; `-` writes to stdout. Format is inferred from the extension (`.json` / `.json5` -> JSON5, otherwise YAML). |
| `--force` | Overwrite an existing file |
| `--name <name>` | Override the play and `app` input default (defaults to the cwd basename) |
| `--repo <url>` | Override the `repo` input default (defaults to `remote.origin.url` in `./.git/config`, if present) |
| `--minimal` | One-task example with no `inputs:` block |

### Formatting recipes with `fmt`

`docket fmt` is a canonical formatter for task files, in the spirit of `gofmt`. It works for both YAML and JSON5: format is detected per file from the path's extension (`.yml` / `.yaml` use the `gopkg.in/yaml.v3` Node API, `.json` / `.json5` use docket's in-tree JSON5 formatter). Both formatters share the same canonical key order so a YAML recipe and its JSON5 twin lay out identically.

For YAML, head / line / foot comments survive via yaml.v3's Node API. For JSON5, comments survive via a comment-aware in-tree AST + emitter (line `// ...` comments above a member, beside a member on the same line, or as foot comments inside a container before its closing brace; block `/* ... */` comments are preserved at the same anchors). Both surfaces reorder task envelope and play keys into a stable order, normalise indentation to a 2-space step, and insert blank lines between top-level plays and between top-level task entries. The default rewrites the named file in place; `--check` and `--diff` are read-only modes. The CLI flags compose, modeled after `black` / `ruff format`.

```shell
# Rewrite ./tasks.yml in place. With no positional argument, fmt
# probes tasks.yml -> tasks.yaml -> tasks.json (same default-lookup
# rule as apply / plan / validate).
docket fmt

# Format a JSON5 recipe in place
docket fmt tasks.json

# CI gate: print the diff and exit 1 if anything is not canonical.
docket fmt --check --diff

# Read from stdin, write canonical to stdout. Stdin format is sniffed
# from the first non-trivia byte: a leading [ or { signals JSON5,
# anything else parses as YAML.
cat tasks.yml | docket fmt -
```

The flags are:

| Flag | Effect |
|------|--------|
| (default) | Format `./tasks.yml` in place; no-op (mtime preserved) when already canonical |
| `--check` | Exit 1 if any file is not canonical; no writes. Composes with `--diff` |
| `--diff` | Print a GNU unified diff against canonical; no writes. Composes with `--check` |
| `--color <when>` | When to colorize the diff: `auto` (default; on if stdout is a TTY and `NO_COLOR` is unset), `always`, `never` |
| `-` | Read from stdin, write canonical to stdout |
| `<path...>` | Format the named files; each argument is expanded as a glob and rewritten in place |

The diff output is GNU unified diff with `--- <path>` / `+++ <path>` / `@@` headers and is consumable by `git apply` and `patch -p0` once colors are stripped.

Before writing, `fmt` re-parses its canonical output and aborts if the round-tripped AST does not match the input AST - a guard against `yaml.v3` emitter edge cases (notably anchors and complex flow scalars). On a parse error or round-trip mismatch the file is not touched and `fmt` exits 1.

### Applying recipes with `apply`

`docket apply` runs every task in the recipe, mutating the live dokku server as needed. Each task line is prefixed with a status marker padded to a fixed column:

| Marker | Meaning |
|--------|---------|
| `[ok]` | Task ran, no change |
| `[changed]` | Task ran, mutated state |
| `[skipped]` | Task was filtered out (tags, `when:`, `--start-at-task`) |
| `[error]` | Task errored; the run aborts |

A play header (`==> Play: tasks`) precedes the per-task lines, and an end-of-run summary line follows them:

```text
==> Play: tasks
[changed] dokku apps:create api
[ok]      dokku config:set api KEY=value

Summary: 2 tasks · 1 changed · 1 ok · 0 skipped · 0 errors  (took 0.8s)
```

On error, the failing task's error message is printed as a `!`-prefixed continuation line and the run aborts with exit 1. The summary still prints with the partial counts before exit.

The flags are:

| Flag | Effect |
|------|--------|
| `--tasks <path>` | Use a specific task file (YAML or JSON5). When omitted, docket probes `tasks.yml` -> `tasks.yaml` -> `tasks.json`. |
| `--verbose` | After each task line, echo every resolved Dokku command the task ran on a `→`-prefixed continuation line, in invocation order. Tasks that loop over inputs (e.g. `dokku_buildpacks` adding several URLs) emit one continuation per call. Commands are masked against the global sensitive value set. Ignored when `--json` is set; the JSON output already includes the resolved commands. |
| `--json` | Suppress the human formatter and emit one JSON-lines event per `play_start`, `task`, or `summary` to stdout. Sensitive values mask to `***`. See "JSON output" below for the schema. |
| `--vars-file <path>` | Load input values from a YAML or JSON file. Repeatable; later files override earlier files for the same key. CLI `--name=value` flags always win. See "Layered input variables with `--vars-file`" below. |
| `--play <name>` | Run only the play with this name. Matches the play's `name:` field; auto-named plays use `play #N`. Composes with `--tags`. |
| `--fail-fast` | Abort the entire run on the first task error. Without this flag, an error aborts only the current play and the next play still runs. |
| `--list-tasks` | Print the resolved task plan and exit without running. Honors `--play` / `--tags` / `--skip-tags` and shows expanded loop iterations and `[skipped]` markers for `when:`-skipped tasks. See "Inspecting and resuming" below. |
| `--start-at-task <name>` | Skip every task before the matched name (rendered as `[skipped] ... (before --start-at-task)`); the matched task and successors run normally. Filter order: `--start-at-task` -> `--tags`/`--skip-tags` -> per-task `when:` at execution. The name search walks every play in source order, narrowed by `--play`. |

For example, a multi-command task renders one continuation per invocation:

```text
[changed] add buildpacks
          → dokku --quiet buildpacks:add app https://github.com/heroku/heroku-buildpack-nodejs.git
          → dokku --quiet buildpacks:add app https://github.com/heroku/heroku-buildpack-nginx.git
```

Color output respects [`NO_COLOR`](https://no-color.org/): set `NO_COLOR=1` to disable ANSI escapes, or pipe to a non-TTY (output is plain in that case automatically).

#### Inspecting and resuming with `--list-tasks` / `--start-at-task`

Two flags help when a recipe grows long: `--list-tasks` previews the resolved task plan without running, and `--start-at-task <name>` resumes a partially-applied recipe from a specific task.

`--list-tasks` walks the resolved plan (post `--play` / `--tags` filtering, post `loop:` expansion, post `when:` evaluation against inputs) and prints one line per envelope:

```text
$ docket apply --list-tasks
==> Play: api
[0] dokku apps:create api  [tags=core]
[1] dokku git:sync api  [tags=deploy]
[2] dokku config:set api  [tags=core,deploy]
[3] dokku ports:add api  [tags=deploy]
```

`when:` predicates that evaluate false against the inputs render as `[skipped]`. Predicates that reference `.registered.<name>` cannot be decided without running prior tasks, so they render as `[unknown]` rather than misreporting a skip. `block:` groups print the group line followed by indented `[block]` / `[rescue]` / `[always]` children.

`--start-at-task <name>` takes the exact envelope name (matching `name:` in the recipe). Earlier tasks render as `[skipped]` with a `(before --start-at-task)` reason and do not run; the matched task and every task after it run normally:

```text
$ docket apply --start-at-task "dokku config:set api"
==> Play: api
[skipped] dokku apps:create api    (before --start-at-task)
[skipped] dokku git:sync api       (before --start-at-task)
[ok]      dokku config:set api
[changed] dokku ports:add api

Summary: 4 tasks * 1 changed * 1 ok * 2 skipped * 0 errors  (took 1.1s)
```

Resolution order is: `--start-at-task` selects first, then `--tags` / `--skip-tags` filter, then per-task `when:` at execution time. A task can be selected by `--start-at-task` and still be filtered out by `--tags`. Inside a `block:`, matching a child does not unwind the group: the executor enters the block, skips earlier children, runs from the matched child onward, and continues with `rescue` / `always` per normal block semantics. For multi-play files the search walks every play in source order; `--play` narrows it.

If `--start-at-task` does not match any task name, the run exits 1 with the available names listed so the typo can be fixed quickly.

### Remote execution over SSH

Set `DOKKU_HOST=[user@]host[:port]` (or pass `--host`) to route every dokku invocation through an `ssh` subprocess so docket can manage a remote dokku server from a developer laptop or CI runner without installing the binary on the server. All invocations in one run share a single TCP+SSH connection via OpenSSH ControlMaster multiplexing.

```shell
# Apply against a remote dokku server.
DOKKU_HOST=deploy@dokku.example.com docket apply

# Same, via the CLI flag (overrides the env var).
docket apply --host deploy@dokku.example.com:2222
```

Because docket shells out to your `ssh` binary, the user's `~/.ssh/config`, `ProxyJump`, ssh-agent, and `known_hosts` work natively - you do not need to teach docket about them.

The flags are:

| Flag | Effect |
|------|--------|
| `--host <user@host:port>` | Remote host to ssh into. Overrides `DOKKU_HOST`. |
| `--sudo` | Wrap the remote `dokku` invocation in `sudo -n` (passwordless sudo only). Equivalent to `DOKKU_SUDO=1`. |
| `--accept-new-host-keys` | Pass `-o StrictHostKeyChecking=accept-new` so SSH adds an unknown host's key on first connect. Convenient for CI where pre-seeding `known_hosts` is impractical, but loses MITM protection on the first connection. Equivalent to `DOKKU_SSH_ACCEPT_NEW_HOST_KEYS=1`. Prefer pre-seeding via `ssh-keyscan host >> ~/.ssh/known_hosts` when you can. |

Errors are categorised so it is clear which side failed: SSH-level failures (connect refused, auth, host-key mismatch) render with an `ssh:` prefix, and remote dokku command failures render with a `dokku:` prefix.

```text
[error]   create app
          ! ssh: ssh deploy@dokku.example.com: Permission denied (publickey).
```

```text
[error]   add buildpack
          ! dokku: app foo does not exist
```

When a task references file paths (e.g. the `cert` and `key` fields on `dokku_certs`), those paths are interpreted on the *remote* host. Local file uploads are not implemented in this release; pre-place referenced files on the remote server.

### Previewing changes with `plan`

`docket plan` reads each task's current state from the live dokku server and reports what `apply` would change, without invoking any mutating dokku command. The output uses the same play header and column layout as `apply`, with a different marker set:

| Marker | Meaning |
|--------|---------|
| `[ok]` | Task is in sync; `apply` would not change anything |
| `[+]` | `apply` would create new state |
| `[~]` | `apply` would modify existing state |
| `[-]` | `apply` would remove existing state |
| `[!]` | The read-state probe itself errored (drift unknown) |

Tasks that perform multiple operations (e.g. `dokku_config` setting several keys) report each individual mutation under the task line:

```text
==> Play: tasks
[~]       configure  (2 key(s) to set)
          - set KEY_ONE (new)
          - set KEY_TWO (was set)

Plan: 1 task(s); 1 would change, 0 in sync, 0 error(s).
```

`Plan()` results drive `apply`: every task probes the server once, and `apply` reuses that probe to decide whether to mutate. `apply` on an already-converged server reports `Changed=false` for every task; back-to-back applies are no-ops by design.

A handful of tasks (notably `dokku_git_auth`, `dokku_registry_auth`, and `dokku_storage_ensure`) cannot probe their current state without invoking the corresponding dokku command, so their plan output reports drift unconditionally with `(... not probed)` in the reason.

The flags are:

| Flag | Effect |
|------|--------|
| `--tasks <path>` | Use a specific task file (YAML or JSON5). When omitted, docket probes `tasks.yml` -> `tasks.yaml` -> `tasks.json`. |
| `--json` | Suppress the human formatter and emit one JSON-lines event per `play_start`, `task`, or `summary` to stdout. Sensitive values mask to `***`. See "JSON output" below for the schema. |
| `--detailed-exitcode` | Exit `0` when no drift is detected, `2` when at least one task reports drift, `1` on read or probe error. Errors win over drift. Without this flag, plan exits `0` regardless of drift. Mirrors the `git diff --exit-code` / `terraform plan -detailed-exitcode` convention. |
| `--vars-file <path>` | Load input values from a YAML or JSON file. Repeatable; later files override earlier files for the same key. CLI `--name=value` flags always win. See "Layered input variables with `--vars-file`" below. |
| `--play <name>` | Plan only the play with this name. Matches the play's `name:` field; auto-named plays use `play #N`. Composes with `--tags`. |
| `--list-tasks` | Print the resolved task plan and exit without contacting the server. Honors `--play` / `--tags` / `--skip-tags` and shows expanded loop iterations and `[skipped]` markers for `when:`-skipped tasks. See "Inspecting and resuming" under `apply` for the full output shape. |

```shell
# CI gate: fail the job if any task would change the server.
docket plan --detailed-exitcode || exit $?
```

### JSON output

`docket apply --json` and `docket plan --json` emit one JSON-lines event per line on stdout. Every event carries a `version` integer pinned at `1`; consumers branch on `version` for forward compatibility. Sensitive values registered via inputs declared `sensitive: true` or task struct fields tagged `sensitive:"true"` are masked as `***`.

| Event | Required fields | Optional fields |
|-------|-----------------|-----------------|
| `play_start` | `version`, `type`, `name`, `ts` | `host` |
| `play_skipped` | `version`, `type`, `name`, `ts` | `when`, `reason` |
| `task` (apply) | `version`, `type`, `play`, `name`, `status` (`ok`/`changed`/`skipped`/`error`), `changed`, `state`, `desired_state`, `duration_ms`, `ts` | `error`, `commands` |
| `task` (plan) | `version`, `type`, `play`, `name`, `status` (`ok`/`+`/`~`/`-`/`skipped`/`error`), `would_change`, `state`, `desired_state`, `duration_ms`, `ts` | `reason`, `mutations`, `commands`, `error` |
| `summary` (apply) | `version`, `type`, `tasks`, `changed`, `ok`, `skipped`, `errors`, `plays_skipped`, `duration_ms` | - |
| `summary` (plan) | `version`, `type`, `tasks`, `would_change`, `in_sync`, `skipped`, `errors`, `plays_skipped`, `duration_ms` | - |

Both `task` event flavors include `commands` as an array of resolved, masked dokku command strings (singular `command` was considered but tasks like `dokku_buildpacks` legitimately invoke N subprocess calls, so an array preserves structure for `jq '.commands[]'`). The plan `commands` array reports the dokku invocations `apply` *would* run; the apply `commands` array reports what was actually executed. Both arrays use the same rendering rules, so plan output and apply output stay byte-identical for the same logical operation.

Sample `plan --json` line for a config task with two new keys:

```jsonl
{"version":1,"type":"task","play":"tasks","name":"configure","status":"~","would_change":true,"state":"present","desired_state":"present","reason":"2 key(s) to set","mutations":["set KEY (new)","set SECRET (new)"],"commands":["dokku --quiet config:set --encoded api KEY=*** SECRET=***"],"duration_ms":58,"ts":"2026-04-26T11:30:00Z"}
```

`--json` and `--detailed-exitcode` compose; CI pipelines can stream JSON to a dashboard while still branching on the exit code.

### Validating recipes with `validate`

`docket validate` performs offline schema and template checks against a `tasks.yml` without contacting any Dokku server, suitable for CI lint jobs that need to reject broken recipes before deploy.

The shipping checks cover: YAML parses, recipe shape (top-level list of plays with `inputs`/`tasks`), task entry shape (envelope keys plus exactly one task-type key), task type registered (with a "did you mean" suggestion for typos), required fields decode, sigil templates render against input defaults, expr predicates (`when:`, scalar-form `loop:`) parse, and `.item` / `.index` references stay inside a `loop:` body. Reserved envelope keys (`register`, `changed_when`, `failed_when`, `ignore_errors`) emit a "reserved but not yet supported" diagnostic until #210 lands.

```shell
docket validate --tasks path/to/tasks.yml
```

Exit codes are `0` when no problems are found and `1` otherwise. Five flags are available:

- `--json` emits one JSON-lines event per problem with a stable `version: 1` schema (`{"type":"validate_problem","code":"unknown_task_type", ...}`), suitable for piping into a CI annotator.
- `--strict` additionally flags any input declared `required: true` that has no `default` and no value supplied via a CLI flag or `--vars-file`, and verifies that any `--play` / `--start-at-task` references passed alongside resolve to real names in the file (problem codes `unknown_play_reference` / `unknown_start_at_task`) - useful in CI to ensure the recipe can be applied without runtime overrides or stale CLI invocations.
- `--vars-file <path>` loads input values from a YAML or JSON file (repeatable; later files override earlier; CLI `--name=value` flags always win). Values fed in through `--vars-file` count as overrides for `--strict`. See "Layered input variables with `--vars-file`" below.
- `--play <name>` (strict-only) verifies the named play exists in the recipe. Pair with `--strict` so a CI lint job catches stale `docket apply --play <name>` invocations.
- `--start-at-task <name>` (strict-only) verifies a task with this name exists in the recipe; narrowed by `--play` when both are set. Pair with `--strict` so a CI lint job catches typos in resume invocations before they reach `apply`.

A task file can also be specified via flag, and may be a file retrieved via http:

```shell
# alternate path (YAML)
docket apply --tasks path/to/task.yml

# JSON5 task file
docket apply --tasks path/to/tasks.json

# html file
docket apply --tasks http://dokku.com/docket/example.yml
```

Some other ideas:

- This could be automatically applied from within a repository if a `.dokku/task.yml` was found. In such a case, certain tasks would be added to a denylist and would be ignored during the run (such as dokku_app or dokku_sync).
- Dokku may expose a command such as dokku app:install that would allow users to invoke docket to install apps.
- A web ui could expose a web ui to customize remote task files and then call `docket` directly on the generated output.

### Inputs

Each app recipe can have custom inputs as specified in the `tasks.yml`. Inputs should _not_ reference any variable context, and are extracted using a two-phase parsing method (extract-then-inject).

```yaml
---
- inputs:
    - name: name
      default: "inflector"
      description: "Name of app to be created"
      required: true
  tasks:
    - dokku_app:
        app: {{ .name }}
    - dokku_sync:
        app: {{ .name }}
        repository: http://github.com/cakephp/inflector.cakephp.org
```

With the above, the following method is used to override the `name` variable. Omitting will use the default value.

```shell
# from the same directory as the tasks.yml
docket apply --name lollipop
```

Any inputs for a given task file will also show up in the `--help` output.

Inputs are injected using golang's `text/template` package via the `gliderlabs/sigil` library, and as such have access to everything `gliderlabs/sigil` does.

Inputs can have the following properties:

- name:
  - type: `string`
  - default: ``
- default:
  - type: `bool|float|int|string`
  - default: zero-value for the type
- description:
  - type: `string`
  - default: `""`
- required:
  - type: `bool`
  - default: `false`
- type:
  - type: string
  - default `string`
  - options:
    - `bool`
    - `float`
    - `int`
    - `string`

If all inputs are specified on the CLI, then they are injected as is. Otherwise, unless the `--no-interactive` flag is specified, `docket` will ask for values for each input, with the cli-specified values merged onto the task file default values as defaults.

Finally, the following input keys are reserved for internal usage:

- `help`
- `tasks`
- `v`
- `version`

#### Layered input variables with `--vars-file`

`apply`, `plan`, and `validate` all accept `--vars-file <path>` for loading input values from an external YAML or JSON file. The flag is repeatable; later files override earlier files for the same key. CLI `--name=value` flags always win.

The full precedence order, lowest to highest:

| Layer | Source |
|-------|--------|
| 1 | File-level `inputs:` defaults declared in `tasks.yml` |
| 2 | Per-play `inputs:` defaults (active; see "Multi-play recipes" above) |
| 3 | `--vars-file <path>` (repeatable; later files override earlier) |
| 4 | `--name=value` CLI flags (always win) |

Vars files hold a flat string-keyed map of input name to value:

```yaml
# prod.yml
app: api
repo: https://github.com/example/api.git
replicas: 3
debug: false
```

JSON works the same way; any path ending in `.json` parses as JSON, anything else parses as YAML:

```json
{
  "app": "api",
  "repo": "https://github.com/example/api.git",
  "replicas": 3,
  "debug": false
}
```

Values are coerced to the input's declared `type:`:

- `string`: any scalar (a YAML bool `true` becomes `"true"`).
- `int`: native ints and whole-number floats; numeric strings via `strconv.Atoi`. JSON numbers always decode as floats and are accepted when whole.
- `float`: native floats, ints, and parseable strings.
- `bool`: native bools and the same string forms `--name=value` accepts (`true`/`yes`/`on`/`y`/`Y` and the matching false set).

Unknown keys (vars-file keys that do not correspond to a declared input) are a hard error with a `did you mean` suggestion against the closest declared input name:

```text
unknown input "appp" in --vars-file prod.yml; did you mean "app"?
```

Common patterns:

```shell
# Layer environment-specific values over the recipe defaults.
docket apply --tasks tasks.yml --vars-file prod.yml

# Stack a base file under a per-environment override; override the app on the CLI.
docket plan --tasks tasks.yml \
  --vars-file base.yml --vars-file prod.yml \
  --app=api-canary
```

### Tasks

All implemented tasks should closely follow those available via the `ansible-dokku` library. Additionally, `docket` will expose a few custom tasks that are specific to this package to ease migration from pure ansible.

Tasks will have both a `name` and an execution context, where the context maps to a single implemented modules. Tasks can be templated out via the variables from the `inputs` section, and may also use any functions exposed by `gliderlabs/sigil`.

#### Adding a new task

Task executors should be added by creating an `tasks/${TASK_NAME}_task.go`. The Task name should be `lower_underscore_case`. By way of example, a `tasks/lollipop_task.go` would contain the following:

```go
package main

type LollipopTask struct {
  App   string `required:"true" yaml:"app"`
  State State `required:"true" yaml:"state" default:"present"`
}

func (t LollipopTask) Plan() PlanResult {
  return DispatchPlan(t.State, map[State]func() PlanResult{
    "present": func() PlanResult {
      // Probe the server once, decide whether to mutate.
      if /* already in desired state */ {
        return PlanResult{InSync: true, Status: PlanStatusOK}
      }
      return PlanResult{
        InSync:    false,
        Status:    PlanStatusCreate, // or PlanStatusModify, PlanStatusDestroy
        Reason:    "...",
        Mutations: []string{"create lollipop"},
        apply: func() TaskOutputState {
          // Run the underlying dokku command. Return Changed=true on success.
          return TaskOutputState{Changed: true, State: StatePresent}
        },
      }
    },
    "absent": func() PlanResult { /* ... */ },
  })
}

func (t LollipopTask) Execute() TaskOutputState {
  return ExecutePlan(t.Plan())
}

func init() {
    RegisterTask(&LollipopTask{})
}
```

The `LollipopTask` struct contains the fields necessary for the task. The only necessary field is `State`, which holds the desired state of the task. All other fields are completely custom for the task at hand.

`Plan()` is the canonical implementation: it probes the live server once, computes the diff, and returns a `PlanResult`. When `InSync` is `false`, `Plan()` embeds an `apply` closure that performs the underlying mutation. For tasks that perform multiple operations (e.g. setting several config keys in one call), populate `PlanResult.Mutations` with one entry per atomic change so the plan output can itemize the diff.

`Execute()` is always `return ExecutePlan(t.Plan())`. The shared `ExecutePlan` helper handles the InSync, error, and apply cases uniformly so the per-state mutation logic lives in exactly one place per task.

`DispatchPlan` and `DispatchState` automatically set `DesiredState` on the returned result.

The `init()` function registers the task for usage within a recipe.
