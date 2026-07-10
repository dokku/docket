# Migration

Moving a Dokku setup to a new server is a common reason to reach for docket. Because a recipe
already describes the desired state of a server, you can point it at a fresh host and `docket
apply` recreates the apps, config, domains, services, storage mounts, and everything else the
recipe declares. There is no source-to-destination copy: docket targets one host per run, so a
migration is modeled as "apply my recipe against the new server," not a transfer from A to B.

The important thing to understand up front is the boundary. docket recreates the declarative
**structure and configuration** of a server. It does not move the **data** - database contents,
uploaded files, DNS records, or issued TLS certificates. Those are migrated by separate steps,
covered below.

## What docket moves, and what it does not

| docket recreates from the recipe | You migrate separately |
|----------------------------------|------------------------|
| Apps (`dokku_app`) | Database / service contents |
| Config vars (`dokku_config`) | Persistent volume files |
| Domains and ports (`dokku_domains`, `dokku_ports`) | DNS records |
| Service structure (`dokku_service_create`, `dokku_service_expose`, `dokku_service_link`, backup schedule, `dokku_acl_service`) | letsencrypt-issued certificates |
| Storage *mounts* (`dokku_storage_mount`) | Secret values not in the recipe |
| Manual certs inlined via `dokku_certs` `cert_content` / `key_content` | Host-level OS configuration |
| Buildpacks, scheduler and proxy config | Datastore backup credentials and `dokku_service_property` values |
| SSH keys (`dokku_ssh_key`) | |
| App code (`dokku_git_sync`, `dokku_git_from_image`, `dokku_git_from_archive`) | |

For certificates, docket can carry a certificate whose PEM bytes you inline in the recipe, but
it does not migrate an existing letsencrypt issuance - you must re-issue that on the new host
once DNS points at it.

## Before you start

Provision the new server first. docket needs [Dokku >= 0.38.22 and dokku-letsencrypt >=
0.25.0](getting-started.md#prerequisites), plus any datastore plugins your services rely on
(dokku-postgres, dokku-redis, dokku-mysql, and so on) already installed. The
[`dokku_plugin`](tasks/dokku_plugin.md) task can install third-party plugins as part of the
recipe, but the base Dokku install and the datastore plugins are prerequisites you set up
before docket runs.

## Step 1: Capture the old server as a recipe

Run [`docket export`](command-reference.md#docket-export) against the old server to write a recipe
plus a companion vars-file describing it:

```bash
docket export --host deploy@old-server
```

This enumerates the apps and reconstructs their declarative state. It also reconstructs any
git-installed third-party plugins into [`dokku_plugin`](tasks/dokku_plugin.md) tasks in a leading
global play; core plugins and plugins installed from a tarball or local path are omitted. Datastore
services are reconstructed into that same global play - the service itself, its exposed ports, its
backup schedule, and its dokku-acl access list - with each app's service links emitted into the
app's own play. Sensitive values (config and other secrets) are lifted into `tasks.vars.yml`; the
recipe references them through inputs, so the pair is applied together with `--vars-file`. If you
already maintain a recipe as the source of truth for the old server, skip this and use it directly.

Some state cannot be read back and is left out with a warning - notably write-only credentials
(`dokku_git_auth`, `dokku_registry_auth`, and datastore backup credentials), datastore service data,
and service properties (`dokku_service_property`), which you add by hand. Each task's
[reference page](tasks/README.md) has an Export support section noting its limits.

## Step 2: Apply the recipe to the new server

Point docket at the new host and preview, then apply the exported pair:

```bash
docket plan  --host deploy@new-server --tasks tasks.yml --vars-file tasks.vars.yml
docket apply --host deploy@new-server --tasks tasks.yml --vars-file tasks.vars.yml
```

`plan` is the safe first move: it shows everything docket would create on the empty server without
changing anything. See [remote execution](remote-execution.md) for how the `--host` flag and SSH
work, including `--sudo` and host-key handling.

## Step 3: Redeploy the code

Applying the recipe creates the apps but does not carry over the running containers. Bring the
code onto the new server with whichever deploy source your recipe uses:

- [`dokku_git_sync`](tasks/dokku_git_sync.md) syncs from a git remote.
- [`dokku_git_from_image`](tasks/dokku_git_from_image.md) deploys from a Docker image.
- [`dokku_git_from_archive`](tasks/dokku_git_from_archive.md) deploys from a tarball or zip URL.

## Step 4: Move service data (outside docket)

[`dokku_service_create`](tasks/dokku_service_create.md) makes an *empty* service, and
[`dokku_service_backup`](tasks/dokku_service_backup.md) only configures the S3 backup schedule and
auth - there is no restore task. Export carries the schedule, bucket, and `use_iam` flag but not the
AWS credentials or encryption passphrase (they cannot be read back), so re-add those before the
schedule can run. Move the actual contents with Dokku's native export/import:

```bash
# On the old server
dokku postgres:export olddb > db.dump

# On the new server
dokku postgres:import newdb < db.dump
```

Each datastore plugin exposes its own `:export` / `:import` pair. Alternatively, restore from an
existing S3 backup on the new server.

## Step 5: Move persistent storage (outside docket)

[`dokku_storage_mount`](tasks/dokku_storage_mount.md) only wires up the mount; it does not copy the
files behind it. Copy the bytes yourself, for example with rsync:

```bash
rsync -a old-server:/var/lib/dokku/data/storage/<app>/ /var/lib/dokku/data/storage/<app>/
```

## Step 6: DNS, TLS, and cutover (outside docket)

The final steps are the network cutover, which docket does not touch:

- **DNS.** Repoint your domain's A/AAAA records at the new server's IP.
- **TLS.** Re-issue letsencrypt *after* DNS resolves to the new host. To carry a manual
  certificate instead, inline its PEM via [`dokku_certs`](tasks/dokku_certs.md) `cert_content` /
  `key_content`; docket streams those bytes to dokku over stdin, which sidesteps the caveat that
  the `cert` / `key` file-path fields must already exist on the remote host (docket does not upload
  local files).
- **Cutover.** Enable maintenance mode on the old app with
  [`dokku_maintenance`](tasks/dokku_maintenance.md), do a final data sync, flip DNS, verify the new
  server serves traffic, then decommission the old one.

## See also

- [Getting started](getting-started.md) - prerequisites and your first recipe
- [Remote execution](remote-execution.md) - driving a remote server with `--host` over SSH
- [Recipes](recipes.md) - the recipe file format, plays, and multi-app recipes
- [Command reference](command-reference.md) - `plan`, `apply`, and their flags
