#!/usr/bin/env bats

load test_helper

# Flag conflicts are resolved before export contacts the server, so those
# tests stay ungated; everything that reads a real server calls
# require_dokku as the first line of its body.
setup() {
  docket_build
  dokku_clean_app docket-test-export
}

teardown() {
  dokku_clean_app docket-test-export
}

@test "docket export fails on a nonexistent --app and writes nothing" {
  require_dokku
  run "$(docket_bin)" export --app docket-nonexistent-xyz --output "$BATS_TEST_TMPDIR/tasks.yml"
  assert_failure
  assert_output --partial "not found on server"
  run test -f "$BATS_TEST_TMPDIR/tasks.yml"
  assert_failure
}

@test "docket export writes existing apps but fails when an --app is missing" {
  require_dokku
  dokku apps:create docket-test-export
  run "$(docket_bin)" export --app docket-test-export --app docket-nonexistent-xyz --output "$BATS_TEST_TMPDIR/tasks.yml"
  assert_failure
  assert_output --partial "not found on server"
  run cat "$BATS_TEST_TMPDIR/tasks.yml"
  assert_success
  assert_output --partial "docket-test-export"
}

@test "docket export --app reports the app count without the global play" {
  require_dokku
  dokku apps:create docket-test-export
  run "$(docket_bin)" export --app docket-test-export --output "$BATS_TEST_TMPDIR/tasks.yml"
  assert_success
  assert_output --partial "(1 app)"
}

@test "docket export --format json5 writes a tasks.json / tasks.vars.json pair" {
  require_dokku
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
  require_dokku
  dokku apps:create docket-test-export
  # The motivating command from #410, with --list-tasks so the pipe is
  # exercised without applying anything back to the server.
  run bash -c "\"$(docket_bin)\" export --app docket-test-export --output - --format json5 | \"$(docket_bin)\" apply --tasks-format json5 --list-tasks -"
  assert_success
  assert_output --partial "docket-test-export"
}

@test "docket export --output - rejects --vars-output" {
  # #419: a streamed recipe inlines its values, so the vars-file the flag
  # asks for can never be written. Rejected before the server is read,
  # which is why this needs no dokku.
  cd "$BATS_TEST_TMPDIR"
  run "$(docket_bin)" export --output - --vars-output secrets.yml
  assert_failure
  assert_output --partial "--vars-output cannot be used with --output -"
  assert [ ! -f secrets.yml ]
}

@test "docket export --output - rejects --overwrite" {
  cd "$BATS_TEST_TMPDIR"
  run "$(docket_bin)" export --output - --overwrite
  assert_failure
  assert_output --partial "--overwrite cannot be used with --output -"
  assert [ ! -f tasks.yml ]
}

@test "docket export captures http-auth state after the users" {
  require_plugin http-auth
  dokku apps:create docket-test-export
  dokku http-auth:enable docket-test-export testuser testpass
  cd "$BATS_TEST_TMPDIR"
  run "$(docket_bin)" export --app docket-test-export --output tasks.yml --vars-output tasks.vars.yml
  assert_success

  run grep -c 'dokku_http_auth:' tasks.yml
  assert_output "1"
  run grep -c 'dokku_http_auth_user:' tasks.yml
  assert_output "1"

  # add-user writes enabled=true as a side effect, so the task owning the
  # enabled flag has to come last for the recipe's stated state to stick.
  enable_line="$(grep -n 'dokku_http_auth:' tasks.yml | cut -d: -f1)"
  users_line="$(grep -n 'dokku_http_auth_user:' tasks.yml | cut -d: -f1)"
  assert [ "$enable_line" -gt "$users_line" ]

  # The enable task carries no credentials, which only validates because the
  # plugin takes them as optional trailing arguments.
  run "$(docket_bin)" validate --tasks tasks.yml --vars-file tasks.vars.yml
  assert_success
  assert_output --partial "is valid"
}

@test "docket export reports a --vars-output left unwritten for want of secrets" {
  require_dokku
  # No config:set, so nothing is sensitive and no vars-file is produced.
  dokku apps:create docket-test-export
  cd "$BATS_TEST_TMPDIR"
  run "$(docket_bin)" export --app docket-test-export --output tasks.yml --vars-output secrets.yml
  assert_success
  assert_output --partial "no sensitive values to export"
  assert_output --partial "secrets.yml"
  assert [ -f tasks.yml ]
  assert [ ! -f secrets.yml ]
}
