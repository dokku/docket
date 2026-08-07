#!/usr/bin/env bats

load test_helper

setup() {
  require_dokku
  docket_build
  dokku_clean_app docket-test-export
}

teardown() {
  dokku_clean_app docket-test-export
}

@test "docket export fails on a nonexistent --app and writes nothing" {
  run "$(docket_bin)" export --app docket-nonexistent-xyz --output "$BATS_TEST_TMPDIR/tasks.yml"
  assert_failure
  assert_output --partial "not found on server"
  run test -f "$BATS_TEST_TMPDIR/tasks.yml"
  assert_failure
}

@test "docket export writes existing apps but fails when an --app is missing" {
  dokku apps:create docket-test-export
  run "$(docket_bin)" export --app docket-test-export --app docket-nonexistent-xyz --output "$BATS_TEST_TMPDIR/tasks.yml"
  assert_failure
  assert_output --partial "not found on server"
  run cat "$BATS_TEST_TMPDIR/tasks.yml"
  assert_success
  assert_output --partial "docket-test-export"
}

@test "docket export --app reports the app count without the global play" {
  dokku apps:create docket-test-export
  run "$(docket_bin)" export --app docket-test-export --output "$BATS_TEST_TMPDIR/tasks.yml"
  assert_success
  assert_output --partial "(1 app)"
}

@test "docket export --format json5 writes a tasks.json / tasks.vars.json pair" {
  dokku apps:create docket-test-export
  # A config value is what gets lifted into the vars-file; without one
  # the export has no vars and writes no companion file at all.
  dokku config:set --no-restart docket-test-export API_KEY=abc123
  cd "$BATS_TEST_TMPDIR"
  run "$(docket_bin)" export --app docket-test-export --format json5
  assert_success
  assert [ -f tasks.json ]
  assert [ -f tasks.vars.json ]
  assert [ ! -f tasks.yml ]

  run head -1 tasks.json
  assert_output "["

  run "$(docket_bin)" validate --tasks tasks.json --vars-file tasks.vars.json
  assert_success
  assert_output --partial "is valid"
}

@test "docket export --output - --format json5 round-trips into apply" {
  dokku apps:create docket-test-export
  # The motivating command from #410, with --list-tasks so the pipe is
  # exercised without applying anything back to the server.
  run bash -c "\"$(docket_bin)\" export --app docket-test-export --output - --format json5 | \"$(docket_bin)\" apply --tasks-format json5 --list-tasks -"
  assert_success
  assert_output --partial "docket-test-export"
}
