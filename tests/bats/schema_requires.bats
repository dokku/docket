#!/usr/bin/env bats

load test_helper

# docket schema is offline by contract, so nothing here calls require_dokku.

setup() {
  docket_build
}

@test "docket schema publishes the tasks an example requires, in apply order" {
  run "$(docket_bin)" schema --task dokku_service_link
  assert_success
  echo "$output" |
    jq -e '.tasks[0].examples[0].requires | map(.type) == ["dokku_plugin", "dokku_app", "dokku_service_create", "dokku_git_from_image"]' >/dev/null ||
    fail "the dokku_service_link example does not require its plugin, app, service and deploy in order"
  echo "$output" |
    jq -e '.tasks[0].examples[0].requires[2].yaml == "dokku_service_create:\n    service: redis\n    name: my-redis\n"' >/dev/null ||
    fail "the required service is not published as a task snippet"
}

@test "docket schema leaves requires off an example that needs nothing" {
  run "$(docket_bin)" schema --task dokku_app
  assert_success
  echo "$output" |
    jq -e '[.tasks[0].examples[] | has("requires")] | any | not' >/dev/null ||
    fail "a dokku_app example publishes requires"
}

@test "an example and its requires make a recipe docket validate accepts" {
  run "$(docket_bin)" schema --task dokku_http_auth_domain
  assert_success
  recipe="$BATS_TEST_TMPDIR/requires.yml"
  {
    echo "---"
    echo "- tasks:"
    echo "$output" | jq -r '.tasks[0].examples[0] | (.requires | map(.yaml)) + [.yaml] | .[]' |
      awk 'NF == 0 { next } /^[^ ]/ { print "    - " $0; next } { print "      " $0 }'
  } >"$recipe"
  run "$(docket_bin)" validate --tasks "$recipe"
  assert_success
}

@test "the reference page prints an example's requires with it" {
  page="$BATS_TEST_DIRNAME/../../docs/tasks/dokku_service_link.md"
  grep -qF "Requires these tasks, applied first:" "$page" || fail "dokku_service_link.md prints no requires"
  grep -qF -- "- dokku_service_create:" "$page" || fail "dokku_service_link.md does not list the service it requires"
}
