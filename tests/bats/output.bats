#!/usr/bin/env bats

load test_helper

setup() {
  require_dokku
  docket_build
  dokku_clean_app docket-test-output
}

teardown() {
  dokku_clean_app docket-test-output
}

@test "docket apply prints structured per-task output" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-output
      dokku_app:
        app: docket-test-output
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "==> Play: tasks"
  assert_output --partial "[changed] "
  assert_output --partial "Summary:"
  assert_output --regexp "took [0-9]+\.[0-9]+s"
}

@test "docket apply on second run reports ok" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-output
      dokku_app:
        app: docket-test-output
EOF
  "$(docket_bin)" apply --tasks "$TASKS_FILE"
  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "[ok]"
  refute_output --partial "[changed]"
  assert_output --partial "0 changed"
}

@test "docket apply --verbose echoes resolved commands" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-output
      dokku_app:
        app: docket-test-output
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --verbose
  assert_success
  assert_output --partial "→ dokku"
  assert_output --partial "apps:create"
}

@test "docket apply --verbose echoes every command for multi-command tasks" {
  # dokku_buildpacks invokes `dokku buildpacks:add` once per buildpack URL,
  # which exercises the per-task multi-command append path. The URLs are
  # only recorded in the buildpacks file (no network fetch happens at
  # add-time) so this stays a pure docket+dokku test.
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-output
      dokku_app:
        app: docket-test-output
    - name: add buildpacks
      dokku_buildpacks:
        app: docket-test-output
        buildpacks:
          - https://github.com/heroku/heroku-buildpack-nodejs.git
          - https://github.com/heroku/heroku-buildpack-nginx.git
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --verbose
  assert_success
  assert_output --partial "→ dokku"
  assert_output --partial "buildpacks:add"
  assert_output --partial "heroku-buildpack-nodejs"
  assert_output --partial "heroku-buildpack-nginx"
  # One -> line per buildpack add.
  local arrow_count
  arrow_count="$(printf '%s\n' "${output}" | grep -c "→ dokku.*buildpacks:add" || true)"
  if [ "${arrow_count}" -lt 2 ]; then
    fail "expected at least 2 '→ dokku ... buildpacks:add' continuation lines, got ${arrow_count} in:${output}"
  fi
}

@test "docket apply on error surfaces the failure" {
  write_tasks_file <<EOF
---
- tasks:
    - name: add ports to a missing app
      dokku_ports:
        app: docket-test-output-missing
        port_mappings:
          - { scheme: http, host_port: 80, container_port: 5000 }
        state: present
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "[error]"
  assert_output --partial "          !"
  assert_output --partial "1 error"
}

@test "docket apply --json on error emits exit_code, stderr" {
  write_tasks_file <<EOF
---
- tasks:
    - name: add ports to a missing app
      dokku_ports:
        app: docket-test-output-missing
        port_mappings:
          - { scheme: http, host_port: 80, container_port: 5000 }
        state: present
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --json
  assert_failure
  local task_event
  task_event="$(printf '%s\n' "${output}" | jq -c 'select(.type=="task" and .status=="error")' | head -1)"
  if [ -z "${task_event}" ]; then
    fail "expected an errored task event in:${output}"
  fi
  local exit_code
  exit_code="$(printf '%s' "${task_event}" | jq -r '.exit_code // empty')"
  if [ -z "${exit_code}" ] || [ "${exit_code}" = "0" ]; then
    fail "expected non-zero exit_code on errored task event, got '${exit_code}' in: ${task_event}"
  fi
  local stderr
  stderr="$(printf '%s' "${task_event}" | jq -r '.stderr // empty')"
  if [ -z "${stderr}" ]; then
    fail "expected stderr on errored task event in: ${task_event}"
  fi
}

@test "docket apply respects NO_COLOR" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-output
      dokku_app:
        app: docket-test-output
EOF
  NO_COLOR=1 run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
  refute_output --partial $'\x1b['
}
