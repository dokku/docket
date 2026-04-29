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

Run it:

```shell
# from the same directory as the tasks.yml
docket apply
```

Running `docket` with no subcommand prints the available commands. Use `docket init` to scaffold a starter `tasks.yml`, `docket apply` to execute a task file, `docket fmt` to canonically format a task file, `docket plan` to preview the changes a task file would make without mutating any state, `docket validate` to check a task file's schema and templates without contacting the server, or `docket version` to print the binary's version.

### Task envelope

Each task entry in `tasks.yml` admits a small set of cross-cutting envelope keys alongside the single `dokku_*` task-type key. Body templating uses [sigil](https://github.com/gliderlabs/sigil) (`{{ .input }}` substitutions) and envelope predicates use [expr-lang/expr](https://github.com/expr-lang/expr) so the two languages live in clearly separate positions.

| Key | Status | What it does |
|-----|--------|--------------|
| `name` | active | Human label for the task. Auto-generated when omitted. |
| `tags` | active | Tag list filtered by `--tags` / `--skip-tags` on `apply` and `plan`. |
| `when` | active | expr expression. Falsy renders the task as `[skipped]`. |
| `loop` | active | List literal, or expr returning a list. Expands one entry into N with `.item` / `.index` available in the body. |
| `register` | reserved (#210) | Bind the task result for downstream tasks. |
| `changed_when` | reserved (#210) | expr override for the task's "changed" verdict. |
| `failed_when` | reserved (#210) | expr override for the task's "failed" verdict. |
| `ignore_errors` | reserved (#210) | Continue on task failure. |

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

The expression context today carries the file-level inputs plus, for loop expansions, `.item` and `.index`. Other context keys (`.timestamp`, `.host`, `.play.name`, `.result`, `.registered`) are reserved for follow-on issues.

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

### Scaffolding with `init`

`docket init` writes a starter `tasks.yml` from an embedded template. It is offline only: no Dokku server contact, no `git` subprocess. The default scaffold ships four tasks (`dokku_app`, `dokku_config`, `dokku_domains`, `dokku_git_sync`) wrapped in a single play with `app` and `repo` inputs, and round-trips cleanly through `docket validate`.

```shell
# Use cwd basename as the app and remote.origin.url from ./.git/config as the repo
docket init

# Stream the rendered YAML to stdout for piping
docket init --output -
```

The flags are:

| Flag | Effect |
|------|--------|
| (default) | Write `./tasks.yml`; refuse if the file exists |
| `--output <path>` | Write to a specific path; `-` writes to stdout |
| `--force` | Overwrite an existing file |
| `--name <name>` | Override the play and `app` input default (defaults to the cwd basename) |
| `--repo <url>` | Override the `repo` input default (defaults to `remote.origin.url` in `./.git/config`, if present) |
| `--minimal` | One-task example with no `inputs:` block |

### Formatting recipes with `fmt`

`docket fmt` is a canonical formatter for `tasks.yml`, in the spirit of `gofmt`. It parses with `gopkg.in/yaml.v3`'s `Node` API so head, line, and foot comments round-trip; reorders task envelope and play keys into a stable order; normalises indentation to a 2-space step; and inserts blank lines between top-level plays and between top-level task entries. The default rewrites `./tasks.yml` in place; `--check` and `--diff` are read-only modes. The CLI flags compose, modeled after `black` / `ruff format`.

```shell
# Rewrite ./tasks.yml in place.
docket fmt

# CI gate: print the diff and exit 1 if anything is not canonical.
docket fmt --check --diff

# Read from stdin, write canonical to stdout.
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
| `--tasks <path>` | Use a specific task file (default `./tasks.yml`) |
| `--verbose` | After each task line, echo every resolved Dokku command the task ran on a `→`-prefixed continuation line, in invocation order. Tasks that loop over inputs (e.g. `dokku_buildpacks` adding several URLs) emit one continuation per call. Commands are masked against the global sensitive value set. Ignored when `--json` is set; the JSON output already includes the resolved commands. |
| `--json` | Suppress the human formatter and emit one JSON-lines event per `play_start`, `task`, or `summary` to stdout. Sensitive values mask to `***`. See "JSON output" below for the schema. |

For example, a multi-command task renders one continuation per invocation:

```text
[changed] add buildpacks
          → dokku --quiet buildpacks:add app https://github.com/heroku/heroku-buildpack-nodejs.git
          → dokku --quiet buildpacks:add app https://github.com/heroku/heroku-buildpack-nginx.git
```

Color output respects [`NO_COLOR`](https://no-color.org/): set `NO_COLOR=1` to disable ANSI escapes, or pipe to a non-TTY (output is plain in that case automatically).

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
| `--tasks <path>` | Use a specific task file (default `./tasks.yml`) |
| `--json` | Suppress the human formatter and emit one JSON-lines event per `play_start`, `task`, or `summary` to stdout. Sensitive values mask to `***`. See "JSON output" below for the schema. |
| `--detailed-exitcode` | Exit `0` when no drift is detected, `2` when at least one task reports drift, `1` on read or probe error. Errors win over drift. Without this flag, plan exits `0` regardless of drift. Mirrors the `git diff --exit-code` / `terraform plan -detailed-exitcode` convention. |

```shell
# CI gate: fail the job if any task would change the server.
docket plan --detailed-exitcode || exit $?
```

### JSON output

`docket apply --json` and `docket plan --json` emit one JSON-lines event per line on stdout. Every event carries a `version` integer pinned at `1`; consumers branch on `version` for forward compatibility. Sensitive values registered via inputs declared `sensitive: true` or task struct fields tagged `sensitive:"true"` are masked as `***`.

| Event | Required fields | Optional fields |
|-------|-----------------|-----------------|
| `play_start` | `version`, `type`, `name`, `ts` | `host` |
| `task` (apply) | `version`, `type`, `play`, `name`, `status` (`ok`/`changed`/`skipped`/`error`), `changed`, `state`, `desired_state`, `duration_ms`, `ts` | `error`, `commands` |
| `task` (plan) | `version`, `type`, `play`, `name`, `status` (`ok`/`+`/`~`/`-`/`skipped`/`error`), `would_change`, `state`, `desired_state`, `duration_ms`, `ts` | `reason`, `mutations`, `commands`, `error` |
| `summary` (apply) | `version`, `type`, `tasks`, `changed`, `ok`, `skipped`, `errors`, `duration_ms` | - |
| `summary` (plan) | `version`, `type`, `tasks`, `would_change`, `in_sync`, `skipped`, `errors`, `duration_ms` | - |

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

Exit codes are `0` when no problems are found and `1` otherwise. Two flags are available:

- `--json` emits one JSON-lines event per problem with a stable `version: 1` schema (`{"type":"validate_problem","code":"unknown_task_type", ...}`), suitable for piping into a CI annotator.
- `--strict` additionally flags any input declared `required: true` that has no `default` and no value supplied via a CLI flag - useful in CI to ensure the recipe can be applied without runtime overrides.

A task file can also be specified via flag, and may be a file retrieved via http:

```shell
# alternate path
docket apply --tasks path/to/task.yml

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
