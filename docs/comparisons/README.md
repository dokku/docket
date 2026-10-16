# Comparisons

Neutral, reference-backed comparisons of how docket handles plan/apply and idempotency next to other
infrastructure-as-code and configuration-management tools. Each page states what each tool does and the
trade-off - not which is better - cites primary sources inline, and traces every docket claim back to
the reference docs.

The pages are ordered from the tools closest to docket to the ones furthest from it:

- [docket vs the dokku CLI](dokku-cli.md) -- a declarative front-end over the same `dokku`
  commands docket already runs, adding a recipe, a plan, and idempotency.
- [docket vs Ansible](ansible.md) -- docket's direct ancestor; idempotency enforced by
  construction and a guaranteed-consistent plan/apply instead of per-module check mode.
- [docket vs Chef, Puppet, and SaltStack](config-management.md) -- the rest of the
  configuration-management idempotency family, agentless single binary versus server-plus-agent systems.
- [docket vs Terraform / OpenTofu](terraform.md) -- a stateless live probe versus a persisted
  state file, and everything that follows from that one decision.
- [docket vs Kamal](kamal.md) -- desired-state convergence versus an imperative deploy
  pipeline, both agentless over SSH.
- [docket vs Docker Compose](docker-compose.md) -- a per-task probe versus recreate-on-diff,
  and where Compose's project-scoped pruning sits between docket and Terraform.
- [docket vs Kubernetes (kubectl apply and GitOps)](kubernetes.md) -- a one-shot converge
  versus continuous reconciliation with Argo CD or Flux.

## See also

- [Getting started](../getting-started.md) -- why docket, and the plan/apply model these pages build on
- [Command reference](../command-reference.md) -- `plan`, `apply`, and `--detailed-exitcode`
- [Writing tasks](../writing-tasks.md) -- the `Plan()` / `Execute()` model that makes docket idempotent
