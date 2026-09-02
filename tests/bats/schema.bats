#!/usr/bin/env bats

load test_helper

# docket schema is offline by contract - no subprocess, no server, no recipe -
# so nothing here calls require_dokku.

setup() {
  docket_build
}

@test "docket schema emits a parseable catalog" {
  run "$(docket_bin)" schema
  assert_success
  echo "$output" | jq -e . >/dev/null || fail "output is not valid JSON"
  echo "$output" | jq -e '.version == 1' >/dev/null || fail "expected version 1"
  echo "$output" | jq -e '(.tasks | length) > 0' >/dev/null || fail "expected at least one task"
}

@test "docket schema describes every task with a synopsis and fields" {
  run "$(docket_bin)" schema
  assert_success
  echo "$output" |
    jq -e 'all(.tasks[]; (.type | length > 0) and (.synopsis | length > 0) and (.fields | length > 0))' >/dev/null ||
    fail "a task is missing its type, synopsis, or fields"
  echo "$output" |
    jq -e 'all(.tasks[]; (.export.status | IN("supported", "partial", "unsupported")))' >/dev/null ||
    fail "a task has an unexpected export status"
  echo "$output" |
    jq -e 'all(.tasks[]; (.probe.status | IN("supported", "partial", "unsupported")))' >/dev/null ||
    fail "a task has an unexpected probe status"
}

@test "docket schema lists tasks sorted by type" {
  run "$(docket_bin)" schema
  assert_success
  echo "$output" | jq -e '[.tasks[].type] == ([.tasks[].type] | sort)' >/dev/null ||
    fail "tasks are not sorted by type"
}

@test "docket schema describes a collection field's element shape" {
  run "$(docket_bin)" schema
  assert_success

  # `list` alone cannot tell a list of strings from a list of structured
  # entries, which is the gap the item schema closes.
  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_ports") | .fields[] | select(.name == "port_mappings")
           | .type == "list" and .item.type == "object"
             and ([.item.fields[].name] == ["scheme", "host", "container"])' >/dev/null ||
    fail "dokku_ports port_mappings does not describe its item shape"

  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_config") | .fields[] | select(.name == "config")
           | .type == "dict" and .item.key_type == "string" and .item.type == "string"' >/dev/null ||
    fail "dokku_config config does not describe its value type"
}

@test "docket schema marks sensitive fields" {
  run "$(docket_bin)" schema
  assert_success
  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_git_from_image") | .fields[] | select(.name == "image") | .sensitive == true' >/dev/null ||
    fail "dokku_git_from_image image is not marked sensitive"

  # letsencrypt is the one property task declared over SensitivePropertyFields
  # rather than PropertyFields, and the whole difference between the two types
  # is this flag.
  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_letsencrypt_property") | .fields[] | select(.name == "value") | .sensitive == true' >/dev/null ||
    fail "dokku_letsencrypt_property value is not marked sensitive"
}

@test "docket schema publishes the whole shape of a property task (#454)" {
  run "$(docket_bin)" schema
  assert_success

  # The property tasks declare their fields once, as `type XPropertyTask
  # PropertyFields`. A recipe key that went missing from the published contract
  # would be invisible in the Go tests that read the same struct, so assert the
  # five keys and their tags from the outside.
  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_builds_property")
           | [.fields[].name] == ["app", "global", "property", "value", "state"]' >/dev/null ||
    fail "dokku_builds_property does not publish all five recipe keys in order"

  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_builds_property")
           | ([.fields[] | select(.identity == "key") | .name] == ["app", "global", "property"])
             and ([.fields[] | select(.required) | .name] == ["property"])' >/dev/null ||
    fail "dokku_builds_property does not key on app, global and property"

  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_builds_property") | .fields[] | select(.name == "state")
           | .default == "present" and .choices == ["present", "absent"]' >/dev/null ||
    fail "dokku_builds_property state lost its default or its choices"
}

@test "docket schema publishes the whole shape of a toggle task (#467)" {
  run "$(docket_bin)" schema
  assert_success

  # The toggle tasks declare their fields once, as `type XToggleTask
  # ToggleFields`. A recipe key that went missing from the published contract
  # would be invisible in the Go tests that read the same struct, so assert the
  # two keys and their tags from the outside.
  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_checks_toggle")
           | [.fields[].name] == ["app", "state"]' >/dev/null ||
    fail "dokku_checks_toggle does not publish both recipe keys in order"

  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_checks_toggle")
           | ([.fields[] | select(.identity == "key") | .name] == ["app"])
             and ([.fields[] | select(.required) | .name] == ["app"])' >/dev/null ||
    fail "dokku_checks_toggle does not key on app alone"

  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_checks_toggle") | .fields[] | select(.name == "state")
           | .default == "present" and .choices == ["present", "absent"]' >/dev/null ||
    fail "dokku_checks_toggle state lost its default or its choices"

  # dokku_maintenance is a toggle that is not named like one, so nothing but an
  # assertion keeps it on the shared field set.
  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_maintenance")
           | [.fields[].name] == ["app", "state"]' >/dev/null ||
    fail "dokku_maintenance does not publish the toggle recipe keys"
}

@test "docket schema publishes a property task's supported property names" {
  run "$(docket_bin)" schema
  assert_success

  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_apps_property") | .property_schema
           | .plugin == "apps" and .subcommand == "apps:set" and .field == "property"' >/dev/null ||
    fail "dokku_apps_property does not publish its property table"

  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_apps_property") | .property_schema.properties[]
           | select(.name == "disable-autocreation") | .scopes == ["global"]' >/dev/null ||
    fail "disable-autocreation is not published as global-only"
}

@test "docket schema publishes property name families it cannot enumerate" {
  run "$(docket_bin)" schema
  assert_success
  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_letsencrypt_property") | .property_schema.dynamic[]
           | select(.prefix == "dns-provider-")
           | .probeable == true and .sensitive == true and .scopes == ["app", "global"]' >/dev/null ||
    fail "letsencrypt does not publish its dns-provider- family"

  # traefik holds the same credentials and reports them the same way, but
  # traefik:set refuses the family outside --global, so it is published
  # global-only (#450). A consumer that ignored scopes would accept a recipe
  # dokku rejects.
  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_traefik_property") | .property_schema.dynamic[]
           | select(.prefix == "dns-provider-")
           | .probeable == true and .sensitive == true and .scopes == ["global"]' >/dev/null ||
    fail "traefik does not publish its dns-provider- family as global-only"
}

@test "docket schema publishes property name families a task refuses (#458)" {
  run "$(docket_bin)" schema
  assert_success

  # chart.* is absent from scheduler-k3s's properties for a different reason
  # than a typo is. Publishing the replacement lets a consumer validating
  # offline answer the way docket does, instead of reporting an unknown name.
  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_scheduler_k3s_property") | .property_schema.rejected[]
           | select(.prefix == "chart.")
           | .replacement == "dokku_scheduler_k3s_chart" and (.reason | length) > 0' >/dev/null ||
    fail "scheduler-k3s does not publish its rejected chart. family"

  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_scheduler_k3s_property") | .property_schema.properties
           | map(select(.name | startswith("chart."))) | length == 0' >/dev/null ||
    fail "scheduler-k3s publishes a chart. property as supported and rejected at once"

  # The replacement always names a task the same catalog describes.
  echo "$output" |
    jq -e '[.tasks[].type] as $types
           | [.tasks[] | select(has("property_schema")) | .property_schema.rejected // []
              | .[].replacement] | all(. as $r | $types | index($r) != null)' >/dev/null ||
    fail "a rejected family points at a task the catalog does not describe"

  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_apps_property") | .property_schema | has("rejected") | not' >/dev/null ||
    fail "a table with nothing to refuse should omit the rejected key"
}

@test "docket schema omits a property schema when the names are not enumerable" {
  run "$(docket_bin)" schema
  assert_success

  # dokku_service_property takes a property field, but the legal names come
  # from whichever datastore plugin backs the service. Publishing an empty
  # table would read as "no properties" rather than "docket cannot know".
  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_service_property") | has("property_schema") | not' >/dev/null ||
    fail "dokku_service_property should not publish a property schema"
}

@test "docket schema carries each task's documented examples" {
  run "$(docket_bin)" schema
  assert_success
  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_app") | .examples | length > 0' >/dev/null ||
    fail "dokku_app publishes no examples"
  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_app") | .examples[0].yaml | test("dokku_app:")' >/dev/null ||
    fail "dokku_app example does not name the task type"
}

@test "docket schema output is byte-stable across runs" {
  "$(docket_bin)" schema >"$BATS_TEST_TMPDIR/first.json"
  "$(docket_bin)" schema >"$BATS_TEST_TMPDIR/second.json"
  run diff "$BATS_TEST_TMPDIR/first.json" "$BATS_TEST_TMPDIR/second.json"
  assert_success
}

@test "docket schema --output writes the catalog to a file" {
  run "$(docket_bin)" schema --output "$BATS_TEST_TMPDIR/catalog.json"
  assert_success
  assert_output ""
  jq -e . "$BATS_TEST_TMPDIR/catalog.json" >/dev/null || fail "the written file is not valid JSON"
}

@test "docket schema rejects a positional argument" {
  run "$(docket_bin)" schema dokku_app
  assert_failure
  assert_output --partial "takes no arguments"
}

@test "docket schema --task narrows the catalog to the named types" {
  run "$(docket_bin)" schema --task dokku_config --task dokku_domains
  assert_success
  echo "$output" | jq -e '.version == 1' >/dev/null || fail "expected version 1"
  echo "$output" | jq -e '[.tasks[].type] == ["dokku_config", "dokku_domains"]' >/dev/null ||
    fail "expected only the two named task types"
}

@test "docket schema --task keeps the document shape" {
  run "$(docket_bin)" schema --task dokku_config
  assert_success

  # The narrowed document is the same shape as the whole catalog, so a
  # consumer parses one format either way and the published JSON Schema
  # keeps covering it.
  echo "$output" | jq -e 'has("version") and has("tasks")' >/dev/null ||
    fail "narrowed output is missing version or tasks"
  echo "$output" |
    jq -e 'all(.tasks[]; (.type | length > 0) and (.synopsis | length > 0) and (.fields | length > 0))' >/dev/null ||
    fail "the narrowed entry is missing its type, synopsis, or fields"
}

@test "docket schema --task ignores the order the flags are given" {
  "$(docket_bin)" schema --task dokku_config --task dokku_domains >"$BATS_TEST_TMPDIR/forward.json"
  "$(docket_bin)" schema --task dokku_domains --task dokku_config >"$BATS_TEST_TMPDIR/reverse.json"
  run diff "$BATS_TEST_TMPDIR/forward.json" "$BATS_TEST_TMPDIR/reverse.json"
  assert_success
}

@test "docket schema --task rejects an unknown task type" {
  run "$(docket_bin)" schema --task dokku_confg
  assert_failure
  assert_output --partial 'did you mean "dokku_config"'
}

@test "docket schema --task writes a narrowed catalog to a file" {
  run "$(docket_bin)" schema --task dokku_config --output "$BATS_TEST_TMPDIR/catalog.json"
  assert_success
  assert_output ""
  jq -e '(.tasks | length) == 1' "$BATS_TEST_TMPDIR/catalog.json" >/dev/null ||
    fail "the written file is not narrowed to one task"
}
