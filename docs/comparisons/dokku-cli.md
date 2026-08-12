# plan/apply and idempotency: docket vs the dokku CLI

The dokku CLI is imperative. Each subcommand does one thing right now: `apps:create` makes an app [^1],
`config:set` sets environment variables [^2], `domains:add` attaches a hostname [^3], and `git:sync`
clones or fetches a repository and can build it [^4]. You run those commands in sequence, by hand, to
bring an app into the shape you want.

docket does not replace that CLI - it drives it. Every docket task ultimately resolves to one or more
`dokku` commands and runs them; `apply --verbose` echoes each resolved `dokku` line it invoked [^6],
and both `plan` and `apply` render the exact command strings they would run or did run [^10]. dokku must
still be installed - locally, or on the server docket reaches over SSH [^5][^11]. docket is a
declarative desired-state layer over the same commands, not an alternative to them.

The difference is what sits between you and those commands. With the bare CLI, ordering, re-run safety,
and "what would this change?" are the operator's job: docket's own getting-started framing notes that
the steps "live in your shell history or a README, they are easy to run out of order, and there is no
safe way to ask 'what would change if I ran these again?'" [^5]. docket reads a file describing the state
you want, compares it to the live server, and runs only the commands needed to close the gap [^5].

This is a neutral comparison. The bare CLI needs no extra tool and gives direct, interactive control,
which suits one-off and exploratory work. docket adds a layer that pays off when you want repeatability,
review, previews, and idempotency. Both ultimately run the same `dokku`.

## At a glance

| Dimension | docket | dokku CLI |
|-----------|--------|-----------|
| What you write | a declarative recipe (desired state) [^7] | a sequence of imperative commands [^1][^2][^3][^4] |
| Underlying execution | resolves to and runs `dokku` commands [^6] | the `dokku` commands themselves [^1][^2][^3][^4] |
| Requires dokku installed | yes - local or over SSH [^5][^11] | yes [^1] |
| Re-running is safe | yes; an in-sync task is a no-op [^5] | depends on the command; a create-style step can be redundant or error [^5] |
| Dry run / preview | `docket plan`, with drift markers [^6] | none |
| Ordering | fixed by the recipe; `loop`/`register`/`when` [^8] | whatever order you type |
| Reviewability | a file you can diff, `fmt`, and `validate` offline [^6] | shell history or a README [^5] |
| Error handling | `failed_when`/`block`/`rescue`/`ignore_errors`/`--start-at-task` [^8][^6] | manual, per command |
| Remote execution | `--host` over SSH, `--sudo` [^11] | `ssh`/`dokku` by hand |
| Capture a live server | `docket export` [^6] | read `:report` output by hand [^1][^3] |

## Idempotency

A dokku command does what it says each time you run it. That is exactly what you want interactively, but
it puts re-run safety on the operator: re-running a create-style step is at best redundant and, depending
on the command, can error rather than silently succeed. docket frames its own value against precisely this
friction - after the first run "the second run sees the server already matches and does nothing", and
"there is no 'already exists' error to work around" [^5].

docket gets there by making every task check the live server before acting. Each task probes current
state once, compares it to the desired state, and only runs the underlying `dokku` command when they
differ [^9]. The observable result is that a second `apply` against an unchanged server changes nothing:
the first run reports each task as `[changed]`, the second reports every task as `[ok]` [^5].

```text
# first run
Summary: 2 tasks · 2 changed · 0 ok · 0 skipped · 0 errors

# second run
Summary: 2 tasks · 0 changed · 2 ok · 0 skipped · 0 errors
```

The commands are the same in both worlds; docket adds the check that decides whether to run them.

## Plan and preview

The dokku CLI has no dry run. To learn what a sequence of commands would do, you run them - or read each
plugin's `:report` output by hand and reason about the difference yourself [^1][^3].

docket makes that preview a first-class command. `docket plan` reads each task's current state from the
live server and reports what `apply` would change without running any mutating command, using drift
markers - `[ok]` in sync, `[+]` create, `[~]` modify, `[-]` remove, and `[!]` when the probe itself
errored [^6]. Because `plan` and `apply` run the same underlying probe - `apply` re-reads the server
within its own run - the two cannot disagree [^9], and the command strings `plan` says it would run are
byte-identical to the ones `apply` runs [^10]. For CI, `docket plan --detailed-exitcode` returns 0 for
no drift, 2 for drift, and 1 on error [^6].

## A declarative file versus shell history

With the CLI, the record of how an app was configured is whatever you kept - shell history, a README, a
setup script [^5]. It is prose or a command log, not something a tool can check.

A docket recipe is a reviewable artifact. It can be committed, diffed in a pull request, formatted
canonically with `docket fmt`, and checked offline with `docket validate` - which parses the file,
verifies each task's shape and required fields, and reports problems without contacting a server [^6].
The same file that documents the desired state is the one that enforces it, so the two cannot drift apart
the way a README and a server can.

## Ordering and repeatability

By hand, order is whatever you type, and reproducing a setup means re-typing or re-sourcing the same
commands in the same order. docket fixes the order in the recipe and runs it top to bottom [^7]. Where the
CLI would have you repeat a command per app, `loop:` expands one task over a list; `register:` saves a
task's result so a later task's `when:` can react to it; and `when:` gates a task on inputs or an earlier
result [^8]. The sequence is written once and reruns identically.

## Error handling

When a hand-run command fails, recovery is manual: read the error, decide whether it matters, fix, and
continue from the right place. docket carries an error-handling envelope on every task [^8]. `failed_when`
is the "this exit code is fine" idiom that turns an imperative command into an idempotent one - treating,
say, "not mounted" as success. `changed_when` overrides the changed verdict. `ignore_errors` continues
past a failed task. `block`/`rescue`/`always` give try/catch/finally over a group of tasks, so a deploy
can carry its own rollback path [^8]. And `--start-at-task` resumes a partially applied recipe from a
named task, with `--fail-fast` controlling whether an error aborts the whole run or only the current play
[^6].

## Remote execution and export

Driving a remote server with the bare CLI means an SSH session or dokku's own remote invocation, run by
hand. docket takes `--host <user@host:port>` to run the same recipe against a remote server over SSH, with
`--sudo` to wrap the remote call in `sudo -n` and `--accept-new-host-keys` for first connect - still
invoking `dokku` on the far side [^11].

docket also inverts the flow. `docket export` reads a live server and writes a recipe describing it, so an
existing server assembled with hand-run commands can be captured as a file rather than reconstructed from
memory; its correctness contract is that applying the export (together with its companion vars file) back
reports no drift [^6]. There is no CLI equivalent beyond reading each plugin's `:report` output yourself
[^1][^3].

## Scope: a front-end, not a replacement

docket is a declarative front-end for the same `dokku` subcommands, not a different way to run a server.
It requires dokku to be installed and resolves its tasks to real `dokku` commands [^5][^6], so anything
docket does, you could do by hand with the CLI - and anything the CLI can do that no docket task covers is
still done at the CLI. The choice is not docket or dokku; it is whether to run those dokku commands
directly or through a layer that adds a recipe, a plan, idempotency, and structured error handling. Direct
is simpler for a one-off; the layer earns its keep when the same setup has to be repeatable, reviewable,
and safe to re-run.

[^1]: Dokku, "Application management" (`apps:create`, `apps:destroy`, `apps:rename`, `apps:list`, `apps:report`) - https://dokku.com/docs/deployment/application-management/
[^2]: Dokku, "Environment variables" (`config:set`, `config:unset`, `config:show`) - https://dokku.com/docs/configuration/environment-variables/
[^3]: Dokku, "Domain configuration" (`domains:add`, `domains:set`, `domains:remove`, `domains:report`) - https://dokku.com/docs/configuration/domains/
[^4]: Dokku, "Git deployment" (`git:sync`) - https://dokku.com/docs/deployment/methods/git/
[^5]: docket, "Getting started" - [docs/getting-started.md](../getting-started.md)
[^6]: docket, "Command reference" (`plan`, `apply`, drift markers, `--detailed-exitcode`, `validate`, `fmt`, `export`) - [docs/command-reference.md](../command-reference.md)
[^7]: docket, "Recipes" (plays, tasks, and ordering) - [docs/recipes.md](../recipes.md)
[^8]: docket, "Task envelope" (`when`, `loop`, `register`, error handling) - [docs/task-envelope.md](../task-envelope.md)
[^9]: docket, "Writing tasks" (the `Plan()` / probe model) - [docs/writing-tasks.md](../writing-tasks.md)
[^10]: docket, "JSON output" (the `commands` arrays and their shared rendering) - [docs/json-output.md](../json-output.md)
[^11]: docket, "Remote execution" (`--host`, `--sudo`, `--accept-new-host-keys`) - [docs/remote-execution.md](../remote-execution.md)
