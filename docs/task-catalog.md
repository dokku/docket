# Task catalog

`docket schema` prints a machine-readable description of every task type docket registers: the
recipe keys each one accepts, their types, defaults, choices and descriptions, which values are
secrets, what resource the task addresses, which app or service it changes, whether it can be
exported and probed, and - for a
property task - the exact set of property names it accepts.

It is the same data the [task reference](tasks/README.md) pages are rendered from, so the two can
never disagree. Reach for it when something other than a human needs to know the shape of a
recipe: a schema for another recipe format, an editor completing task bodies, a linter checking a
recipe before it reaches a server.

```bash
docket schema
docket schema --output catalog.json
docket schema --task dokku_config --task dokku_domains
```

The command is offline: it opens no subprocess and contacts no server. The answer depends only on
which docket binary is running, which is why the catalog is not a file in this repository - it
cannot go stale.

## The document

One JSON object, pretty-printed, with a `version` and a `tasks` array sorted by task type:

```json
{
  "version": 1,
  "tasks": [
    {
      "type": "dokku_domains",
      "synopsis": "Manages the domains for a given dokku application or globally",
      "export": { "status": "supported" },
      "probe": { "status": "supported" },
      "identity": {
        "keys": ["app", "global"],
        "collections": [{ "name": "domains", "item": "value" }]
      },
      "objects": [{ "kind": "app", "keys": ["app"] }],
      "fields": [
        { "name": "app", "type": "string", "required": false, "identity": "key",
          "description": "Name of the app" },
        { "name": "global", "type": "bool", "required": false, "identity": "key",
          "description": "Flag indicating if the domains should be applied globally" },
        { "name": "domains", "type": "list", "required": false,
          "required_when": ["present", "absent"], "forbidden_when": ["clear"],
          "identity": "collection", "ordered": false, "description": "List of domain names",
          "item": { "type": "string", "identity": "value" } },
        { "name": "state", "type": "string", "required": false, "default": "present",
          "choices": ["present", "absent", "set", "clear"],
          "description": "Desired state of the domains" }
      ],
      "constraints": [
        { "kind": "exactly_one_of", "fields": ["app", "global"] }
      ],
      "examples": [
        { "name": "Add a domain to an app", "yaml": "dokku_domains:\n    app: node-js-app\n..." }
      ]
    }
  ]
}
```

A task that runs an operation rather than declaring a resource also carries `"imperative": true`
after its synopsis (or its deprecation notice). It has nothing to undo, so it offers no `absent` or
`clear` state, and a consumer that tears down what it applied can skip it. The key is absent on every
declarative task. Today the imperative tasks are `dokku_app_clone`, `dokku_git_sync`,
`dokku_git_from_archive` and `dokku_git_from_image`.

Every name in the document is a key a recipe author types. Go type names and Go field names are
deliberately absent - the catalog describes the recipe surface, not docket's implementation.

## Dokku and plugin versions

`minimum_dokku_version` is the oldest Dokku release that task's probe parses, as
`major.minor.patch`. `plugin_requirements` lists the non-core plugins it needs: `minimum`, `name`
when the plugin:list name is fixed, and `when` or `name_field` holding the recipe key when the
requirement is conditional or the plugin name comes from a field. `name` is absent when
`name_field` is set. `requirements` stays the plugin prose those declarations print, one line per
plugin, and does not repeat the Dokku floor.

`plan`, `apply`, and `export` refuse a task when the live server, or an installed plugin it
requires, is older. `--ignore-version` skips that check. Adding these keys does not bump
`version`.

There is a [JSON Schema](schemas/task-catalog-v1.schema.json) for the whole thing. Unlike the
[JSON-lines streams](json-output.md), which validate one line at a time, this one validates the
document as a unit.

### Narrowing the catalog

`--task <type>` restricts the `tasks` array to the types you name, and is repeatable:

```bash
docket schema --task dokku_config --task dokku_domains | jq -r '.tasks[].type'
```

The document is unchanged in every other respect: the same `version`, the same `tasks` array, and
each entry byte-identical to its form in the whole catalog. The published JSON Schema validates a
narrowed document as it validates a full one, so a consumer parses one format either way and
nothing has to change for a consumer that never passes the flag.

Types are selected by registry key, matched exactly and case-sensitively, in any order - the array
still comes back sorted by `type`. Naming one type twice emits it once, since the document is a
set keyed by type. An unknown type exits non-zero and names the closest registered one, the
way an unknown task type in a recipe does; it is not an empty `tasks` array, which would read as
"docket has no such task" rather than "you typed it wrong".

## Versioning

`version` is the catalog's wire format, pinned at `1`. Branch on it so a future schema change does
not silently break a consumer. It is independent of the `version` on
[`--json` events](json-output.md): they are different formats with different consumers, and a
breaking change to one does not force the other's consumers to re-branch. Adding a key within a
major version does not bump it.

The binary's own version is deliberately not in the document, so two builds of the same code emit
byte-identical catalogs and a diff shows only real changes. Use `docket version` for that.

## Fields

Each entry in `fields` is one recipe key, in the order the task declares them - which is the order
the reference page lists them in.

| Key | Meaning |
|-----|---------|
| `name` | The recipe key. |
| `type` | `string`, `bool`, `int`, `float`, `list`, `dict`, or `any`. |
| `required` | Whether a recipe must supply it. |
| `required_when` | The states in which a recipe must supply it. Absent when the field is never conditionally required. |
| `forbidden_when` | The states in which a recipe must not supply it. Absent when the field is allowed in every state. |
| `default` | The default, as a literal string to be read according to `type`. Absent when there is none. |
| `choices` | The permitted values. Absent when the field is not an enum. |
| `description` | Prose for a human. |
| `sensitive` | Present and `true` when the value is a secret. |
| `runner_file` | Present and `true` when the value names a file docket reads on the machine it runs on, not on the dokku server. |
| `write_only` | Present and `true` when the server stores the value but a read of the resource never returns it. |
| `apply_only` | Present and `true` when the value steers how apply runs and is not server state. |
| `ordered` | On every `list`: `true` when the element order is significant, `false` when docket ignores it. Absent on any other type. |
| `identity` | `key` or `collection`. Absent on a field that is neither. |
| `item` | The element shape of a `list` or `dict`. Absent on a scalar. |

Two of these need care:

- **`required: false` plus a `default`** means optional, and the server sees the default. A field
  carrying a default is never required, whatever its declaration says, because the default is
  filled in before the missing-field check runs.
- **A `default` on a field docket treats as optional-with-a-nil-default** documents what the task
  coerces an omitted key to, not a value written into the recipe. `dokku_config`'s `restart` is
  the example: the catalog reports `"default": "true"`, and an explicit `restart: false` is still
  honoured.

`runner_file` matters to a consumer that runs docket somewhere other than the dokku server. Most
paths a task accepts are handed to dokku and resolve on the server, but a `runner_file` field is
read by docket itself, so the file has to exist on the runner even when docket drives a
[remote server](remote-execution.md#what-the-runner-needs). A wrapper that knows it cannot supply
local files can warn about, or refuse, a recipe that sets one before running anything.

`write_only` and `apply_only` matter to a consumer that refreshes its state from the server, such
as a Terraform provider reading a resource back with
[`sdk.ReadTask`](embedding.md#reading-one-resource). Neither field carries the server's value: a
read leaves both at their zero value or default, and every field without either flag is one a read
fills in. A consumer should keep the value it was configured with rather than report the
difference as drift.

- **`write_only`** is a value the server keeps but a read will not hand back, such as
  `dokku_service_create`'s `password` or `dokku_registry_auth`'s `username` and `password`. An
  [export](command-reference.md#docket-export) leaves it empty or lifts it into a required input
  with no value, unless another command reads it back: `dokku_certs`'s `cert_content` and
  `key_content` are exported through `certs:show`, but a read settles drift from the certificate's
  fingerprint and never moves the PEM off the server. The probe may still verify a `write_only`
  value by comparison - `dokku_registry_auth` asks `registry:auth-status` whether the credentials
  work, and `dokku_service_backup` compares fingerprints - so `write_only` says nothing about drift
  detection.
- **`apply_only`** is a knob that shapes one apply and leaves nothing behind to read, such as
  `dokku_config`'s `restart`, `dokku_http_auth_user`'s `update_password`, `dokku_certs`'s `cert`
  path, whose certificate is exported as `cert_content` instead, or
  `dokku_maintenance_custom_page`'s `tarball`, whose page is read back as `content`. An export
  carries it at its zero value or default.

Both flags are unconditional. A gap that depends on another value stays in the task's `export` and
`probe` caveats instead: `dokku_service_property`'s `value` is unreadable only for the backup
credential properties, and `dokku_maintenance_custom_page`'s `content` only on a plugin without
`maintenance:custom-page-export`.

`ordered` tells a consumer whether to model a `list` as a list or as a set. `true` means docket
applies the elements in order and compares them in order, so a reorder is drift:
`dokku_buildpacks`'s `buildpacks` set build precedence, `dokku_service_expose`'s `ports` map
positionally onto the service's container ports, and `dokku_service_create`'s network lists are
compared as written. `false` means docket ignores the order, as it does for `dokku_domains`'s
`domains` or `dokku_http_auth_allowed_ip`'s `allowed_ips`, and an exported task may return the
elements in any order. `false` says nothing about uniqueness: `dokku_scheduler_k3s_profile`'s
`kubelet_args` ignores order but still counts a repeated argument. Every `list` carries the key,
element fields included, so a list without it comes from a docket that predates it.

### Item shapes

`list` is the same word for a list of strings and a list of structured entries, so a collection
field carries an `item` describing its elements. When the element is structured, `item.type` is
`object` and `item.fields` is a full field list for it - which is where a consumer learns that a
`dokku_http_auth_user` entry's `password` is a secret the server never returns, while its `hash`
is read back:

```json
{ "name": "users", "type": "list", "ordered": false, "identity": "collection",
  "item": {
    "type": "object", "identity": "fields", "keys": ["username"],
    "fields": [
      { "name": "username", "type": "string", "required": true, "identity": "key" },
      { "name": "password", "type": "string", "required": false, "sensitive": true, "write_only": true },
      { "name": "hash", "type": "string", "required": false, "sensitive": true }
    ]
  } }
```

A `dict` carries `item.key_type` (always `string`) alongside `item.type`, so a
`map[string]string` of config pairs and a map of arbitrary values are distinguishable.

`item.identity` says what identifies one entry - `value` for a list of scalars, `map_key` for a
dict, `fields` (plus `keys`) for structured entries. It is present only on a field tagged as a
collection: an untagged list is an attribute of the resource rather than the set of items the task
manages, so it has no item identity to declare.

An object item can carry `item.constraints` as well, read the same way as a task's
[constraints](#constraints). Its `states` are still the task's.

## Constraints

`required` only says a field is always needed. The rest of what a task needs is declared, and the
declarations are what docket enforces, so a consumer that attaches them to its own schema refuses
exactly what `plan`, `apply` and `docket validate` would.

`required_when` and `forbidden_when` on a field list the states in which it must, or must not, be
set. The states are values of the task's `state` field, so they apply only to a task that has one.

`constraints` on a task, and `item.constraints` on an object item, are the rules over several
fields, in declaration order:

| `kind` | Meaning |
|--------|---------|
| `exactly_one_of` | Exactly one of `fields` is set. |
| `at_least_one_of` | One or more of `fields` is set. |
| `all_or_none_of` | `fields` are set together or not at all. |
| `conflicts_with` | None of `fields` is set while `field` is. |
| `required_with` | Every one of `fields` is set while `field` is. |

`field` is present only on `conflicts_with` and `required_with`. `states` lists the states the rule
applies in, and is absent when it applies in every state. Every name is a key of the same task or
item.

```json
{ "kind": "at_least_one_of", "fields": ["cert", "cert_content"], "states": ["present"] }
```

A field is set when it differs from its zero value and from its `default`. An empty list or map, a
`false` bool, and a value equal to the default all count as unset. A whitespace-only string counts
as set.

The kinds map onto Terraform's validators: `exactly_one_of` onto `ExactlyOneOf`,
`at_least_one_of` onto `AtLeastOneOf`, `conflicts_with` onto `ConflictsWith`, `required_with` onto
`RequiredWith`, and `all_or_none_of` onto `RequiredWith` from each field to the others. A rule
with `states`, and every `required_when` and `forbidden_when`, depends on the configured `state`,
which those validators cannot express.

Rules that depend on a value rather than on whether a field is set - an enum, a format, a port
range, a property name - are not declared. They stay in each task's own validation and in the field
descriptions, and `docket validate` reports them. Adding these keys does not bump `version`.

## Identity

`identity.keys` are the recipe keys that decide *which* resource the task manages, in the order a
[resource address](task-envelope.md#names-and-resource-addresses) renders them. A key holding its
zero value is omitted from the address, which is what lets the mutually exclusive `app` / `global`
pair produce two different addresses for the same task type.

It is the same list as the fields carrying `"identity": "key"`, in declaration order, and is
repeated at the top level because building an address is the main thing a consumer does with it.

`identity.collections` names the collections the task manages wholesale, and how one entry in each
is identified.

## Objects

`objects` names the app and the datastore service a task changes, app first. A server-level task
such as `dokku_plugin` changes neither and carries no `objects` key.

```json
"objects": [
  { "kind": "app", "keys": ["app"], "restarts": {} },
  { "kind": "service", "keys": ["service", "name"] }
]
```

| Key | Meaning |
|-----|---------|
| `kind` | `app` or `service`. |
| `keys` | The recipe keys naming the object: the app's key, or the datastore type's key then the service name's key. |
| `creates` | Present and `true` on the task that creates the object, and destroys it in its `absent` state. |
| `restarts` | Present when applying the task can restart or redeploy the object. An empty object means whenever it applies. |

`keys` follow the object, not the task's declaration order: `dokku_acl_service` names the instance
`service` and the datastore `type`, so its keys are `["type", "service"]`, the same service
`dokku_service_property`'s `["service", "name"]` names. An object applies only when every one of
its keys is set, so an app-or-global task with `global: true` changes no app.

`restarts` narrows when the restart happens. Every condition present must hold:

| Key | Meaning |
|-----|---------|
| `when` | Recipe keys of bool fields. The task restarts the object only when at least one is true. An omitted key takes its `default`, so `dokku_config`'s `restart` restarts the app unless a recipe sets it to `false`. |
| `unless` | Recipe keys of bool fields. A true value for any of them stops the restart, as `dokku_ps_scale`'s `skip_deploy` does. |
| `states` | The `state` values the task restarts it in. `dokku_domains_toggle` restarts the app when enabling, not when disabling. |

A proxy reload is not a restart, so `dokku_domains`, `dokku_ports` and `dokku_letsencrypt` carry no
`restarts`. `dokku_service_create` with `restart_apps` also restarts the apps linked to the service,
but their names are not keys of the task, so that is not declared.

### Ordering tasks

docket's sdk serializes tasks on the same object (see
[Running tasks concurrently](embedding.md#running-tasks-concurrently)), but it never reorders
them. A consumer that applies tasks in its own order, such as a Terraform provider, orders them
from `objects`:

- Apply the task that `creates` an object before every other task naming the same object, and tear
  it down after them. A `dokku_service_backup` or a `dokku_service_property` needs its
  `dokku_service_create`; a `dokku_service_link` needs both its `dokku_app` and its
  `dokku_service_create`.
- Expect a task with `restarts` to interrupt the object while it applies. Its effect on a running
  app is the restart; a task that changes a setting without one, such as `dokku_resource_limit`,
  takes effect on the next deploy.

## Property tasks

A `*_property` task looks like it takes a free-form `property: string`. It does not: each one
manages a fixed table of property names, and until this catalog existed the only way to discover
them was to trigger a validation error. `property_schema` publishes the table:

```json
"property_schema": {
  "plugin": "apps",
  "subcommand": "apps:set",
  "field": "property",
  "properties": [
    { "name": "deploy-source", "scopes": ["app"], "app_report_key": "deploy-source" },
    { "name": "disable-autocreation", "scopes": ["global"],
      "global_report_key": "global-disable-autocreation" }
  ]
}
```

`scopes` is what a consumer usually wants: a property listed only under `global` may be set with
`global: true` and is rejected for an app, matching dokku's own rejection. The report keys are what
docket reads from `dokku <plugin>:report --format json` to detect drift.

Some plugins accept a family of names that cannot be enumerated - dokku validates them through the
set subcommand rather than through its report schema. Those are published as prefixes, so a
consumer does not reject a legal recipe:

```json
"dynamic": [ { "prefix": "dns-provider-", "probeable": true, "sensitive": true,
               "scopes": ["global"] } ]
```

`probeable: false` means docket cannot read those values back, so a recipe using that family
reports drift on every run and never converges.

`sensitive: true` means the members hold secrets: docket masks them in the command echo and lifts
them into an input on export. It is independent of `probeable` - a credential docket cannot read
back is still masked on the way out.

`scopes` reads exactly as it does on an enumerable property: a family listed only under `global` may
be used with `global: true` and is rejected for an app, matching dokku's own rejection. The
`dns-provider-*` credentials are global-only on traefik and app-or-global on letsencrypt, so a
consumer that ignored this would accept a recipe dokku refuses.

A name can also be absent from `properties` because another task manages it. That is a different
answer from "no such property", and the catalog says so rather than leaving a consumer to report an
unknown name and offer a list it will never be in:

```json
"rejected": [ { "prefix": "chart.", "replacement": "dokku_scheduler_k3s_chart",
                "reason": "the scheduler-k3s:set path for chart values is deprecated in dokku" } ]
```

`replacement` is always a `type` present elsewhere in the same catalog, so a consumer can point the
user at it directly. `docket validate` rejects a matching name with the same sentence `plan` and
`apply` do - `<prefix>* properties are managed by <replacement>; <reason>`.

**An absent `property_schema` on a task that has a `property` field means the legal names are not
enumerable from docket, not that there are none.** `dokku_service_property` is the case: its names
come from whichever datastore plugin backs the service, and which of them a service accepts depends
on its datastore. `<service>:info` reports their values, so the task converges, but `<service>:set`
is what validates a name.

## Stability

- Tasks are sorted by `type`, including when `--task` narrows the catalog, so the output never
  depends on the order the flags were given; properties by `name`; dynamic and rejected families by
  `prefix`.
- Fields, identity keys, requirements and choices keep declaration order, because that order is
  meaningful.
- Objects are always the app, then the service, and an object's keys follow its kind rather than
  declaration order.
- Two runs of the same binary produce byte-identical output, for the whole catalog and for any
  `--task` selection.

## See also

- [Command reference](command-reference.md#docket-schema) - the command and its flags
- [Tasks](tasks/README.md) - the same information rendered for a human
- [JSON output](json-output.md) - the `--json` event streams, a separate format
- [Writing tasks](writing-tasks.md) - the declarations a task makes to fill the catalog
