# plan/apply and idempotency: docket vs Ansible

docket and Ansible are close relatives, so this comparison reads differently from the one against
Terraform/OpenTofu. docket grew out of [`ansible-dokku`](https://github.com/dokku/ansible-dokku) and
exposes the same modules, so its task names look familiar and existing `ansible-dokku` task lists
migrate with minimal changes [^6][^10]. The task envelope - `when`, `loop`, `register`, `changed_when`,
`failed_when`, `ignore_errors`, `block`/`rescue`/`always`, `tags` - is taken almost verbatim from
Ansible [^5][^13], as is the `--start-at-task` flag [^11]. On the axes where docket looks unusual next
to Terraform (no state file, assertion-based deletion, ordered execution), it simply agrees with
Ansible.

Ansible and docket also share the same definition of the property in question. Ansible's glossary:
"an operation is idempotent if the result of performing it once is exactly the same as the result of
performing it repeatedly without any intervening actions" [^1]. And they share the same mechanism for
achieving it - each unit of work reads the live target and only acts if the state does not already
match. Neither keeps a persisted state file.

So the interesting question is not *whether* docket and Ansible differ on idempotency but *where docket
tightens the Ansible model*. Three places stand out: idempotency is enforced by construction rather than
left to module discipline; `plan` and `apply` are one guaranteed-consistent pair rather than a
per-module check mode; and the whole thing ships as a single Go binary scoped to Dokku instead of a
general-purpose Python control node. This document is neutral - Ansible's generality and docket's focus
serve different jobs - and it treats the two together where they agree.

## At a glance

| Dimension | docket | Ansible |
|-----------|--------|---------|
| Idempotency check | equality of desired vs current, per task [^14] | equality of desired vs current, per module [^1][^2] |
| Who guarantees it | the framework: every task is a probe-then-maybe-apply `Plan()` [^14] | the module author; "not all ... modules behave this way" [^2] |
| Persistent state | none [^14] | none [^2] |
| Source of "current state" | live per-task probe, every run [^14] | live per-module check, every run [^2] |
| Dry run | `plan` and `apply` share one `Plan()`; universal [^14] | check mode, per-module; unsupported modules "report nothing and do nothing" [^3] |
| Deleting a resource removed from the file | not deleted; explicit `state: absent` [^14] | not deleted; explicit `state: absent` |
| Execution model | ordered plays then tasks, single target [^12] | ordered plays then tasks, across an inventory [^2] |
| Task envelope | `when`/`loop`/`register`/`changed_when`/`failed_when`/`ignore_errors`/`block`/`rescue`/`always` [^13] | the same keys; docket's source [^5][^13] |
| Templating | sigil `{{ .app }}` bodies, expr predicates [^13] | Jinja2 `{{ }}` throughout [^8] |
| Runtime and scope | single Go binary, Dokku only [^10] | Python control node + inventory, general purpose |

## Idempotency mechanism

Ansible and docket both converge by having each unit of work check the live target and skip when it
already matches. Ansible states it plainly: "most Ansible modules check whether the desired final state
has already been achieved and exit without performing any actions if that state has been achieved ...
modules that behave this way are 'idempotent'" [^2]. The important qualifier is the next sentence:
"whether you run a playbook once or multiple times, the outcome should be the same. However, not all
playbooks and not all modules behave this way" [^2]. Idempotency in Ansible is a per-module convention,
not a framework guarantee. The classic exception is the `command` module (and `shell`), which runs its
command on every pass unless you guard it - with `creates`/`removes` so a matching file means "this
step will not be run", or with `changed_when`/`when` [^4].

docket removes the discretion. Every task implements a `Plan()` that probes the live server once,
computes current versus desired, and either reports it is in sync (and does nothing) or returns a
closure that performs the mutation; `apply` is defined as executing that plan [^14]. Idempotency is a
property of the task model itself rather than of how carefully each task was written. There is no
general "run this arbitrary command" task acting as an unguarded escape hatch. The escape hatches that
remain are narrow and documented: a few tasks cannot read their own state (notably `dokku_git_auth`,
`dokku_registry_auth`, and `dokku_storage_ensure`) and so always apply [^11], and some dynamic property
families skip probing [^14] - the same "this module cannot honestly dry-run" situation Ansible has, but
enumerated rather than open-ended.

The practical difference: in Ansible "is this task idempotent?" is a question you ask per module and per
author; in docket it is answered by the type of thing a task is.

## Statelessness

Both tools are stateless in the sense that matters here: neither keeps a persisted record of what it
manages. Ansible determines what to do by having each module check the live target's current state [^2];
docket does the same through each task's `Plan()` probe [^14]. This is the axis where docket looks
nothing like Terraform/OpenTofu, whose state file exists precisely to "map real world resources to your
configuration" [^9] - and exactly like Ansible.

The shared consequences are the same ones the Ansible ecosystem lives with: no state file to store,
lock, back up, or reconcile, and no way for a stored record to drift from reality; but also no built-in
inventory of managed resources, and the cost of reading live state on every run. docket's `export` -
reading a live server and writing a recipe that reapplies with no drift [^11] - is the nearest thing to
a snapshot, and it is a command you invoke rather than a file the tool maintains.

## plan/apply versus check mode

This is where docket most clearly tightens the Ansible model. Ansible's dry run is check mode:
"in check mode, Ansible runs without making any changes on remote systems" [^3]. But check mode inherits
the same per-module discretion as idempotency: "modules that support check mode report the changes they
would have made. Modules that do not support check mode report nothing and do nothing" [^3]. `--diff`
similarly only reports for "any module that supports diff mode" [^3]. So an Ansible dry run is as complete
as the modules in the play, and a play that leans on `command`/`shell` can dry-run as a no-op that tells
you little.

docket makes dry run structural. `plan` and `apply` call the *same* `Plan()` method - `apply` re-probes
within its own run and reuses that read to decide whether to mutate - so the two cannot disagree and
every task can be planned [^14]. The command strings `plan` says it *would* run and `apply` says it
*did* run are rendered by identical code and are byte-identical [^15]. `plan` reports drift with `[ok]`,
`[+]`, `[~]`, `[-]`, and `[!]` (probe error), and `docket plan --detailed-exitcode` returns 0 for no
drift, 2 for drift, and 1 on error [^11]. Where Ansible offers a per-module preview, docket offers a
whole-recipe one with a matching exit-code contract for CI.

## Deletion and assertion semantics

Here docket and Ansible agree, and both differ from Terraform. In neither tool is the file a complete
inventory whose members get pruned. Each task or module is an *assertion* about one resource, and
removing it from the file does nothing to the target. Deletion is requested explicitly, with the same
idiom in both: `state: absent` [^14]. The upside is that you cannot destroy something by deleting or
forgetting a block; the downside is that neither tool cleans up orphans for you - anything you want gone
must be named with `state: absent`.

## Execution model

Both run in source order with no dependency graph. Ansible: "a playbook runs in order from top to
bottom. Within each play, tasks also run in order from top to bottom" [^2]. docket keeps the same play
then task ordering it inherited, with `loop:` expanding a task over a list and `register:` passing one
task's result to a later task's condition [^13]. Neither derives an order from resource references the
way Terraform's graph does; you write the order.

The difference is the target axis. An Ansible play targets an inventory of managed nodes and runs each
task across all of them, using forks as its concurrency model, so one task fans out over many hosts
[^7]. docket drives a single Dokku target - the local CLI or one host over SSH - so its ordering is
purely the task sequence, with no host fan-out [^16]. Ansible's parallelism is across hosts; docket has
none to speak of.

## The task envelope and templating

docket's error handling and flow control are Ansible's, deliberately. Ansible defines `ignore_errors` to
"continue despite of the failure", `failed_when` to "define what 'failure' means in each task",
`changed_when` to "define when a particular task has 'changed' a remote node", and blocks to "define
responses to task errors" via `rescue`/`always` [^5]. docket carries the same keys with the same meanings:
`failed_when` as the "this exit code is fine" idiom that makes an imperative call idempotent,
`changed_when` to rewrite the changed verdict, `ignore_errors` to continue past an error, and
`block`/`rescue`/`always` as try/catch/finally over a group [^13], plus `--start-at-task` to resume and
`--fail-fast` to widen the abort scope [^11]. If you know Ansible's envelope, you know docket's.

Templating is the one surface that looks similar but is engineered differently. Ansible uses Jinja2 for
both interpolation and conditionals [^8]. docket splits the two: task bodies use sigil templates
(`{{ .app }}`) and envelope predicates use the expr language (`app == "api"`), kept in separate places
so they are not confused [^13]. The `{{ }}` shape is familiar from Ansible, but the engines and the
split are docket's own, and docket pairs them with offline typed validation (`docket validate`) and a
canonical formatter (`docket fmt`) - checks that in the Ansible world live in separate tools like
`ansible-lint` rather than in the core run [^11].

## Runtime and scope

The last difference is operational rather than conceptual. Ansible is a general-purpose automation
engine: a Python control node, an inventory, connection plumbing, and a large module ecosystem that can
manage almost any host. docket is a single Go binary with no Ansible installation required [^10], scoped
to one job - declaring the state of a Dokku app - driving the local `dokku` CLI or one server over SSH
[^16]. It trades Ansible's breadth for a focused tool with typed tasks, a real plan/apply, and offline
validation, while keeping the parts of Ansible - the envelope, statelessness, assertion semantics,
ordered execution - that already fit the job [^13][^14]. For an existing `ansible-dokku` user, that is
the migration story: the same task vocabulary, minus the Ansible runtime, plus a plan.

[^1]: Ansible, "Glossary" (idempotency definition) - https://docs.ansible.com/projects/ansible/latest/reference_appendices/glossary.html
[^2]: Ansible, "Ansible playbooks" (ordered execution, idempotency as a module property) - https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_intro.html
[^3]: Ansible, "Validating tasks: check mode and diff mode" - https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_checkmode.html
[^4]: Ansible, "ansible.builtin.command module" (`creates`/`removes` guards) - https://docs.ansible.com/projects/ansible/latest/collections/ansible/builtin/command_module.html
[^5]: Ansible, "Error handling in playbooks" - https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_error_handling.html
[^6]: `ansible-dokku` (docket's direct ancestor) - https://github.com/dokku/ansible-dokku
[^7]: Ansible, "Controlling playbook execution: strategies and more" (forks, per-task host fan-out) - https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_strategies.html
[^8]: Ansible, "Templating (Jinja2)" - https://docs.ansible.com/projects/ansible/latest/playbook_guide/playbooks_templating.html
[^9]: HashiCorp, "State" - https://developer.hashicorp.com/terraform/language/state
[^10]: docket, "Getting started" (`ansible-dokku` lineage, single binary) - [docs/getting-started.md](../getting-started.md)
[^11]: docket, "Command reference" (`plan`, `apply`, drift markers, `--detailed-exitcode`, `validate`, `fmt`, `export`) - [docs/command-reference.md](../command-reference.md)
[^12]: docket, "Recipes" (plays, tasks, and ordering) - [docs/recipes.md](../recipes.md)
[^13]: docket, "Task envelope" (`when`, `loop`, `register`, error handling, templating split) - [docs/task-envelope.md](../task-envelope.md)
[^14]: docket, "Writing tasks" (the `Plan()` / probe model) - [docs/writing-tasks.md](../writing-tasks.md)
[^15]: docket, "JSON output" (the `commands` arrays and their shared rendering) - [docs/json-output.md](../json-output.md)
[^16]: docket, "Remote execution" (one host over SSH) - [docs/remote-execution.md](../remote-execution.md)
