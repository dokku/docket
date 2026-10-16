# plan/apply and idempotency: docket vs Kamal

docket and Kamal are close in operational shape and far apart in what they model, which makes for a
sharp comparison. Both are single tools you run from your laptop against servers you own: agentless,
SSH-driven, with no control plane and no persisted state file. Kamal calls itself "Capistrano for
Containers, without the need to carefully prepare servers in advance" [^1], and it "works seamlessly
across multiple servers, using SSHKit to execute commands" [^2] - plain Docker commands over SSH, not
declarative state reconciled the way Kubernetes or Swarm do it. docket is likewise a single Go binary
[^7] that drives a Dokku target locally or over one SSH host [^11], reading live state each run rather
than storing it [^8].

Where they diverge is the job. Kamal is a deploy *pipeline*: `kamal deploy` builds and pushes an image,
boots new containers, health-checks them, and switches traffic through kamal-proxy [^1][^3][^5]. It is
"intentionally designed around imperative commands, like Capistrano" [^1], and its command surface is
rollout-shaped - `deploy`, `redeploy`, `rollback`, `setup`, `accessory`, `proxy` [^3]. docket is
desired-state *convergence*: each task probes the live server, computes drift, and runs only the
commands needed to close the gap, across a broad app surface - apps, config, domains, TLS, storage,
plugins, and git sync - not just container rollout [^8][^12].

That difference shows up most in idempotency and preview. Kamal makes a deploy happen; docket makes the
server *match a description*, and can tell you what would change before it does. Neither is a substitute
for the other's core value: Kamal owns zero-downtime container cutover [^1], and docket owns drift-aware
declaration of a Dokku app's whole configuration [^9]. This document is neutral about which fits your
setup and focuses on how each handles plan/apply and idempotency.

## At a glance

| Dimension | docket | Kamal |
|-----------|--------|-------|
| Core model | desired-state convergence [^8] | imperative deploy pipeline [^1][^3] |
| Idempotency check | equality of desired vs current, per task [^8] | no per-resource drift check; each deploy runs the pipeline [^3] |
| Dry run / plan | `plan` and `apply` share one `Plan()`; `--detailed-exitcode` 0/2/1 [^9] | no plan/diff/dry-run command among its commands [^3] |
| Persistent state | none; live probe each run [^8] | none; commands over SSH, no control plane [^2] |
| Transport | local `dokku` CLI or one host over SSH [^11] | SSH via SSHKit, multiple servers [^2] |
| What it manages | full Dokku app state (config, domains, TLS, storage, plugins, git) [^12] | container rollout, accessories, proxy [^1][^3][^4] |
| Traffic cutover | delegated to Dokku's proxy [^12] | kamal-proxy health-checks `/up`, then switches [^5] |
| Rollback | re-apply desired state [^8] | `kamal rollback VERSION` [^3] |
| Runtime | single Go binary, no Ansible/Ruby needed [^7] | Ruby gem, Docker-native [^1][^6] |
| Scope | Dokku-specific (Dokku wraps Docker/buildpacks) [^7] | app-runtime-agnostic containers [^1][^2] |

## Idempotency mechanism

docket converges by having every task check the live server and skip when it already matches. Each task
implements a `Plan()` that probes once, computes current versus desired, and either reports it is in
sync (and does nothing) or returns a closure that performs the mutation; `apply` is defined as executing
that plan, so a second `apply` against an unchanged server changes nothing [^8]. Idempotency is a
property of the task model.

Kamal's model is a deploy pipeline rather than a per-resource convergence. `kamal deploy` is documented
as "Deploy app to servers", `kamal redeploy` as "Deploy app to servers without bootstrapping servers,
starting kamal-proxy and pruning" [^3]; the tool boots the specified image version and cuts traffic over
to it each time you run it. There is no per-resource equality check that decides "this is already in the
desired state, do nothing" the way docket's probe does - the deploy runs its steps. This is a good fit
for Kamal's purpose: shipping a new image version is the common case, and a rolling, health-checked boot
is exactly what you want there [^1][^5]. It is a different guarantee from docket's "run it twice, the
second run is a no-op" [^7].

## Dry run and plan

docket makes preview structural. `plan` and `apply` call the *same* `Plan()`, and `apply` reuses the
probe within its own run, so the two cannot disagree and every task can be previewed [^8]. `plan`
reports drift with `[ok]`, `[+]`, `[~]`, `[-]`, and `[!]` (probe error), and `docket plan
--detailed-exitcode` returns 0 for no drift, 2 for drift, and 1 on error, which gives CI a gate on
whether anything would change [^9].

Kamal's documented command set does not include a plan, diff, or dry-run command that previews changes
against desired state without applying them [^3]. It offers rich rollout controls - health checks,
timeouts, rollback to a prior version - but the preview-before-change step that docket's `plan` provides
is not part of the surface. If your question is "what will change if I run this?", docket answers it
before touching the server; Kamal's answer is the deploy itself, with health checks and a rollback path
if the new version misbehaves [^3][^5].

## Statelessness

Here the two agree, and both differ from state-file tools like Terraform. Kamal keeps no agent on the
servers and no control plane of its own; it executes commands over SSH with SSHKit [^1][^2], and nothing
about your infrastructure is recorded in a persisted state file that could drift from reality. docket is
stateless in the same sense: the live probe is the source of truth on every run, so there is nothing to
store, lock, or reconcile, and no stored record to fall out of sync [^8].

The shared trade-offs follow: neither keeps a cheap inventory of what it manages, and both pay to read
live state (docket on every plan/apply probe; Kamal implicitly, by running against the servers each
deploy). docket's `export` - reading a live server and writing a recipe that reapplies with no drift -
is the nearest thing to a snapshot, and it is a command you invoke rather than a file the tool maintains
[^9].

## What each manages

The surfaces barely overlap. Kamal manages the containerized app's rollout plus its supporting pieces:
the image build and push, the app containers, "accessories" for databases and caches ("Manage
accessories (db/redis/search)" [^3], "Additional services to run in Docker" [^4]), and the proxy that
fronts traffic [^1][^5]. Its configuration lives in `config/deploy.yml` with sections for service, image,
servers, registry, env, proxy, accessories, and builder [^4].

docket manages the declarative state of a Dokku app across many subsystems - the app itself, config
variables, domains, TLS via letsencrypt, storage mounts, third-party plugins, and git sync - each as a
task with its own idempotent probe [^8][^12]. It does not build or push images or orchestrate a container
cutover itself; that is Dokku's job, which docket triggers through tasks like git sync [^12]. So Kamal
owns the deploy mechanics end to end, while docket declares the configuration around and including a
deploy and lets Dokku perform the container work.

## Execution model

Both are agentless and run over SSH, and both are essentially ordered rather than graph-scheduled. Kamal
fans a deploy out across every server in the config, using SSHKit to execute commands on multiple hosts
[^2], which suits a horizontally scaled web app on several boxes. docket drives a single Dokku target -
the local CLI or one host over SSH [^11] - and runs its recipe as ordered plays and tasks, with no host
fan-out [^10].

The concurrency axis is therefore different: Kamal's parallelism is across servers running the same
rollout; docket has none to speak of and instead sequences tasks against one target. Neither derives an
execution order from a dependency graph; you get the order you wrote (docket) or the fixed pipeline steps
of a deploy (Kamal).

## Deploy pipeline versus convergence

The clearest way to hold the two in mind is pipeline versus convergence. A Kamal deploy is a sequence
with a defined shape: build and push the image, boot new containers, health-check them - "the proxy will
by default hit `/up` once every second until we hit the deploy timeout" - and once the app is up, the
proxy stops probing [^5]. The value is a zero-downtime, health-gated cutover to a new version [^1][^5].

A docket run is not a rollout with a beginning and end so much as a reconciliation: read what the server
currently is, compare it to the recipe, and issue only the commands that close the difference, task by
task [^8]. Running it when nothing has changed does nothing; running it after editing one config value
changes only that. The two answer different questions - "ship this new version safely" versus "make the
server match this description" - which is why many setups could reasonably use a deploy tool and a
convergence tool for different parts of the same app.

## Runtime and scope

Operationally both are lightweight single tools with no agent to install, but they sit at different
layers. Kamal is Docker-native and app-runtime-agnostic: it deploys "web apps anywhere" as containers,
"originally built for Rails apps" but working with "any type of web app that can be containerized", and
a fresh server "will be auto-provisioned with Docker" on first use [^1]. docket is Dokku-specific, and
Dokku itself is the layer that wraps Docker, buildpacks, and the proxy; docket declares a Dokku app's
state and lets Dokku realize it [^7]. It ships as a single Go binary with no additional runtime required
[^7], while Kamal is distributed as a Ruby gem [^6].

Put simply, Kamal talks to Docker directly and owns the deploy; docket talks to Dokku and owns the
declaration. If you already run Dokku, docket adds a plan/apply and idempotency layer over its whole app
surface; if you run raw Docker on your own servers, Kamal gives you a zero-downtime container deploy
without adopting a PaaS.

[^1]: Kamal, "Deploy web apps anywhere" (homepage) - https://kamal-deploy.org/
[^2]: Kamal, README (`basecamp/kamal`) - https://github.com/basecamp/kamal
[^3]: Kamal, "View all commands" - https://kamal-deploy.org/docs/commands/view-all-commands/
[^4]: Kamal, "Configuration" overview - https://kamal-deploy.org/docs/configuration/overview/
[^5]: Kamal, "Proxy" configuration (health checks) - https://kamal-deploy.org/docs/configuration/proxy/
[^6]: Kamal, "Installation" (`gem install kamal`) - https://kamal-deploy.org/docs/installation/
[^7]: docket, "Getting started" (single binary, Dokku scope) - [docs/getting-started.md](../getting-started.md)
[^8]: docket, "Writing tasks" (the `Plan()` / probe model) - [docs/writing-tasks.md](../writing-tasks.md)
[^9]: docket, "Command reference" (`plan`, `apply`, drift markers, `--detailed-exitcode`, `export`) - [docs/command-reference.md](../command-reference.md)
[^10]: docket, "Recipes" (plays, tasks, and ordering) - [docs/recipes.md](../recipes.md)
[^11]: docket, "Remote execution" (one host over SSH) - [docs/remote-execution.md](../remote-execution.md)
[^12]: docket, "Tasks" (the full task surface) - [docs/tasks/README.md](../tasks/README.md)
