#!/usr/bin/env bats

load test_helper

# docket schema is offline by contract, so nothing here calls require_dokku.

setup() {
  docket_build
}

@test "docket schema publishes typed field defaults (#645)" {
  run "$(docket_bin)" schema
  assert_success

  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_ps_scale") | .fields[] | select(.name == "skip_deploy")
           | .default == false and (.default | type) == "boolean"' >/dev/null ||
    fail "dokku_ps_scale skip_deploy default is not the JSON boolean false"

  echo "$output" |
    jq -e '.tasks[] | select(.type == "dokku_config") | .fields[] | select(.name == "restart")
           | .default == true' >/dev/null ||
    fail "dokku_config restart default is not the JSON boolean true"

  echo "$output" |
    jq -e 'all(.tasks[].fields[] | select(has("default"));
               (.type == "bool" and (.default | type) == "boolean")
               or (.type == "string" and (.default | type) == "string")
               or ((.type == "int" or .type == "float") and (.default | type) == "number")
               or (.type == "list" and (.default | type) == "array"))' >/dev/null ||
    fail "a published default does not match its field's type"
}
