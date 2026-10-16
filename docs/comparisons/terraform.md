# plan/apply and idempotency: docket vs Terraform/OpenTofu

Both docket and Terraform/OpenTofu are declarative infrastructure tools: you describe the state you
want, the tool figures out the commands to get there, and running the same description twice is safe.
Karim's post "Idempotency in IaC" argues that this safety reduces to something simple - idempotency
in IaC is "just an equality check" between the state you declared and the state that already exists,
and when the two are equal the tool does nothing [^1]. docket accepts that same premise [^8].

Where the two tools diverge is in one design decision: *where the "current state" side of that
equality check comes from*. Terraform and OpenTofu read the real world once, record it in a persisted
**state file**, and reconcile your configuration against that file [^3][^4]. docket keeps no state file
at all - each task reads the live server every run and compares against that fresh read [^8]. That
single difference is the root of almost every other difference below: how deletion works, how drift
is handled, how execution is ordered, and how failures are caught.

This document treats Terraform and OpenTofu together, because they share the plan/apply/state model
described here; the post that prompted this comparison uses OpenTofu specifically [^1]. It is a neutral
comparison - each section states what each tool does and the trade-off, not which is better. The
approaches fit different problems: Terraform/OpenTofu manage a closed world of cloud resources through
provider APIs; docket converges a Dokku server through `dokku` commands.

## At a glance

| Dimension | docket | Terraform/OpenTofu |
|-----------|--------|--------------------|
| Idempotency check | equality of desired vs current [^8] | equality of desired vs current [^1] |
| Source of "current state" | live per-task probe, every run [^8] | state file, refreshed from providers [^3][^4] |
| Persistent state | none | `terraform.tfstate` (+ backends, locking) [^3] |
| Deleting a resource removed from config | not deleted; delete is explicit via `state: absent` [^8] | destroyed on next apply [^3] |
| Execution model | ordered plays then ordered tasks (Ansible lineage) [^11] | dependency graph, auto-ordered and parallelized [^5] |
| plan and apply agreement | same `Plan()`, re-probed at apply [^8] | saved plan (`-out`) applied exactly, or re-plan [^2] |
| Error handling | `failed_when`, `block`/`rescue`/`always`, `ignore_errors` [^12] | lifecycle rules, provider retries [^6] |
| Drift limit | unprobeable tasks over-apply (safe, not a no-op) [^9] | dishonest providers cause permanent diffs [^1] |

## Idempotency mechanism

The post walks OpenTofu's decision as refresh, then merge, then decide, then apply: it reads the
current object, merges it with your configuration into a proposed object, and compares. The comparison
is the whole story - when `unmarkedPlannedNewVal.Equals(unmarkedPriorVal)` holds, the action becomes a
NoOp and the provider's update method never runs [^1]. "Idempotency in IaC is just an equality check"
is the post's way of saying the machinery around that comparison is incidental; the comparison is what
makes a second run safe [^1].

docket lands on the same equality check, but performs it per task against a live read rather than
against a stored prior. Every task implements a `Plan()` method that probes the live server once,
computes the difference between current and desired state, and either reports it is already in sync
(and does nothing) or embeds a closure that performs the mutation [^8]. `apply` is defined as executing
that same plan, so an in-sync task is a no-op for exactly the reason the post describes: the compared
values are equal.

The observable result is identical to Terraform's. The first `apply` reports each task as `[changed]`;
a second `apply` against an unchanged server reports every task as `[ok]` and mutates nothing [^10]:

```text
# first run
Summary: 2 tasks · 2 changed · 0 ok · 0 skipped · 0 errors

# second run
Summary: 2 tasks · 0 changed · 2 ok · 0 skipped · 0 errors
```

The difference is not *whether* there is an equality check but *what it compares against*: a value
merged into a persisted state object [^1], versus a value read live from the server on this run [^8].

## State: none versus a state file

Terraform and OpenTofu need a state file. Its "primary purpose is to store bindings between objects in
a remote system and resource instances declared in your configuration"; it exists to "map real world
resources to your configuration, keep track of metadata, and to improve performance for large
infrastructures" [^3][^4]. That file is real infrastructure of its own: it must be stored in a backend,
locked so two applies do not race, refreshed to catch out-of-band changes, and edited through
dedicated commands (`terraform import`, `terraform state`) when reality and the file diverge. Secret
values that pass through resources are recorded in it [^7].

docket has no state file. The live probe is the source of truth on every run, so there is nothing to
store, lock, refresh, or import, and no way for a state file to drift from the server it describes [^8].
The trade-off is the flip side of the same coin: docket has no cheap ledger of "what do I manage", it
pays the probe cost on every plan and apply, and a resource it cannot read back cannot be reconciled at
all. The closest analog to Terraform's state snapshot or `import` is `docket export`, which reads a live
server and writes a recipe describing it; its correctness contract is idempotency - applying an exported
recipe (together with its companion vars file) back to the same server reports no drift [^9].

## plan and apply symmetry

Terraform's `plan` refreshes state, compares the configuration to the prior state, and proposes the set
of create/update/delete actions that would make the world match the configuration [^2]. That plan can be
saved with `-out` and handed to `apply` so the applied changes are exactly the ones shown, or `apply`
can compute a fresh plan itself. `terraform plan -detailed-exitcode` returns 0 for an empty diff, 2 for
a non-empty diff, and 1 for an error [^2].

docket guarantees plan/apply agreement structurally: both commands call the same `Plan()`, and `apply`
reuses the probe that `plan` performed to decide whether to mutate [^8]. There is no saved-plan artifact -
docket re-probes at apply time within the run rather than replaying a file. The command strings that
`plan` says it *would* run and that `apply` says it *did* run are rendered by the same code and are
byte-identical [^13]. `plan` reports drift with a small marker set - `[ok]` in sync, `[+]` create, `[~]`
modify, `[-]` remove, and `[!]` when the probe itself errored - and `docket plan --detailed-exitcode`
deliberately mirrors Terraform: 0 for no drift, 2 for drift, 1 on error, with errors winning over drift
[^9].

## Deletion and pruning semantics

This is the sharpest divergence, and it follows directly from the state decision. Because Terraform's
state file records every object it created against a resource instance, the configuration is the
*complete* desired state of everything Terraform manages. When Terraform "creates a remote object in
response to a change of configuration, it records the identity of that remote object ... and potentially
updates or deletes that object in response to future configuration changes" [^3]. Remove a resource block
from the configuration and the next apply destroys the object, because state still lists it as managed
while the configuration no longer declares it.

docket has no such managed set. Each task is an *assertion* about one resource, not membership in a
global inventory, so removing a task from a recipe does nothing to the server - docket only ever acts on
resources you name. Deletion is explicit: a task requests it with `state: absent` [^8]. The consequence
cuts both ways. You cannot accidentally destroy a resource by deleting or forgetting a block, but docket
also will not clean up orphaned resources for you; anything you want gone must be named with `state:
absent`.

## Ordered tasks versus a dependency graph

Terraform builds a dependency graph from the references between resources and uses it to order
operations and to parallelize independent ones automatically [^5]; you generally do not specify order,
you express dependencies and Terraform derives the schedule.

docket runs in source order. It grew out of `ansible-dokku` and keeps the Ansible shape: a recipe is a
list of plays, each play a list of tasks, executed top to bottom [^11]. Ordering is whatever you write.
`loop:` expands one task into one copy per list item, and `register:` saves a task's result so a later
task's condition can react to it - for example, stamping a config value only on the run that first
created an app [^12]. There is no automatic dependency graph and no automatic parallelism. The trade-off
is explicitness and predictability against the convenience of a scheduler that orders and parallelizes
for you.

## Error handling and retries

Terraform's error handling lives mostly in resource `lifecycle` rules - `create_before_destroy`,
`prevent_destroy`, `ignore_changes` [^6] - and in provider-level retry logic; a failed apply leaves
partial state that the next run reconciles.

docket carries an Ansible-style error envelope on every task [^12]. `failed_when` is the standard "this
exit code is fine" idiom that turns an imperative command into an idempotent one - for example, treating
"not mounted" as success when unmounting - and a falsy predicate fully clears the failure. `changed_when`
overrides the changed verdict. `ignore_errors: true` lets the run continue past a task that errored.
`block` / `rescue` / `always` give a try / catch / finally over a group of tasks, so a deploy can carry
its own rollback path. And `--start-at-task` resumes a partially applied recipe from a named task, while
`--fail-fast` controls whether an error aborts the whole run or only the current play [^9].

## Drift and "honest providers"

The post names the limit of the equality-check model directly: "OpenTofu can only be as honest as its
providers." If a provider misreports a value during refresh, the compared values never converge and you
get a permanent diff - the tool keeps trying to "fix" state that is already correct, and idempotency
breaks [^1].

docket has the same class of limit, because its equality check is only as honest as each task's probe,
but it degrades differently. A few tasks - notably `dokku_git_auth`, `dokku_registry_auth`, and
`dokku_storage_ensure` - cannot read their own state without running the underlying command, so they
always report drift with a `(... not probed)` reason and apply unconditionally [^9]. Some property
families whose report key only appears after the value is set, such as letsencrypt's `dns-provider-*`,
skip probing for the same reason [^8]. And `dokku_plugin` is idempotent by plugin name only, so a
changed URL or committish on an already-installed plugin is not detected [^14]. When the probe command
cannot run at all - the `dokku` CLI is missing, or the SSH host is unreachable - an existence probe
renders `[!]` and `plan` exits 1 rather than optimistically predicting `[+] create` for state it never
read [^9]; a property read that fails short of a transport error degrades the other way, reporting
drift and re-applying the value. Either way, docket does not silently assume the server matches.

The shapes of the two failure modes differ. When current state cannot be read honestly, Terraform tends
toward a *permanent diff* - a change it proposes forever [^1] - while docket tends toward *over-applying*:
it re-runs the command it could not prove unnecessary [^9]. docket's outcome is safe rather than a true
no-op, so idempotency degrades to "runs again" rather than "diffs forever". Both are bounded by the same
underlying constraint the post identifies: the equality check is only as good as the read of current
state feeding it.

[^1]: Karim, "Idempotency in IaC" - https://www.spamsbykarim.com/blog/idempotency-in-iac (primary source)
[^2]: HashiCorp, "terraform plan command reference" - https://developer.hashicorp.com/terraform/cli/commands/plan
[^3]: HashiCorp, "State" - https://developer.hashicorp.com/terraform/language/state
[^4]: OpenTofu, "State" - https://opentofu.org/docs/language/state/
[^5]: HashiCorp, "Resource graph" (dependency ordering and parallel graph walks) - https://developer.hashicorp.com/terraform/internals/graph
[^6]: HashiCorp, "The lifecycle meta-argument" (`create_before_destroy`, `prevent_destroy`, `ignore_changes`) - https://developer.hashicorp.com/terraform/language/meta-arguments/lifecycle
[^7]: HashiCorp, "Sensitive data in state" - https://developer.hashicorp.com/terraform/language/state/sensitive-data
[^8]: docket, "Writing tasks" (the `Plan()` / probe model) - [docs/writing-tasks.md](../writing-tasks.md)
[^9]: docket, "Command reference" (`plan`, `apply`, drift markers, `--detailed-exitcode`, `export`) - [docs/command-reference.md](../command-reference.md)
[^10]: docket, "Getting started" - [docs/getting-started.md](../getting-started.md)
[^11]: docket, "Recipes" (plays, tasks, and ordering) - [docs/recipes.md](../recipes.md)
[^12]: docket, "Task envelope" (`when`, `loop`, `register`, error handling) - [docs/task-envelope.md](../task-envelope.md)
[^13]: docket, "JSON output" (the `commands` arrays and their shared rendering) - [docs/json-output.md](../json-output.md)
[^14]: docket, "dokku_plugin" - [docs/tasks/dokku_plugin.md](../tasks/dokku_plugin.md)
