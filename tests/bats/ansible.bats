#!/usr/bin/env bats
#
# The wrapper contract documented in docs/ansible-dokku.md (#409). These
# exercise the exact invocations that page tells an ansible-dokku wrapper
# to make, so a change in flag handling, payload parsing, or the
# validate --json shape breaks here rather than in the wrapper.
#
# Offline only: validate never contacts a server, and apply is exercised
# through --list-tasks. The live apply --detailed-exitcode cases live in
# json.bats, which already requires dokku.

load test_helper

setup() {
  docket_build
}

# The worked example from the "payload" section of the page: one play,
# one task, fully resolved, no inputs block.
payload() {
  cat <<'EOF'
[
  {
    "name": "dokku_config",
    "tasks": [
      {
        "name": "set config on api",
        "dokku_config": {
          "app": "api",
          "restart": true,
          "config": { "LOG_LEVEL": "info" },
          "state": "present"
        }
      }
    ]
  }
]
EOF
}

@test "the documented payload validates" {
  cd "$BATS_TEST_TMPDIR"
  payload >payload.json
  run bash -c "\"$(docket_bin)\" validate --tasks-format json5 - <payload.json"
  assert_success
  assert_output --partial "is valid"
}

@test "--tasks-format json is accepted as a synonym for json5" {
  cd "$BATS_TEST_TMPDIR"
  payload >payload.json
  run bash -c "\"$(docket_bin)\" validate --tasks-format json - <payload.json"
  assert_success
}

@test "validate --json prints nothing on a clean payload" {
  cd "$BATS_TEST_TMPDIR"
  payload >payload.json
  run bash -c "\"$(docket_bin)\" validate --tasks-format json5 --json - <payload.json"
  assert_success
  [ -z "$output" ] || fail "expected empty output on a clean recipe, got: $output"
}

@test "the documented payload resolves through apply --list-tasks --json" {
  cd "$BATS_TEST_TMPDIR"
  payload >payload.json
  run bash -c "\"$(docket_bin)\" apply --tasks-format json5 --list-tasks --json - <payload.json"
  assert_success
  while IFS= read -r line; do
    [ -z "$line" ] && continue
    echo "$line" | jq . >/dev/null || fail "invalid JSON: $line"
  done <<<"$output"
  echo "$output" | jq -e 'select(.type == "list_task") | .name == "set config on api"' >/dev/null ||
    fail "expected the task name echoed back: $output"
}

@test "a bad payload reports validate_problem events a wrapper can branch on" {
  cd "$BATS_TEST_TMPDIR"
  cat >bad.json <<'EOF'
[{"tasks": [{"name": "no app", "dokku_config": {"restart": true}}]}]
EOF
  run bash -c "\"$(docket_bin)\" validate --tasks-format json5 --json - <bad.json"
  assert_failure
  echo "$output" | jq -e '.type == "validate_problem"' >/dev/null || fail "expected a validate_problem: $output"
  echo "$output" | jq -e '.code == "missing_required_field"' >/dev/null || fail "expected missing_required_field: $output"
  echo "$output" | jq -e '.version == 1' >/dev/null || fail "expected version 1: $output"
}

@test "an unknown task type reports a did-you-mean hint" {
  cd "$BATS_TEST_TMPDIR"
  cat >typo.json <<'EOF'
[{"tasks": [{"name": "typo", "dokku_appp": {"app": "api"}}]}]
EOF
  run bash -c "\"$(docket_bin)\" validate --tasks-format json5 --json - <typo.json"
  assert_failure
  echo "$output" | jq -e '.code == "unknown_task_type"' >/dev/null || fail "expected unknown_task_type: $output"
  echo "$output" | jq -e '.hint | test("dokku_app")' >/dev/null || fail "expected a did-you-mean hint: $output"
}

@test "an unescaped {{ in a payload value is a render error, not silent corruption" {
  cd "$BATS_TEST_TMPDIR"
  cat >braces.json <<'EOF'
[{"tasks": [{"name": "braces", "dokku_config": {"app": "api", "config": {"MSG": "hello {{ world"}}}]}]
EOF
  run bash -c "\"$(docket_bin)\" validate --tasks-format json5 - <braces.json"
  assert_failure
  assert_output --partial "template render error"
}

@test "the documented backtick escape emits a literal {{ in a JSON payload" {
  cd "$BATS_TEST_TMPDIR"
  cat >escaped.json <<'EOF'
[{"tasks": [{"name": "v=[{{ `{{` }} .Values.name }}]", "dokku_app": {"app": "api"}}]}]
EOF
  run bash -c "\"$(docket_bin)\" apply --tasks-format json5 --list-tasks - <escaped.json"
  assert_success
  assert_output --partial 'v=[{{ .Values.name }}]'
}

# The host-directory half of a dokku_storage module call: one
# dokku_storage_entry per host directory, ahead of the mount tasks that
# reference it. `create_host_dir` plus `user`/`group` collapse into the
# entry's chown, which the wrapper must send as a single value.
@test "a storage host-directory payload validates" {
  cd "$BATS_TEST_TMPDIR"
  cat >storage.json <<'EOF'
[
  {
    "name": "dokku_storage",
    "tasks": [
      {
        "name": "create the host directory for hello-world",
        "dokku_storage_entry": {
          "name": "hello-world-data",
          "chown": "herokuish",
          "state": "present"
        }
      },
      {
        "name": "mount storage on hello-world",
        "dokku_storage_mount": {
          "app": "hello-world",
          "entry_name": "hello-world-data",
          "container_dir": "/data",
          "state": "present"
        }
      }
    ]
  }
]
EOF
  run bash -c "\"$(docket_bin)\" validate --tasks-format json5 - <storage.json"
  assert_success
  assert_output --partial "is valid"
}

# The other two pieces of a dokku_storage module call the wrapper used to do
# with raw filesystem calls: the unconditional `os.chmod(host_dir, 0o777)`
# becomes `mode`, and `destroy_host_dir: true` becomes the field of the same
# name on the `state: absent` branch.
@test "a storage host-directory mode and removal payload validates" {
  cd "$BATS_TEST_TMPDIR"
  cat >storage.json <<'EOF'
[
  {
    "name": "dokku_storage",
    "tasks": [
      {
        "name": "create the host directory for hello-world",
        "dokku_storage_entry": {
          "name": "hello-world-data",
          "chown": "herokuish",
          "mode": "0777",
          "state": "present"
        }
      },
      {
        "name": "remove the host directory for hello-world",
        "dokku_storage_entry": {
          "name": "hello-world-scratch",
          "destroy_host_dir": true,
          "state": "absent"
        }
      }
    ]
  }
]
EOF
  run bash -c "\"$(docket_bin)\" validate --tasks-format json5 - <storage.json"
  assert_success
  assert_output --partial "is valid"
}

@test "a bad storage mode reports a validate_problem a wrapper can branch on" {
  cd "$BATS_TEST_TMPDIR"
  cat >storage.json <<'EOF'
[{"tasks": [{"name": "bad mode", "dokku_storage_entry": {"name": "hello-world-data", "mode": "0888"}}]}]
EOF
  run bash -c "\"$(docket_bin)\" validate --tasks-format json5 --json - <storage.json"
  assert_failure
  echo "$output" | jq -e '.code == "invalid_task_input"' >/dev/null || fail "expected invalid_task_input: $output"
  echo "$output" | jq -e '.message | test("mode")' >/dev/null || fail "expected the message to name mode: $output"
}

@test "destroy_host_dir outside state absent reports a validate_problem" {
  cd "$BATS_TEST_TMPDIR"
  cat >storage.json <<'EOF'
[{"tasks": [{"name": "misplaced flag", "dokku_storage_entry": {"name": "hello-world-data", "destroy_host_dir": true}}]}]
EOF
  run bash -c "\"$(docket_bin)\" validate --tasks-format json5 --json - <storage.json"
  assert_failure
  echo "$output" | jq -e '.code == "invalid_task_input"' >/dev/null || fail "expected invalid_task_input: $output"
  echo "$output" | jq -e '.message | test("destroy_host_dir")' >/dev/null || fail "expected the message to name destroy_host_dir: $output"
}

@test "a bad storage chown reports a validate_problem a wrapper can branch on" {
  cd "$BATS_TEST_TMPDIR"
  cat >storage.json <<'EOF'
[{"tasks": [{"name": "bad chown", "dokku_storage_entry": {"name": "hello-world-data", "chown": "packeto"}}]}]
EOF
  run bash -c "\"$(docket_bin)\" validate --tasks-format json5 --json - <storage.json"
  assert_failure
  echo "$output" | jq -e '.code == "invalid_task_input"' >/dev/null || fail "expected invalid_task_input: $output"
  echo "$output" | jq -e '.message | test("chown")' >/dev/null || fail "expected the message to name chown: $output"
}

@test "a load-time failure writes no JSON to stdout" {
  cd "$BATS_TEST_TMPDIR"
  cat >typo.json <<'EOF'
[{"tasks": [{"name": "typo", "dokku_appp": {"app": "api"}}]}]
EOF
  run bash -c "\"$(docket_bin)\" apply --tasks-format json5 --json - <typo.json 2>/dev/null"
  assert_failure
  [ -z "$output" ] || fail "expected empty stdout on a load-time failure, got: $output"
}
