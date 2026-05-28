# Task envelope

Every entry in a `tasks:` list has exactly one `dokku_*` key that says *what* to do. Alongside it
you can add **envelope keys** that control *whether, how often, and under what conditions* the task
runs, and what to do when it fails. The envelope is the same for every task type, so once you learn
it you can apply it anywhere.

| Key | What it does |
|-----|--------------|
| `name` | A human label for the task, shown in the output. Auto-generated when omitted. |
| `tags` | A tag list, filtered by `--tags` / `--skip-tags`. |
| `when` | A condition; when false the task renders as `[skipped]`. |
| `loop` | Expand one entry into many, once per item in a list. |
| `register` | Save this task's result for later tasks to read. |
| `changed_when` | Override whether the task counts as "changed". |
| `failed_when` | Override whether the task counts as "failed". |
| `ignore_errors` | Continue the run even if this task errors. |
| `block` / `rescue` / `always` | Try / catch / finally over a group of tasks. |

An unknown envelope key is rejected when the recipe is parsed, with a "did you mean" suggestion for
the closest valid key.

Two small expression languages are used in the envelope, and they live in separate places so they
do not get confused:

- **Task bodies** use [sigil](https://github.com/gliderlabs/sigil) templates: `{{ .app }}`.
- **Envelope predicates** (`when`, `loop`, `changed_when`, `failed_when`) use
  [expr](https://github.com/expr-lang/expr): `app == "api"`.

## Tags

Tags are free-form labels on a task. They let you run a subset of a recipe without splitting it
into separate files:

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

`--tags` keeps only tasks whose tags intersect the list; untagged tasks are excluded. `--skip-tags`
drops tasks whose tags intersect the list; untagged tasks are kept. Passing both keeps the
intersection of "kept by `--tags`" and "not dropped by `--skip-tags`":

```bash
docket plan  --tasks tasks.yml --tags api          # only the api task
docket apply --tasks tasks.yml --skip-tags worker  # everything except worker
```

## `when`: run a task conditionally

`when:` is an expr expression evaluated for each task just before it runs. A false result renders
the task as `[skipped]` and counts toward the "skipped" total. This is how you make an environment-
specific step, for example enabling Let's Encrypt only in production:

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

The expression sees the file-level inputs. Inside a `loop:` it also sees `.item` and `.index`.
Once any task has used `register:`, every later predicate also sees `.registered.<name>` (below).

## `loop`: repeat a task over a list

`loop:` expands a single task into one copy per item, so you do not repeat yourself. The value is
either a literal list:

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

Each iteration renders the body with `.item` (the current value) and `.index` (its zero-based
position) available. Referencing `.item` or `.index` outside a `loop:` is rejected when the recipe
is parsed, so a stray reference cannot silently render to nothing.

`when:` is evaluated per iteration, so `loop: [a, b, c]` with `when: 'item != "b"'` runs only the
`a` and `c` iterations.

## `register`: pass data between tasks

`register: <name>` saves the finished task's result under that name. Every later predicate can read
it as `.registered.<name>`. This lets one task react to what an earlier task did - for example,
stamping a config value only the first time an app is created:

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

A registered value exposes the same fields the task reported: `.Changed`, `.Error`, `.State`,
`.DesiredState`, `.Stdout`, `.Stderr`, `.ExitCode`, `.Commands`, and `.Message`. Comparisons like
`registered.foo.Error != nil` or `registered.foo.Stderr contains "..."` work directly. A name can
only be registered once - a reused name is rejected at parse time. The registered map is shared
across every play in one run.

When `register:` is combined with `loop:`, the value also carries a `.Results` list with one entry
per iteration. The top-level fields aggregate: `.Changed` is true if any iteration changed,
`.Error` is the first error, and the rest mirror the last iteration:

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

## `changed_when` and `failed_when`: override the verdict

Each task reports its own "changed" and "failed" verdicts. Sometimes that verdict is wrong for your
situation - an operation that "fails" when there is nothing to do, or a step you never want counted
as a change. `changed_when:` and `failed_when:` are expr predicates that override those verdicts.
Both evaluate against `.result` (the finished task's state) plus the usual context (`.registered`,
inputs, loop vars). They are applied in order: `failed_when`, then `changed_when`, then the
`register` snapshot - so a registered value reflects the overrides.

`failed_when` is the standard "this exit code is fine" idiom for idempotent operations. A truthy
result marks the task failed; a falsy result fully clears the failure:

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

`changed_when` rewrites the "changed" flag. `changed_when: 'false'` silences a task that reports
itself as changed; `changed_when: 'true'` makes an in-sync task render as changed.

## `ignore_errors`: continue past a failure

`ignore_errors: true` lets the run continue when a task errors. The task still shows as `[error]`
(with an `(ignored)` marker) and as `"status": "error"` with `"ignored": true` in JSON, but the run
does not abort and the error does not count toward the summary:

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

`ignore_errors` is checked after `failed_when`, so a failure already cleared by `failed_when` is not
re-flagged. It only matters for `apply`; `plan` never aborts a run, so it is a no-op there.

## `block` / `rescue` / `always`: structured error handling

A task entry that carries `block:` becomes a group: a try / catch / finally over a list of child
tasks. Use it when a sequence of steps needs a cleanup or rollback path. The outer envelope
(`name`, `tags`, `when`, `loop`, `register`, and so on) applies to the whole group:

```yaml
- tasks:
    - name: deploy with rollback
      block:
        - dokku_app_clone:     { source_app: api, app: api-candidate }
        - dokku_git_sync:      { app: api-candidate, remote: "{{ .repo }}" }
        - dokku_checks_toggle: { app: api-candidate, state: enabled }
      rescue:
        - dokku_app: { app: api-candidate, state: absent }
      always:
        - dokku_config:
            app: api
            config:
              LAST_DEPLOY_ATTEMPT: "now"
```

The rules:

- `block:` children run in order. The first one that fails (and whose own `ignore_errors` is false)
  stops the block.
- `rescue:` children run, in order, only when a block child failed. They run with the failing
  child's result bound to `.failed_task`, so a rescue can branch on the cause:
  `when: 'failed_task.Stderr contains "..."'`.
- `always:` children run unconditionally after `block:` / `rescue:`, even if a rescue itself errored.
- The group's own envelope applies to the combined outcome: `register:` saves the post-rescue,
  post-always result, and `ignore_errors: true` on the group swallows any error that remains after
  rescue and always.
- `ignore_errors: true` on a *child* of `block:` swallows that child's error and continues; it does
  not trigger `rescue:`. Rescue is the "handle it" path; `ignore_errors` is the "swallow it" path.
- `loop:` on a group runs the whole group once per item, with `.item` / `.index` shared by every
  child.
- Groups nest: a group can appear inside another group's `block:`, `rescue:`, or `always:`.

Under `plan`, block children are always checked for drift. `rescue:` and `always:` children are
only planned when at least one block child reports drift or a probe error. In the output, group
children carry a `[block]` / `[rescue]` / `[always]` prefix and the group's summary line is marked
`(group)`.

## See also

- [Recipes](recipes.md) - play-level `tags` and `when`
- [Inputs](inputs.md) - the variables available to envelope expressions
- [JSON output](json-output.md) - how `ignored`, `changed`, and `status` appear in JSON
