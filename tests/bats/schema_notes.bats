#!/usr/bin/env bats

load test_helper

# docket schema is offline by contract, so nothing here calls require_dokku.

setup() {
  docket_build
}

@test "docket schema publishes each task's notes as its reference page prints them" {
  run "$(docket_bin)" schema --task dokku_domains
  assert_success
  page="$BATS_TEST_DIRNAME/../../docs/tasks/dokku_domains.md"
  for kind in export probe identity objects constraints; do
    markdown="$(echo "$output" | jq -r --arg k "$kind" '.tasks[0].notes[] | select(.kind == $k) | .markdown')"
    [[ -n "$markdown" ]] || fail "dokku_domains publishes no $kind note"
    grep -qF -- "$(echo "$markdown" | head -n 1)" "$page" || fail "the $kind note heading is not on the page"
  done
}

@test "docket schema keys state-dependent field notes by state" {
  run "$(docket_bin)" schema --task dokku_domains
  assert_success
  echo "$output" |
    jq -e '.tasks[0].fields[] | select(.name == "domains") | .note.suffix == " (required when state is present or absent) (must not be set when state is clear)"' >/dev/null ||
    fail "domains does not publish its note suffix"
  echo "$output" |
    jq -e '.tasks[0].fields[] | select(.name == "domains") | .note.states == {"present": " (required)", "absent": " (required)", "set": "", "clear": " (must not be set)"}' >/dev/null ||
    fail "domains does not key its note by state"
  echo "$output" |
    jq -e '.tasks[0].fields[] | select(.name == "app") | has("note") | not' >/dev/null ||
    fail "a field with nothing to append publishes a note"
}

@test "docket schema keys a state-dependent task note by state" {
  run "$(docket_bin)" schema --task dokku_app
  assert_success
  echo "$output" |
    jq -e '.tasks[0].notes[] | select(.kind == "objects") | (.states.present | contains("Creates it.")) and (.states.absent | contains("Destroys it."))' >/dev/null ||
    fail "dokku_app does not key its objects note by state"
  echo "$output" |
    jq -e '.tasks[0].notes[] | select(.kind == "identity") | has("states") | not' >/dev/null ||
    fail "a note that does not depend on the state is keyed by state"
}
