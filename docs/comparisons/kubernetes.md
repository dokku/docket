# plan/apply and idempotency: docket vs Kubernetes (kubectl apply and GitOps)

docket and Kubernetes both start from declarative desired state: you write down what you want, and the
tool works out the changes needed to get there. `kubectl apply` merges your manifest into the live
object and changes only what differs [^1], and `kubectl diff` can show "the current online
configuration, and the configuration as it would be if applied" before you commit to it [^3]. Applying
the same configuration again converges to the same result [^1]. docket does the same thing per task:
each task probes the live server once, compares current against desired, and only mutates when they
differ [^11]. On the narrow question the blog "Idempotency in IaC" asks - is a re-run safe - the two
agree.

They part company on *when* and *for how long* that convergence happens, and this is the sharpest model
contrast in this whole set of comparisons. Kubernetes is a **continuous reconciliation** system. Your
applied objects are persisted in the cluster's datastore (etcd) [^8], and controllers work forever to
drive actual state toward that stored desired state [^9]. GitOps operators layer another loop on top:
Argo CD "has the ability to automatically sync an application when it detects differences between the
desired manifests in Git, and the live state in the cluster" [^4], and Flux reconciles on an interval so
that "if you make any changes to the cluster using kubectl edit/patch/delete, they will be promptly
reverted" [^5]. Convergence is a standing background process.

docket is a **one-shot converge**. It has no controller, no persisted desired state, and no background
loop. It runs when you run it, reads the live server on that run, makes the changes, and exits [^11].
Drift that appears an hour later is invisible until the next time you run `docket plan` or `docket
apply`.

This document is neutral. Kubernetes and docket target very different scales - a cluster of many
workloads reconciled continuously versus a single Dokku host converged on demand - and neither model is
"better" in the abstract; they answer different questions. The sections below map where they line up and
where they diverge.

## At a glance

| Dimension | docket | Kubernetes / GitOps |
|-----------|--------|---------------------|
| Idempotency check | equality of desired vs current, per task [^11] | declarative merge of intent into live objects [^1][^2] |
| Source of "current state" | live per-task probe, every run [^11] | the cluster datastore (etcd), read by kubectl or a controller [^1][^8] |
| Persistent desired state | none [^11] | yes - applied objects stored in the cluster, tracked by `last-applied-configuration` / `managedFields` [^1][^2] |
| When convergence happens | once, when you run `apply` [^11] | continuously - controllers reconcile forever; GitOps re-syncs on an interval [^5][^9] |
| Dry run / preview | `plan` and `apply` share one `Plan()`; universal [^11] | `kubectl diff` (server-side dry run); Argo CD / Flux diff views [^3][^4] |
| Drift correction | only on your next `plan` / `apply` [^12] | automatic and continuous (Argo CD self-heal, Flux revert) [^4][^5] |
| Deleting a resource removed from the file | not deleted; explicit `state: absent` [^11] | pruned - `apply --prune`, Argo CD auto-prune, Flux garbage collection [^7][^4][^6] |
| Execution model | ordered recipe, single Dokku target [^13] | many controllers reconciling in parallel across a cluster [^9][^5] |
| Runtime | single Go binary [^15] | a cluster: API server, datastore, controllers (plus a GitOps operator) [^8][^5] |

## Idempotency mechanism

Both tools converge by comparing desired state to live state and acting only on the difference.
`kubectl apply` merges your "fully specified intent" into the existing object and changes only what
differs, using a recorded baseline - historically the `kubectl.kubernetes.io/last-applied-configuration`
annotation [^1], and with Server-Side Apply a `managedFields` record where "the Kubernetes API server
tracks managed fields for all newly created objects" and moves ownership of a field to whichever manager
last changed it [^2]. The comparison is what makes a second apply a no-op.

docket reaches the same place per task. Every task implements a `Plan()` that probes the live server
once, computes current versus desired, and either reports it is in sync (and does nothing) or returns a
closure that performs the mutation; `apply` is defined as executing that plan [^11]. The difference is not
*whether* there is an equality check but *what records the desired side*: Kubernetes keeps the applied
intent in the object itself (annotation or managed fields) so any client or controller can merge against
it [^1][^2], while docket holds nothing between runs and recomputes intent from the recipe each time [^11].

## Dry run and plan

Kubernetes previews changes with `kubectl diff`, which shows "the current online configuration, and the
configuration as it would be if applied", performing a server-side dry run without mutating anything [^3].
It even carries an exit-code contract - `0` when there is no difference, `1` when there is, and greater
than `1` on error [^3]. GitOps tools add their own previews: Argo CD surfaces the difference between Git
and the cluster as an `OutOfSync` status before (or instead of) syncing [^4].

docket makes preview structural rather than a separate code path. `plan` and `apply` call the *same*
`Plan()` method, and `apply` reuses the probe within its own run, so the two cannot disagree and every
task can be previewed [^11]. The command strings `plan` says it *would* run and `apply` says it *did* run
are rendered by identical code and are byte-identical [^17]. `docket plan --detailed-exitcode` mirrors the
same idea as `kubectl diff`'s exit status, returning `0` for no drift, `2` for drift, and `1` on error
[^12]. The shapes are close; the difference is that `kubectl diff` is a distinct command that re-derives
the apply, whereas docket's preview *is* the apply path stopped short of mutating.

## State and reconciliation

This is the core divergence. Kubernetes is a state store with active reconcilers. Applied objects live
in the cluster datastore [^8], and controllers continuously drive real resources toward that stored
desired state - the reconcile loop is the heart of the system [^9]. GitOps extends the loop to a git
repository as the source of truth: Flux describes reconciliation as "ensuring that a given state ...
matches a desired state declaratively defined somewhere (e.g. a Git repository)", running "every five
minutes by default" and correcting drift as it goes [^5], with the interval configurable per
Kustomization [^6]; Argo CD watches for divergence between Git and the cluster and can sync
automatically [^4].

docket is stateless and has no loop. Each run reads the live server as the single source of truth, makes
the recipe true, and exits - there is nothing persisted to reconcile against and no process running
between invocations [^11]. The trade-off is direct: docket never fights you over an intentional manual
change and needs no control plane to keep running, but it also will not notice or repair drift on its own
- convergence happens exactly when you invoke it. The nearest analog to a stored snapshot is `docket
export`, which reads a live server and writes a recipe that reapplies with no drift, but that is a
command you run, not a controller the tool maintains [^12].

## Drift detection

Because Kubernetes reconciles continuously, drift detection and correction are automatic. Argo CD reports
an application as `OutOfSync` when the cluster deviates from Git and, with self-heal enabled, re-syncs
"when the live cluster's state deviates from the state defined in Git" [^4]. Flux's controllers revert
out-of-band changes on their next interval - manual `kubectl edit/patch/delete` "will be promptly
reverted" [^5]. Drift is caught within a reconcile interval whether or not anyone is watching.

docket detects drift only when you ask. `docket plan` reads the server and reports each task as `[ok]`,
`[+]` create, `[~]` modify, `[-]` remove, or `[!]` when the probe itself errored [^12]. Between runs, drift
simply accumulates unobserved. In practice teams close this gap by running `docket plan --detailed-exitcode`
on a schedule or in CI, which turns drift into a non-zero exit an operator can act on [^12] - but that is an
external cron or pipeline, not a controller inside the tool.

## Deletion and pruning

Here Kubernetes behaves like the managed-set model from the Terraform comparison, not like docket. Because
the applied set is tracked, resources removed from the config can be pruned: `kubectl apply --prune` will
"automatically delete resource objects, that do not appear in the configs" [^1][^7], Argo CD prunes
resources deleted from Git when auto-prune is enabled [^4], and Flux's garbage collection removes
"Kubernetes objects that were previously applied on the cluster but are missing from the current source
revision" [^6]. Deleting a manifest can delete the resource.

docket has no managed set to prune against. Each task is an *assertion* about one resource, so removing a
task from a recipe does nothing to the server; deletion is requested explicitly with `state: absent` [^11].
The upside is that you cannot destroy a workload by deleting or forgetting a block, and there is no prune
step to arm carefully; the downside is that docket will not garbage-collect orphans for you - anything you
want gone must be named.

## Execution model

Kubernetes has no single ordered script. Many controllers reconcile many object kinds concurrently, each
watching the datastore and acting when its resources drift, with dependencies expressed through the API
(references, ownership, readiness and health) rather than through a sequence you write [^9][^5]. GitOps
operators add ordering primitives on top (sync waves, health gates) [^10], but the base model is a fan-out
of independent loops across a cluster.

docket runs a recipe top to bottom against a single target. Plays run in order, tasks run in order [^13],
with `loop:` expanding a task over a list and `register:` passing one task's result to a later task's
condition [^14]. There is no dependency graph and no host fan-out - docket drives one Dokku target, the
local CLI or a single host over SSH [^16]. The contrast is one ordered pass over one server versus many
perpetual loops over a cluster.

## Scope and operational weight

The operational footprints differ by an order of magnitude, which is really a statement about the problems
the two tools are built for. Kubernetes is a distributed system: an API server, a datastore, a set of
controllers, and - for GitOps - an Argo CD or Flux operator running in-cluster, all kept alive so
reconciliation can continue [^8][^5]. That machinery is what buys continuous drift correction, pruning, and
cluster-wide scale.

docket is a single Go binary with no runtime to operate, scoped to one job - declaring the state of a
Dokku app [^15] - and driving the local `dokku` CLI or one server over SSH [^16]. It has no control plane
to install, secure, or upgrade, and it stops existing the moment it exits. It trades continuous
reconciliation and cluster scale for a tool you can run from a laptop or a CI job against a single host.
For a Dokku operator that is the point; for a fleet of services needing self-healing at scale, the
Kubernetes model is what that requires. The two sit at opposite ends of the same declarative idea.

[^1]: Kubernetes, "Declarative Management of Kubernetes Objects Using Configuration Files" (`kubectl apply`, `last-applied-configuration`, pruning) - https://kubernetes.io/docs/tasks/manage-kubernetes-objects/declarative-config/
[^2]: Kubernetes, "Server-Side Apply" (field management, `managedFields`, fully specified intent) - https://kubernetes.io/docs/reference/using-api/server-side-apply/
[^3]: Kubernetes, "kubectl diff" (server-side dry run, exit-code contract) - https://kubernetes.io/docs/reference/kubectl/generated/kubectl_diff/
[^4]: Argo CD, "Automated Sync Policy" (auto-sync, self-heal, auto-prune, `OutOfSync`) - https://argo-cd.readthedocs.io/en/stable/user-guide/auto_sync/
[^5]: Flux, "Core Concepts" (continuous reconciliation, default interval, drift revert) - https://fluxcd.io/flux/concepts/
[^6]: Flux, "Kustomization" (garbage collection / pruning, reconciliation interval) - https://fluxcd.io/flux/components/kustomize/kustomizations/
[^7]: Kubernetes, "kubectl apply" reference (the `--prune` flag) - https://kubernetes.io/docs/reference/kubectl/generated/kubectl_apply/
[^8]: Kubernetes, "Kubernetes Components" (etcd as the cluster datastore) - https://kubernetes.io/docs/concepts/overview/components/
[^9]: Kubernetes, "Controllers" (reconcile loops driving current state toward desired state) - https://kubernetes.io/docs/concepts/architecture/controller/
[^10]: Argo CD, "Sync Phases and Waves" - https://argo-cd.readthedocs.io/en/stable/user-guide/sync-waves/
[^11]: docket, "Writing tasks" (the `Plan()` / probe model) - [docs/writing-tasks.md](../writing-tasks.md)
[^12]: docket, "Command reference" (`plan`, `apply`, drift markers, `--detailed-exitcode`, `export`) - [docs/command-reference.md](../command-reference.md)
[^13]: docket, "Recipes" (plays, tasks, and ordering) - [docs/recipes.md](../recipes.md)
[^14]: docket, "Task envelope" (`loop`, `register`) - [docs/task-envelope.md](../task-envelope.md)
[^15]: docket, "Getting started" (single binary, scope) - [docs/getting-started.md](../getting-started.md)
[^16]: docket, "Remote execution" (one host over SSH) - [docs/remote-execution.md](../remote-execution.md)
[^17]: docket, "JSON output" (the `commands` arrays and their shared rendering) - [docs/json-output.md](../json-output.md)
