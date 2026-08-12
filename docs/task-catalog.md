# Task catalog

`docket schema` prints a machine-readable description of every task type docket registers: the
recipe keys each one accepts, their types, defaults, choices and descriptions, which values are
secrets, what resource the task addresses, whether it can be exported and probed, and - for a
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
      "fields": [
        { "name": "app", "type": "string", "required": false, "identity": "key",
          "description": "Name of the app" },
        { "name": "global", "type": "bool", "required": false, "identity": "key",
          "description": "Flag indicating if the domains should be applied globally" },
        { "name": "domains", "type": "list", "required": false, "identity": "collection",
          "description": "List of domain names; omit for state 'clear'",
          "item": { "type": "string", "identity": "value" } },
        { "name": "state", "type": "string", "required": false, "default": "present",
          "choices": ["present", "absent", "set", "clear"],
          "description": "Desired state of the domains" }
      ],
      "examples": [
        { "name": "Add a domain to an app", "yaml": "dokku_domains:\n    app: node-js-app\n..." }
      ]
    }
  ]
}
```

Every name in the document is a key a recipe author types. Go type names and Go field names are
deliberately absent - the catalog describes the recipe surface, not docket's implementation.

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
| `default` | The default, as a literal string to be read according to `type`. Absent when there is none. |
| `choices` | The permitted values. Absent when the field is not an enum. |
| `description` | Prose for a human. |
| `sensitive` | Present and `true` when the value is a secret. |
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

### Item shapes

`list` is the same word for a list of strings and a list of structured entries, so a collection
field carries an `item` describing its elements. When the element is structured, `item.type` is
`object` and `item.fields` is a full field list for it - which is where a consumer learns that a
`dokku_http_auth_user` entry's `password` is a secret:

```json
{ "name": "users", "type": "list", "identity": "collection",
  "item": {
    "type": "object", "identity": "fields", "keys": ["username"],
    "fields": [
      { "name": "username", "type": "string", "required": true, "identity": "key" },
      { "name": "password", "type": "string", "required": false, "sensitive": true },
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

The catalog cannot express every rule a task enforces. Mutually exclusive fields, per-item
requirements and conditional requirements live in each task's own validation and in the field
descriptions; `docket validate` is the authority.

## Identity

`identity.keys` are the recipe keys that decide *which* resource the task manages, in the order a
[resource address](task-envelope.md#names-and-resource-addresses) renders them. A key holding its
zero value is omitted from the address, which is what lets the mutually exclusive `app` / `global`
pair produce two different addresses for the same task type.

It is the same list as the fields carrying `"identity": "key"`, in declaration order, and is
repeated at the top level because building an address is the main thing a consumer does with it.

`identity.collections` names the collections the task manages wholesale, and how one entry in each
is identified.

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
"dynamic": [ { "prefix": "dns-provider-", "probeable": true, "sensitive": true } ]
```

`probeable: false` means docket cannot read those values back, so a recipe using that family
reports drift on every run and never converges.

`sensitive: true` means the members hold secrets: docket masks them in the command echo and lifts
them into an input on export. It is independent of `probeable` - a credential docket cannot read
back is still masked on the way out.

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
come from whichever datastore plugin backs the service, and no plugin exposes a report that lists
them.

## Stability

- Tasks are sorted by `type`, including when `--task` narrows the catalog, so the output never
  depends on the order the flags were given; properties by `name`; dynamic and rejected families by
  `prefix`.
- Fields, identity keys, requirements and choices keep declaration order, because that order is
  meaningful.
- Two runs of the same binary produce byte-identical output, for the whole catalog and for any
  `--task` selection.

## See also

- [Command reference](command-reference.md#docket-schema) - the command and its flags
- [Tasks](tasks/README.md) - the same information rendered for a human
- [JSON output](json-output.md) - the `--json` event streams, a separate format
- [Writing tasks](writing-tasks.md) - the declarations a task makes to fill the catalog
