#!/usr/bin/env bats

load test_helper

# register.bats covers the #210 envelope features end-to-end against
# the docket binary: register: cross-task data flow, changed_when /
# failed_when overrides, and ignore_errors fatal-exit suppression.
#
# Tests that mutate live Dokku state gate on require_dokku; the
# validate-only tests run anywhere because validate is offline by
# contract.

setup() {
  docket_build
}

teardown() {
  dokku_clean_app docket-test-register
  dokku_clean_app docket-test-changed-when
  dokku_clean_app docket-test-ignore-errors-after
}

@test "register: makes prior task result available to follow-up when:" {
  require_dokku
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure app
      register: app_result
      dokku_app:
        app: docket-test-register
    - name: only on first run
      when: 'registered.app_result.Changed'
      dokku_config:
        app: docket-test-register
        config:
          FIRST_RUN_FLAG: "true"
EOF

  # First run: app does not exist, so the create changes state and
  # the follow-up runs.
  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
  run dokku config:get docket-test-register FIRST_RUN_FLAG
  assert_output "true"

  # Second run: the app already exists, so register's Changed is
  # false and the follow-up skips. The flag should not be re-set.
  dokku config:unset docket-test-register FIRST_RUN_FLAG
  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
  run dokku config:get docket-test-register FIRST_RUN_FLAG
  refute_output "true"
}

@test "changed_when: false silences a self-reported-changed task" {
  require_dokku
  write_tasks_file <<EOF
---
- tasks:
    - name: stamp
      changed_when: 'false'
      dokku_config:
        app: docket-test-changed-when
        config:
          LAST_PING: "now"
EOF
  # Pre-create the app so dokku_config has somewhere to write.
  dokku apps:create docket-test-changed-when

  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "[ok]"
  refute_output --partial "[changed]"
}

@test "failed_when: clears the error when stderr matches a known pattern" {
  require_dokku
  write_tasks_file <<EOF
---
- tasks:
    - name: tolerant probe
      failed_when: 'result.Error != nil and not (result.Stderr contains "does not exist")'
      dokku_config:
        app: nonexistent-app-zzz-docket-test
        state: present
        config:
          K: v
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
}

@test "ignore_errors: true continues past errors and exits 0" {
  require_dokku
  write_tasks_file <<EOF
---
- tasks:
    - name: this errors
      ignore_errors: true
      dokku_config:
        app: nonexistent-app-yyy-docket-test
        state: present
        config:
          K: v
    - name: this still runs
      dokku_app:
        app: docket-test-ignore-errors-after
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
  run dokku apps:exists docket-test-ignore-errors-after
  assert_success
}

@test "register: duplicate name surfaces a validate problem" {
  write_tasks_file <<EOF
---
- tasks:
    - register: dup
      dokku_app:
        app: a
    - register: dup
      dokku_app:
        app: b
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "register"
  assert_output --partial "already declared"
}
