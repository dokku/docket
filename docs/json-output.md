# JSON output

`docket apply --json` and `docket plan --json` replace the human-readable output with one JSON
object per line (JSON-lines). This is what you reach for when a CI pipeline or dashboard needs to
consume the result programmatically instead of scraping text.

Every event carries a `version` integer, pinned at `1`. Consumers should branch on `version` so a
future schema change does not silently break them. Values marked sensitive - inputs declared
`sensitive: true`, or task fields tagged `sensitive:"true"` - are masked as `***`. Masking covers
every string field a secret can reach, including `name` and `play` (a loop over a sensitive value
expands the task name) and the `when` / `reason` fields on `play_skipped` (a play predicate can
interpolate a sensitive input).

## Events

One event is emitted per play start, per task, and one summary at the end. The fields differ
slightly between `apply` and `plan`:

| Event | Required fields | Optional fields |
|-------|-----------------|-----------------|
| `play_start` | `version`, `type`, `name`, `ts` | `host` |
| `play_skipped` | `version`, `type`, `name`, `ts` | `when`, `reason` |
| `warning` | `version`, `type`, `play`, `name`, `reason`, `message`, `ts` | - |
| `task` (apply) | `version`, `type`, `play`, `name`, `status` (`ok`/`changed`/`skipped`/`error`), `changed`, `state`, `desired_state`, `duration_ms`, `ts` | `error`, `commands` |
| `task` (plan) | `version`, `type`, `play`, `name`, `status` (`ok`/`+`/`~`/`-`/`skipped`/`error`), `would_change`, `state`, `desired_state`, `duration_ms`, `ts` | `reason`, `mutations`, `commands`, `error` |
| `summary` (apply) | `version`, `type`, `tasks`, `changed`, `ok`, `skipped`, `errors`, `plays_skipped`, `duration_ms` | - |
| `summary` (plan) | `version`, `type`, `tasks`, `would_change`, `in_sync`, `skipped`, `errors`, `plays_skipped`, `duration_ms` | - |

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
`"deprecated": true` and a `deprecation` field with the message.

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

## Composing with exit codes

`--json` and `--detailed-exitcode` compose, so a pipeline can stream JSON to a dashboard while still
branching on the [plan exit code](command-reference.md#docket-plan):

```bash
docket plan --json --detailed-exitcode | tee plan.jsonl
```

## See also

- [Command reference](command-reference.md) - the `--json` and `--detailed-exitcode` flags
- [Task envelope](task-envelope.md#ignore_errors-continue-past-a-failure) - how `ignore_errors` shows up as `"ignored": true`
