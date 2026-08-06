# plan/apply and idempotency: docket vs Chef, Puppet, and SaltStack

Chef, Puppet, and SaltStack are the general-purpose configuration-management systems in the same
idempotency lineage as Ansible - and therefore as docket, which grew out of `ansible-dokku`. Ansible
gets its own comparison in [`ansible.md`](ansible.md); this document covers the
other three together, because they share a model and differ from docket in the same ways. All three
converge by resource-level idempotency: a manifest, recipe, or state file declares resources, each
resource type checks the node's current state, and it acts only when current and desired differ [^2][^6].
docket rests on the same idea, one task at a time [^12].

Because the shared premise is identical, the interesting question is again *where docket tightens the
model* rather than whether it disagrees. Three differences stand out, and they are the same three that
set docket apart from Ansible. First, idempotency is enforced by construction: every docket task is a
probe-then-maybe-apply `Plan()`, rather than a convention each resource-type author must uphold [^12].
Second, dry run is a guaranteed-consistent `plan`/`apply` pair built from that same `Plan()` [^12],
rather than a tool-specific preview mode - Puppet `--noop` [^2], Salt `test=True` [^6], Chef
`--why-run` [^4] - whose fidelity varies by resource, most sharply in Chef, where the vendor itself
calls why-run "by definition incomplete" [^5]. Third, docket is an agentless single Go binary scoped to
Dokku [^16], rather than a server-plus-agent system (Puppet primary/agent, Chef server/client, Salt
master/minion) with masterless fallbacks [^4][^9][^10][^11].

This is a neutral comparison. These three are broad systems that manage almost any resource on almost
any host; docket manages one thing, a Dokku app, and trades breadth for a focused tool with typed
tasks, a real plan, and offline validation. Where they behave alike - resource idempotency, reconverge
from live state each run, explicit deletion - the document says so.

## At a glance

| Dimension | docket | Chef / Puppet / Salt |
|-----------|--------|----------------------|
| Idempotency check | equality of desired vs current, per task [^12] | resource-level convergence: each resource/state type checks current vs desired [^2][^6] |
| Who guarantees it | the framework: every task is a probe-then-maybe-apply `Plan()` [^12] | the resource-type author, per type; fidelity varies |
| Persistent prior-state file | none [^12] | none in the Terraform sense; each run reconverges from live state [^2][^6] |
| Dry run | `plan` and `apply` share one `Plan()`; universal, byte-identical [^12][^17] | Puppet `--noop` [^2], Salt `test=True` [^6], Chef `--why-run` - "by definition incomplete" [^4][^5] |
| Deleting a resource removed from the file | not deleted; explicit `state: absent` [^12] | explicit `ensure => absent` per resource; Puppet adds optional `purge` to prune unmanaged [^3] |
| Execution model | ordered plays then tasks [^14] | Puppet relationship graph (manifest order + relationships/autorequire) [^1]; Salt deterministic SLS order + requisites [^7]; Chef recipe order [^8] |
| Architecture | agentless single binary, one target [^16][^18] | typically server + agent pull (Puppet primary/agent, Chef server/client, Salt master/minion); masterless variants exist [^4][^9][^10][^11] |
| Authoring | YAML/JSON5 recipes, sigil `{{ }}` + expr [^14][^15] | Puppet DSL; Chef Ruby recipes; Salt YAML + Jinja |
| Scope and runtime | Dokku only, Go binary [^16] | general-purpose configuration management |

## Idempotency mechanism

All three systems converge the same way docket does: each unit of desired state carries the logic to
inspect the node and skip when it already matches. In Puppet a resource is applied only when it is not
already in sync - the `noop` metaparameter makes this explicit: Puppet "checks whether the resource is
in the desired state as declared in the catalog" and, if it is not, "takes no action, but reports the
changes it would have made" [^2]. Salt reports a state as changed or already-satisfied per state
function, and its test mode surfaces exactly the states "that will be applied" [^6]. Chef's model is a
two-phase compile-then-converge over a resource collection, where each resource's provider decides
whether action is needed [^8]. In every case idempotency is a property each resource type must
implement correctly, and its quality varies across the ecosystem - the same per-unit convention Ansible
has.

docket removes the discretion in the same way it does versus Ansible: every task implements a `Plan()`
that probes the live server once, computes current versus desired, and either reports it is in sync (and
does nothing) or returns a closure that performs the mutation, with `apply` defined as executing that
plan [^12]. Idempotency is a property of the task model, not of how carefully each task was written, and
the exceptions are narrow and documented - a few tasks that cannot read their own state and so always
apply [^13]. The practical difference is the one that recurs throughout this family: in Chef, Puppet, and
Salt "is this resource idempotent, and does it dry-run faithfully?" is asked per resource type; in
docket it is answered by the task model itself.

## Dry run: plan versus noop, test, and why-run

Each of the three ships a dry-run mode, and this is where they diverge from docket most visibly. Puppet
runs in noop mode, where it checks whether each resource is in the desired state and, for anything out
of state, "takes no action, but reports the changes it would have made" [^2]. Salt adds `test=True` to
any state run: "the return information will show states that will be applied ... and the result is
reported as `None`" [^6]. Chef offers why-run mode via `-W` / `--why-run`, "a way to see what Chef
Infra Client would have configured, had an actual Chef Infra Client run occurred" [^4].

The catch is fidelity, and it is not uniform. Puppet's and Salt's previews reuse the same in-sync checks
the real run performs, so they are generally faithful [^2][^6]. Chef's is not: Chef's own guidance - a
post titled "Why 'Why-Run' Mode Is Considered Harmful" - calls why-run "by definition incomplete", warns
that it "in many non-trivial situations will give incorrect results, thereby creating a false sense of
security", and steers users to Test Kitchen and InSpec instead [^5]. So across this family a dry run is
only as trustworthy as each resource type's support for it - the same per-resource discretion seen in
idempotency.

docket makes dry run structural rather than a mode. `plan` and `apply` call the *same* `Plan()`, and
`apply` reuses the probe within its own run, so the two cannot disagree and every task is plannable
[^12]. The command strings `plan` says it *would* run and `apply` says it *did* run are rendered by
identical code and are byte-identical [^17]. `plan` reports drift with `[ok]`, `[+]`, `[~]`, `[-]`, and
`[!]` (probe error), and `docket plan --detailed-exitcode` returns 0 for no drift, 2 for drift, and 1 on
error [^13]. There is no separate preview engine that can drift from the real run.

## Statelessness and architecture

Like Ansible and docket, none of the three keep a Terraform-style persisted file of prior values that
must be reconciled and locked; each run reads the node's live state and reconverges from it [^2][^6].
What they add is a control plane. Puppet, Chef, and Salt are typically server-plus-agent systems: a
Puppet agent pulls a compiled catalog from a primary server, a chef-client pulls run-lists and cookbooks
from a Chef Infra Server, and Salt minions take instructions from a Salt master. Each also offers a
masterless mode for local runs - `puppet apply` [^9], `chef-solo` or local-mode `chef-client` [^4][^10],
and `salt-call --local` [^11] - but the server model is the common deployment.

docket has no control plane at all. It is a single Go binary that drives one Dokku target, the local
`dokku` CLI or one host over SSH [^16][^18], with nothing installed on the target beyond Dokku itself.
The trade-off is the usual one: docket gives up fleet management, node inventories, and scheduled pull
runs across many machines, in exchange for no agents to install, no server to run, and no catalog
compilation step. `docket export` - reading a live server and writing a recipe that reapplies with no
drift - is the nearest thing to a stored snapshot, and it is a command you invoke, not state a server
maintains [^13].

## Deletion and pruning

Deletion is explicit in all four tools by the same idiom: a resource declared `ensure => absent` (Puppet
and Salt spellings vary; docket uses `state: absent`) is removed, and simply *deleting* a resource from
the manifest does not remove it from the node [^12]. On its own, that matches docket and Ansible - the
file is a set of assertions, not a complete inventory that gets pruned.

Puppet is the exception worth naming, because it offers optional set-pruning that docket does not. The
`resources` metatype's `purge` attribute will "delete any resource that is not specified in your
configuration and is not autorequired by any managed resources" [^3], which brings the Terraform-style
"remove it from config and it gets destroyed" behavior to a whole resource type when you opt in. docket
has no equivalent: it acts only on resources you name, and anything you want gone must be named with
`state: absent` [^12]. The trade-off is the same as everywhere else in this axis - Puppet's purge can
keep a node clean of drift automatically but can also delete something you simply forgot to declare,
while docket cannot prune for you but also cannot surprise you.

## Execution and ordering

Within this family, ordering models split, and Puppet is the one that looks like Terraform. Puppet
applies "resources ... in the order they are defined in their manifest, but only if the resource has no
implicit relationship with another resource"; explicit relationships via metaparameters (`before`,
`require`, `notify`, `subscribe`) and chaining arrows (`->`, `~>`), plus automatic ones through
`autorequire` and friends, build an ordering that is "not limited by evaluation-order" [^1]. That is a
dependency graph, much like Terraform's. Salt "always executes states in a deterministic manner", in the
order defined in the SLS unless requisites (`require`, `watch`) or the `order` option intervene [^7].
Chef evaluates resources in the order they appear in a recipe and converges them in that order [^8].

docket runs in source order: ordered plays, then ordered tasks [^14], with `loop:` expanding a task over
a list and `register:` passing one task's result to a later task's condition [^15]. It has no dependency
graph and no autorequire; you write the order, as you do in Chef and in un-related Puppet resources. So
on ordering docket sits with Chef and default-Salt on the explicit-order side, and Puppet's relationship
graph is the outlier that most resembles the Terraform comparison.

## Authoring and scope

The three systems each have their own authoring surface: Puppet a purpose-built declarative DSL, Chef
Ruby recipes backed by a resource DSL, and Salt YAML templated with Jinja. docket recipes are YAML or
JSON5 [^14], with task bodies interpolated by sigil templates (`{{ .app }}`) and envelope predicates
written in the expr language (`app == "api"`), kept separate so the two are not confused [^15]. docket
also pairs authoring with offline typed validation (`docket validate`) and a canonical formatter
(`docket fmt`), catching a malformed recipe before any server is touched [^13] - checks that in this
ecosystem tend to live in adjacent tools (`puppet parser validate`, cookbook linting, Salt's render
checks) rather than in one built-in command.

The last difference is scope, and it frames all the others. Chef, Puppet, and Salt are general-purpose
engines that manage packages, files, services, users, and much more across large fleets. docket manages
one thing - the declarative state of a Dokku app - and keeps only the parts of the configuration-
management model that fit that job: resource idempotency, reconverge-from-live-state, explicit deletion,
and ordered execution [^12][^14]. It adds a guaranteed plan/apply and offline validation on top, and
leaves the control plane out.

[^1]: Puppet, "Relationships and ordering" - https://www.puppet.com/docs/puppet/7/lang_relationships.html
[^2]: Puppet, "Metaparameters" (the `noop` metaparameter) - https://www.puppet.com/docs/puppet/7/metaparameter.html
[^3]: Puppet, "Resource type: resources" (the `purge` attribute) - https://www.puppet.com/docs/puppet/7/types/resources.html
[^4]: Chef, "Chef Infra Client (executable)" (why-run mode, `-W` / `--why-run`, local mode) - https://docs.chef.io/client/19/reference/ctl_chef_client/
[^5]: Chef, "Why 'Why-Run' Mode Is Considered Harmful" - https://www.chef.io/blog/why-why-run-mode-is-considered-harmful
[^6]: Salt Project, "State Testing" (`test=True`) - https://docs.saltproject.io/en/latest/ref/states/testing.html
[^7]: Salt Project, "Ordering States" - https://docs.saltproject.io/en/latest/ref/states/ordering.html
[^8]: Chef, "Chef Infra overview" (compile then converge over a resource collection) - https://docs.chef.io/client/19/overview/chef_overview/
[^9]: Puppet, "puppet apply man page" (standalone, serverless runs) - https://www.puppet.com/docs/puppet/7/man/apply.html
[^10]: Chef, "chef-solo" (running without a Chef Infra Server) - https://docs.chef.io/client/19/features/chef_solo/
[^11]: Salt Project, "Standalone Minion" (`salt-call --local`) - https://docs.saltproject.io/en/latest/topics/tutorials/standalone_minion.html
[^12]: docket, "Writing tasks" (the `Plan()` / probe model, `state` fields) - [docs/writing-tasks.md](../writing-tasks.md)
[^13]: docket, "Command reference" (`plan`, `apply`, drift markers, `--detailed-exitcode`, `validate`, `fmt`, `export`) - [docs/command-reference.md](../command-reference.md)
[^14]: docket, "Recipes" (plays, tasks, ordering, YAML/JSON5) - [docs/recipes.md](../recipes.md)
[^15]: docket, "Task envelope" (`loop`, `register`, templating split) - [docs/task-envelope.md](../task-envelope.md)
[^16]: docket, "Getting started" (single binary, scope) - [docs/getting-started.md](../getting-started.md)
[^17]: docket, "JSON output" (the `commands` arrays and their shared rendering) - [docs/json-output.md](../json-output.md)
[^18]: docket, "Remote execution" (one host over SSH) - [docs/remote-execution.md](../remote-execution.md)
