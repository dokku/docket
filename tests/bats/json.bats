#!/usr/bin/env bats

load test_helper

setup() {
  require_dokku
  docket_build
  dokku_clean_app docket-test-json
  dokku_clean_app docket-test-json-clean
  dokku_clean_app docket-test-json-drift
  dokku_clean_app docket-test-json-mut
}

teardown() {
  dokku_clean_app docket-test-json
  dokku_clean_app docket-test-json-clean
  dokku_clean_app docket-test-json-drift
  dokku_clean_app docket-test-json-mut
}

@test "docket apply --json emits valid JSON-lines" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-json
      dokku_app:
        app: docket-test-json
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --json
  assert_success
  while IFS= read -r line; do
    [ -z "$line" ] && continue
    echo "$line" | jq . >/dev/null || fail "invalid JSON: $line"
  done <<<"$output"
}

@test "docket apply --json includes version 1 on every event" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-json
      dokku_app:
        app: docket-test-json
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --json
  assert_success
  while IFS= read -r line; do
    [ -z "$line" ] && continue
    [ "$(echo "$line" | jq -r .version)" = "1" ] || fail "missing or wrong version: $line"
  done <<<"$output"
}

@test "docket apply --json emits a play_start event before the first task" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-json
      dokku_app:
        app: docket-test-json
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --json
  assert_success
  first_type="$(echo "$output" | head -n1 | jq -r .type)"
  [ "$first_type" = "play_start" ] || fail "first event was $first_type, want play_start"
}

@test "docket apply --json emits a summary event last" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-json
      dokku_app:
        app: docket-test-json
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --json
  assert_success
  last_type="$(echo "$output" | tail -n1 | jq -r .type)"
  [ "$last_type" = "summary" ] || fail "last event was $last_type, want summary"
}

@test "docket apply --json task events use changed not would_change" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-json
      dokku_app:
        app: docket-test-json
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --json
  assert_success
  task_event="$(echo "$output" | jq -c 'select(.type == "task")' | head -n1)"
  [ -n "$task_event" ] || fail "no task event found"
  echo "$task_event" | jq -e '.changed != null' >/dev/null || fail "missing changed field: $task_event"
  echo "$task_event" | jq -e '.would_change == null' >/dev/null || fail "would_change should not be set on apply: $task_event"
}

@test "docket apply --json with --verbose still emits only JSON on stdout" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-json
      dokku_app:
        app: docket-test-json
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --json --verbose
  assert_success
  refute_output --partial $'\u2192 dokku'
  while IFS= read -r line; do
    [ -z "$line" ] && continue
    echo "$line" | jq . >/dev/null || fail "non-JSON on stdout: $line"
  done <<<"$output"
}

@test "docket plan --json includes per-task mutations array" {
  write_tasks_file create.yml <<EOF
---
- tasks:
    - dokku_app:
        app: docket-test-json-mut
EOF
  "$(docket_bin)" apply --tasks "$TASKS_FILE"

  write_tasks_file plan.yml <<EOF
---
- tasks:
    - name: configure
      dokku_config:
        app: docket-test-json-mut
        restart: false
        config:
          KEY_ONE: value-one
          KEY_TWO: value-two
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --json
  assert_success
  assert_output --partial '"mutations":'
  task_event="$(echo "$output" | jq -c 'select(.type == "task" and .would_change == true)' | head -n1)"
  [ -n "$task_event" ] || fail "no drift task event found"
  count="$(echo "$task_event" | jq '.mutations | length')"
  [ "$count" = "2" ] || fail "expected 2 mutations, got $count: $task_event"
}

@test "docket plan --json includes commands array on drift" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_app:
        app: docket-test-json-drift
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --json
  assert_success
  task_event="$(echo "$output" | jq -c 'select(.type == "task" and .would_change == true)' | head -n1)"
  [ -n "$task_event" ] || fail "no drift task event found"
  cmd_count="$(echo "$task_event" | jq '.commands | length')"
  [ "$cmd_count" -ge 1 ] || fail "expected at least 1 command, got $cmd_count: $task_event"
  echo "$task_event" | jq -e '.commands[0] | contains("apps:create")' >/dev/null || fail "command should mention apps:create: $task_event"
}

@test "docket plan --detailed-exitcode returns 0 when in sync" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_app:
        app: docket-test-json-clean
EOF
  "$(docket_bin)" apply --tasks "$TASKS_FILE"
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --detailed-exitcode
  assert_success
}

@test "docket plan --detailed-exitcode returns 2 when drift detected" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_app:
        app: docket-test-json-drift
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --detailed-exitcode
  [ "$status" -eq 2 ] || fail "expected exit 2 on drift, got $status: $output"
}

@test "docket plan default exit code stays 0 even with drift" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_app:
        app: docket-test-json-drift
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
}

@test "docket plan --json --detailed-exitcode composes" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_app:
        app: docket-test-json-drift
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --json --detailed-exitcode
  [ "$status" -eq 2 ] || fail "expected exit 2 on drift, got $status"
  while IFS= read -r line; do
    [ -z "$line" ] && continue
    echo "$line" | jq . >/dev/null || fail "invalid JSON: $line"
  done <<<"$output"
}

@test "docket apply --json masks sensitive values in commands" {
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - name: ensure docket-test-json
      dokku_app:
        app: docket-test-json
    - name: set the secret
      dokku_config:
        app: docket-test-json
        config:
          MY_SECRET: "{{ .secret_value }}"
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --secret_value=topsecret999 --json
  assert_success
  refute_output --partial "topsecret999"
  assert_output --partial "***"
}
