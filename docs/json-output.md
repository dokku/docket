# JSON output

`docket apply --json` and `docket plan --json` replace the human-readable output with one JSON
object per line (JSON-lines). This is what you reach for when a CI pipeline or dashboard needs to
consume the result programmatically instead of scraping text. `docket validate --json` does the same
for offline problems, and every stream has a [JSON Schema](#schemas).

Every event carries a `version` integer, pinned at `1`. Consumers should branch on `version` so a
future schema change does not silently break them. Values marked sensitive - inputs declared
`sensitive: true`, or task fields tagged `sensitive:"true"` - are masked as `***`. Masking covers
every string field a secret can reach, including `name` and `play` (a loop over a sensitive value
expands the task name) and the `when` / `reason` fields on `play_skipped` (a play predicate can
interpolate a sensitive input). It applies to every stream docket emits: the `apply` / `plan` run
stream, `validate --json`, and `--list-tasks --json` - including that stream's `loop_item`, whose
strings are masked at any depth, object keys included.

What is never masked is docket's own vocabulary: the `type`, `phase`, `probe`, and `status` fields
are fixed enums rather than recipe content, and masking one would emit a stream that fails the
schemas below.

## Correlating runs

`name` is the correlation key. Two runs of the same recipe emit the same `name` for the same task,
so a dashboard or a CI job comparing `plan` to `apply` can line them up. A task with no `name:` in
the recipe is named after the [resource it manages](task-envelope.md#names-and-resource-addresses) -
`dokku_config[app=api]` - which is stable for the same reason.

Four caveats on masking, each of which can make two distinct tasks indistinguishable in the stream:

- A generated name embeds the task's identity field values. No field is ever both an identity key
  and declared sensitive, but a `sensitive: true` input interpolated into one still reaches the
  name, and it is masked there. A name that renders entirely as `***` also cannot be pasted back
  into `--start-at-task`, which matches on the real name.
- Masking is substring replacement. A short sensitive value can mask a large part of two different
  names into the same `***`-bearing string. Pin a `name:` on the tasks a consumer must tell apart.
- A sensitive value carrying leading or trailing whitespace masks its trimmed spelling as well as
  its literal one. Docket trims some of the text it builds - a loop item in a task name, for one -
  and substring replacement would otherwise miss the trimmed form, so both are masked everywhere
  they appear. Padding a secret therefore widens what masks rather than narrowing it.
- A sensitive value that a generated name has to quote masks its escaped spelling as well as its
  literal one, for the same reason. An address quotes a key value holding a comma, a `]`, or a `"`,
  and quoting escapes what it wraps, so `sec"ret` reaches the name as `sec\"ret`. Both spellings
  are masked everywhere they appear.

## Events

One event is emitted per play start, per task, and one summary at the end. The fields differ
slightly between `apply` and `plan`:

| Event | Required fields | Optional fields |
|-------|-----------------|-----------------|
| `play_start` | `version`, `type`, `name`, `ts` | `host` |
| `play_skipped` | `version`, `type`, `name`, `ts` | `when`, `reason` |
| `warning` | `version`, `type`, `play`, `name`, `reason`, `message`, `ts` | - |
| `task` (apply) | `version`, `type`, `play`, `name`, `status` (`ok`/`changed`/`skipped`/`error`), `changed`, `state`, `desired_state`, `duration_ms`, `ts` | `error`, `skip_reason`, `stdout`, `stderr`, `exit_code`, `ignored`, `commands`, `phase`, `group` |
| `task` (plan) | `version`, `type`, `play`, `name`, `status` (`ok`/`+`/`~`/`-`/`skipped`/`error`), `would_change`, `state`, `desired_state`, `duration_ms`, `ts` | `reason`, `mutations`, `commands`, `error`, `phase`, `group` |
| `summary` (apply) | `version`, `type`, `tasks`, `changed`, `ok`, `skipped`, `errors`, `plays_skipped`, `duration_ms` | - |
| `summary` (plan) | `version`, `type`, `tasks`, `would_change`, `in_sync`, `skipped`, `errors`, `plays_skipped`, `duration_ms` | - |

A few fields need a word of explanation:

- `skip_reason` accompanies a `skipped` apply task when a reason was recorded.
- `stdout`, `stderr`, and `exit_code` are the failing command's output and are present only on an
  errored apply task. `ignored` is `true` when [`ignore_errors`](task-envelope.md#ignore_errors-continue-past-a-failure)
  swallowed the error, in which case the task counts toward neither `errors` nor the exit code.
- `phase` is `block`, `rescue`, or `always` on a child of a
  [group](task-envelope.md#block--rescue--always-structured-error-handling); `group` is `true` on the
  group envelope itself.
- On a plan task, `state` mirrors `desired_state`. Plan never mutates, so it has no post-mutation
  state to report; the field exists so `task` events have the same key set on both commands.

A `warning` event precedes the `task` event it is associated with so consumers can correlate by
ordering. The `reason` is a stable machine key so consumers can branch on the category:

| `reason` | Emitted when |
|----------|--------------|
| `deprecated` | A task whose type implements `Deprecation()` is about to run; `message` carries the deprecation notice. |
| `unknown_property` | A property task's probe found no matching key in the plugin's `:report --format json` payload (a stale key map or a dokku version that does not emit it). |
| `probe_rejected` | An older plugin rejected `:report --format json` outright, so the property task could not probe current state. |

In every case `message` is masked, so a registered sensitive value that reaches the warning (for
example server stderr echoed by a rejected probe) renders as `***`. `--list-tasks --json` does not
emit a separate `warning` event; instead, the `list_task` event for a deprecated task carries
`"deprecated": true` and a `deprecation` field with the message, masked the same way.

`unknown_property` and `probe_rejected` are probe failures: this run could not read the state, and
another run against a different server or a newer plugin might. A task type that can *never* read
its state is a different thing, declared rather than discovered, and it is not a warning at all. The
`list_task` event carries it as `"probe": "unsupported"` (the task reports drift on every run) or
`"probe": "partial"` (only some of its fields are read), each with a `probe_caveat` naming what
cannot be read. Both keys are absent for a task that probes everything it manages. `probe_caveat` is
prose and is masked; `probe` is an enum and is not. A recipe containing an `unsupported` task never
exits `0` under `--detailed-exitcode`.

## Commands

Both `task` flavors include `commands` as an array of resolved, masked `dokku` command strings. It
is an array rather than a single string because some tasks (such as `dokku_buildpacks`) legitimately
run several commands, and an array keeps that structure for `jq '.commands[]'`. The `plan` array
reports the commands `apply` *would* run; the `apply` array reports what it *did* run. Both use the
same rendering, so plan and apply output stay byte-identical for the same logical operation.

A `plan --json` line for a config task with two new keys:

```jsonl
{"version":1,"type":"task","play":"tasks","name":"configure","status":"~","would_change":true,"state":"present","desired_state":"present","reason":"2 key(s) to set","mutations":["set KEY (new)","set SECRET (new)"],"commands":["dokku --quiet config:set --encoded api KEY=*** SECRET=***"],"duration_ms":58,"ts":"2026-04-26T11:30:00Z"}
```

## Validate problems

`docket validate --json` writes a different stream: one `validate_problem` object per problem, and
nothing at all when the recipe is clean. A clean run exits `0` with empty stdout, so a consumer can
treat any output as failure without parsing it. Every problem carries `version`, `type`, `code`, and
`message`; `play`, `task`, `line`, `column`, and `hint` appear when they are known. `message` and
`hint` are masked.

```jsonl
{"code":"unknown_task_type","column":7,"hint":"did you mean \"dokku_app\"?","line":4,"message":"unknown task type \"dokku_appp\"","play":"play #1","task":"task #1 \"typo\"","type":"validate_problem","version":1}
{"code":"missing_required_field","column":9,"line":8,"message":"missing required field \"app\" on dokku_config","play":"play #1","task":"task #2 \"configure\"","type":"validate_problem","version":1}
```

`code` is a stable machine key. Branch on it rather than on `message`, which is prose and may be
reworded:

| `code` | Reported when |
|--------|---------------|
| `yaml_parse` | The recipe is not parseable YAML. |
| `json5_parse` | The recipe is not parseable JSON5. |
| `duplicate_key` | The same key appears twice in one mapping. |
| `recipe_shape` | The recipe is not a list of plays, or a play is not a mapping. |
| `task_entry_shape` | A task entry does not carry exactly one task-type key. |
| `empty_task_body` | A task-type key has a null body (`dokku_app:` with nothing after it). |
| `unknown_task_type` | The task-type key is not registered. `hint` carries a did-you-mean. |
| `unknown_key` | An unrecognized envelope key sits alongside the task-type key. |
| `envelope_key_type` | An envelope key has the wrong type (for example `tags:` as a string). |
| `duplicate_task_name` | Two tasks in one play share a `name`. |
| `block_shape` | A `block` / `rescue` / `always` clause is not a list of task entries. |
| `block_empty` | A `block:` clause contains no child tasks. |
| `block_orphan_clause` | A task entry declares `rescue` or `always` with no `block`. |
| `block_with_task_type` | A group entry also carries a task-type key. |
| `envelope_key_unsupported` | An envelope key is reserved for a future release but not yet implemented. `hint` names the tracking issue. |
| `task_body_decode` | The task body does not decode into the task's struct. |
| `missing_required_field` | A field tagged `required:"true"` is absent or zero. |
| `invalid_task_input` | A task's own `Validate()` rejected the combination of fields - conditional requirements, mutually-exclusive fields, enum values. |
| `template_render` | A `{{ ... }}` template failed to render. |
| `unsafe_input_value` | An input value would break the scalar it is substituted into. See [special characters in values](inputs.md#special-characters-in-values). |
| `input_missing` | (`--strict`) A `required: true` input has no default and no supplied value. |
| `invalid_input_name` | An input name is not a valid `{{ .name }}` variable - a hyphenated name, for example. |
| `reserved_input_name` | An input name collides with a built-in flag. |
| `register_duplicate` | Two tasks `register:` the same name. |
| `loop_var_outside_loop` | `.item` or `.index` is referenced outside a `loop:`. |
| `expr_compile` | A `when` / `changed_when` / `failed_when` predicate does not compile. |
| `unknown_play_reference` | (`--strict`) `--play` names a play that does not exist. |
| `unknown_start_at_task` | (`--strict`) `--start-at-task` names a task that does not exist. |
| `vars_file_error` | A `--vars-file` could not be read or parsed. |
| `argument_error` | The command's own arguments are invalid. |
| `read_error` | The recipe could not be read. |

## Schemas

The three streams have machine-readable JSON Schemas, so a consumer can validate what it parses
instead of trusting the tables above. Each is drafted against 2020-12 and describes **one line**, not
the whole stream: split on newlines and validate each line independently.

| Stream | Schema |
|--------|--------|
| `apply --json`, `plan --json` | [`schemas/events-v1.schema.json`](schemas/events-v1.schema.json) |
| `apply --list-tasks --json`, `plan --list-tasks --json` | [`schemas/list-tasks-v1.schema.json`](schemas/list-tasks-v1.schema.json) |
| `validate --json` | [`schemas/validate-v1.schema.json`](schemas/validate-v1.schema.json) |

[`docket schema`](task-catalog.md) also emits JSON, but it is not one of these streams: it is a
single document describing docket's task types rather than a line-per-event record of a run, and
its schema - [`schemas/task-catalog-v1.schema.json`](schemas/task-catalog-v1.schema.json) -
validates the whole document at once. Its `version` is its own, independent of the `version` on
the events above.

`--list-tasks --json` has its own schema because it is a different stream, not a subset of the run
stream: no task executes, no server is contacted, its per-task event is `list_task` rather than
`task`, there is no `summary`, and its `play_skipped` carries `play` where the run stream's carries
`name` and omits `ts`.

Every schema sets `additionalProperties: false`, and docket's own test suite validates real emitted
events against these files, so a field that exists in the code but not in the schema fails CI.

## Composing with exit codes

`--json` and `--detailed-exitcode` compose, so a pipeline can stream JSON to a dashboard while still
branching on the [plan exit code](command-reference.md#docket-plan):

```bash
docket plan --json --detailed-exitcode | tee plan.jsonl
```

`apply` takes the same flag, which is the cheapest way to answer "did anything change?" without
parsing the stream at all: `0` means nothing changed, `2` means something did, `1` means an error.
Without it, `apply` exits `0` either way. The equivalent signal inside the stream is
`summary.changed > 0` for the run, or `task.changed` per task:

```bash
docket apply --json --detailed-exitcode | tee apply.jsonl
# $? would be tee's status, not docket's; read the head of the pipeline instead.
case "${PIPESTATUS[0]}" in
  0) echo "no changes" ;;
  2) echo "changed" ;;
  *) echo "failed" ;;
esac
```

A load-time failure - an unreadable recipe, a parse error, an unknown task type - happens before the
emitter starts, so it produces **no JSON on stdout** and a human-readable message on stderr. Treat a
non-zero exit with empty stdout as a failure whose detail is on stderr, or run
`docket validate --json` first to get the same problems as structured events.

Do not read that the other way round: a non-zero exit does not imply empty stdout.
`--list-tasks --json` exits `1` when a `when:` fails to evaluate, and the stream is complete. For a
play `when:`, the failure is a `play_skipped` event whose `reason` carries the `when error: <err>`
form, followed by every remaining play's tasks. For a task `when:`, it is a `list_task` event
carrying `"when_error": true`, followed by every remaining task.

## See also

- [Command reference](command-reference.md) - the `--json` and `--detailed-exitcode` flags
- [Task catalog](task-catalog.md) - the `docket schema` document, a separate JSON format
- [Wrapping docket from ansible-dokku](ansible-dokku.md) - a worked consumer of these streams
- [Task envelope](task-envelope.md#ignore_errors-continue-past-a-failure) - how `ignore_errors` shows up as `"ignored": true`
