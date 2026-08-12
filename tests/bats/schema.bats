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
           | select(.prefix == "dns-provider-") | .probeable == true and .sensitive == true' >/dev/null ||
    fail "letsencrypt does not publish its dns-provider- family"

  # traefik holds the same credentials but does not report them, so the family
  # is sensitive without being probeable (#457).
  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_traefik_property") | .property_schema.dynamic[]
           | select(.prefix == "dns-provider-") | .probeable == false and .sensitive == true' >/dev/null ||
    fail "traefik does not publish its dns-provider- family as sensitive"
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
