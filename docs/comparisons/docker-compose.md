# plan/apply and idempotency: docket vs Docker Compose

Docker Compose is the declarative tool most Dokku users have already met: a compose file describes a
set of services, and `docker compose up` converges the running containers to match it [^1][^2]. That
"one file plus one converge command" shape is close enough to docket's that the two invite comparison,
but they operate on different units and reach idempotency by different mechanisms.

The unit is the first difference. Compose declares services - containers, with their networks and
volumes [^1]. docket declares the state of a Dokku app, which is broader than its containers: app
existence, config, domains, TLS, persistent storage, and plugins [^12]. Compose orchestrates the
container runtime directly; docket declares Dokku state, and Dokku is what wraps Docker, buildpacks,
nginx, and TLS underneath.

The idempotency mechanism is the second. Compose converges by recreation: "if there are existing
containers for a service, and the service's configuration or image was changed after the container's
creation, `docker compose up` picks up the changes by stopping and recreating the containers" [^2]. Run
`up` twice with no change and the second run recreates nothing - idempotent, but by comparing a
service's current container against the file and recreating on a diff. docket instead has each task
probe the live server and skip when already in sync [^6].

There is one axis where Compose sits between docket and Terraform rather than beside either: it groups
a project's resources under a project name and can prune the ones no longer declared, with
`--remove-orphans` removing "containers for services not defined in the Compose file" [^2][^3]. docket
tracks no such set. This document is neutral - Compose's direct runtime control and docket's broader
Dokku-state model serve different jobs - and it notes where each fits.

## At a glance

| Dimension | docket | Docker Compose |
|-----------|--------|----------------|
| Unit of declaration | Dokku app state: apps, config, domains, TLS, storage, plugins [^12] | services (containers), networks, volumes [^1] |
| Idempotency check | per-task live probe; in-sync tasks no-op [^6] | per-service: recreate only if config or image changed since creation [^2] |
| Source of "current state" | live per-task probe, every run [^6] | the running containers compared to the file [^2] |
| Persistent state | none [^6] | no state file; resources grouped under a project name [^3] |
| Tracks a managed set / prunes removed | no; deletion is explicit `state: absent` [^6] | yes, opt-in: `--remove-orphans` removes services no longer in the file [^2] |
| Dry run / plan | `plan` and `apply` share one `Plan()`; `--detailed-exitcode` 0/2/1 [^7] | global `--dry-run` "Execute command in dry run mode" [^3] |
| Execution order | ordered tasks you write; `loop`/`register` [^8][^9] | dependency order from `depends_on` [^4] |
| Scope | declares Dokku state (which wraps Docker, buildpacks, nginx, TLS) [^12] | orchestrates the container runtime directly [^1] |
| Runtime | single Go binary; local or one SSH host [^10][^11] | Docker CLI plugin; local Docker or a remote context |

## Idempotency mechanism

Both tools make a second run safe, but they compare different things. Compose's mechanism is
recreate-on-diff at the container level: "if there are existing containers for a service, and the
service's configuration or image was changed after the container's creation, `docker compose up` picks
up the changes by stopping and recreating the containers (preserving mounted volumes)" [^2]. If nothing
changed, `up` leaves the containers as they are; `--force-recreate` overrides that to "recreate
containers even if their configuration and image haven't changed", and `--no-recreate` suppresses
recreation entirely [^2]. So Compose is idempotent in the sense that an unchanged file plus a second
`up` is a no-op, decided by whether a service's declared config still matches its running container.

docket's mechanism is a per-task probe. Every task implements a `Plan()` that reads the live server
once, computes current versus desired, and either reports it is in sync (and does nothing) or returns
a closure that performs the mutation; `apply` executes that plan [^6]. The comparison is not "did this
container's config hash change" but "does the server's actual state for this task - the config value,
the domain, the TLS enablement, the mount - already match the recipe". Because a Dokku app is more
than a container, docket's equality check spans surface that has no container to recreate.

The upshot: Compose reconciles the container runtime by rebuilding what drifted; docket reconciles a
broader slice of app state by probing each piece and touching only what is off.

## Statelessness and the managed set

Neither tool keeps a Terraform-style state file, but they differ on whether they track a managed set.
docket is fully stateless and keeps no inventory: each task's `Plan()` probe is the only source of
truth, and there is no record of "everything docket manages" [^6]. Compose also has no state file, but
it groups a run's resources under a project name - derived from `-p`, `COMPOSE_PROJECT_NAME`, the
top-level `name:`, or the project directory's basename [^3] - and that grouping lets it reason about the
whole set, not just the service in front of it.

That is why Compose can answer a question docket cannot: "which running containers belong to this
project but are no longer in the file?" It surfaces them as orphans and, with `--remove-orphans`, deletes
them [^2]. docket has no equivalent because it has no managed set to diff against - it only ever acts on
the tasks you wrote. On this axis Compose sits between docket (tracks nothing) and Terraform (tracks
everything and destroys removed resources by default): Compose tracks the set but prunes only when you
ask.

## Dry run and plan

docket makes dry run a first-class, structural feature. `plan` and `apply` call the same `Plan()`
method, and `apply` reuses the probe within its own run, so the two cannot disagree and every task can
be previewed [^6]. `plan` reports drift with `[ok]`, `[+]`, `[~]`, `[-]`, and `[!]` (probe error), and
`docket plan --detailed-exitcode` returns 0 for no drift, 2 for drift, and 1 on error - an exit-code
contract built for CI [^7].

Compose offers a global `--dry-run` flag, described tersely as "Execute command in dry run mode" [^3].
It lets you invoke a Compose command to see what it would do without executing it, which covers the
same need at the command level. It is not, however, framed as a separate `plan` verb with a stable
drift-marker vocabulary or a detailed exit-code contract the way docket's `plan` is; it is a mode you
add to an existing command.

## Deletion and pruning

The two tools remove things differently. In docket, deletion is an explicit assertion: a task requests
it with `state: absent`, and removing a task from the recipe does nothing to the server, because docket
tracks no managed set to prune from [^6]. You cannot destroy a resource by deleting or forgetting a
block, but nothing is cleaned up automatically either.

Compose removes on two triggers. `docker compose down` tears down the project's containers and
networks [^5], and `--remove-orphans` on `up` or `down` removes "containers for services not defined in
the Compose file" [^2]. Removal is opt-in - a plain `up` leaves orphaned containers in place - so
Compose is more aggressive than docket (it can prune a service you deleted from the file) but less
aggressive than Terraform's default, where removing a resource block destroys it on the next apply.

## Execution model

Compose derives an order from the graph of service relationships: "Compose always starts and stops
containers in dependency order, where dependencies are determined by `depends_on`, `links`,
`volumes_from`, and `network_mode: "service:..."`", and "Compose creates services in dependency order"
[^4]. You express dependencies and Compose schedules from them, much as Terraform does with its
resource graph.

docket runs in the order you write. A recipe is ordered plays then ordered tasks [^8], with `loop:`
expanding a task over a list and `register:` passing one task's result to a later task's condition [^9].
There is no dependency graph derived from references between tasks; the sequence is the source order.
Compose's model is convenient when services genuinely depend on one another; docket's is explicit and
predictable.

## Scope and runtime

Finally, the two sit at different layers. Compose is a Docker CLI plugin that orchestrates the
container runtime directly - creating and converging services against local Docker or a remote context
[^1]. docket declares Dokku state and lets Dokku do the orchestration; a docket recipe can create an
app, set config, attach domains, enable TLS, and mount storage, and Dokku translates those into the
underlying Docker, buildpack, nginx, and certificate operations [^12]. docket ships as a single Go
binary driving the local `dokku` CLI or one host over SSH [^10][^11], whereas Compose runs wherever the
Docker CLI does. A Dokku user reaching for Compose gets direct container control; reaching for docket
gets the whole app's declarative state, with a real plan/apply over it.

[^1]: Docker, "Docker Compose overview" - https://docs.docker.com/compose/
[^2]: Docker, "docker compose up" (recreate-on-change, `--force-recreate` / `--no-recreate`, `--remove-orphans`) - https://docs.docker.com/reference/cli/docker/compose/up/
[^3]: Docker, "docker compose" CLI reference (global `--dry-run`, project name) - https://docs.docker.com/reference/cli/docker/compose/
[^4]: Docker, "Control startup and shutdown order in Compose" (`depends_on` dependency order) - https://docs.docker.com/compose/how-tos/startup-order/
[^5]: Docker, "docker compose down" - https://docs.docker.com/reference/cli/docker/compose/down/
[^6]: docket, "Writing tasks" (the `Plan()` / probe model, `state` fields) - [docs/writing-tasks.md](../writing-tasks.md)
[^7]: docket, "Command reference" (`plan`, `apply`, drift markers, `--detailed-exitcode`) - [docs/command-reference.md](../command-reference.md)
[^8]: docket, "Recipes" (plays, tasks, and ordering) - [docs/recipes.md](../recipes.md)
[^9]: docket, "Task envelope" (`loop`, `register`) - [docs/task-envelope.md](../task-envelope.md)
[^10]: docket, "Getting started" (single binary) - [docs/getting-started.md](../getting-started.md)
[^11]: docket, "Remote execution" (one host over SSH) - [docs/remote-execution.md](../remote-execution.md)
[^12]: docket, "Tasks" (the full task surface) - [docs/tasks/README.md](../tasks/README.md)
