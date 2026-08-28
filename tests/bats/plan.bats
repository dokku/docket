#!/usr/bin/env bats

load test_helper

setup() {
  require_dokku
  docket_build
  dokku_clean_app docket-test-plan
  dokku_clean_storage_entry docket-test-plan-data
}

teardown() {
  dokku_clean_app docket-test-plan
  dokku_clean_storage_entry docket-test-plan-data
}

@test "docket plan reports drift on a missing app" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-plan
      dokku_app:
        app: docket-test-plan
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "==> Play: tasks"
  assert_output --partial "[+]"
  assert_output --partial "Plan:"
  assert_output --partial "1 would change"
}

@test "docket plan does not mutate state" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-plan
      dokku_app:
        app: docket-test-plan
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  run dokku apps:exists docket-test-plan
  # apps:exists returns non-zero when the app does not exist.
  assert_failure
}

@test "docket plan reports in sync after apply" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-plan
      dokku_app:
        app: docket-test-plan
EOF
  "$(docket_bin)" apply --tasks "$TASKS_FILE"
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "[ok]"
  assert_output --partial "in sync"
  assert_output --partial "0 would change"
}

@test "docket plan itemizes config keys to set" {
  write_tasks_file setup.yml <<EOF
---
- tasks:
    - dokku_app:
        app: docket-test-plan
EOF
  "$(docket_bin)" apply --tasks "$TASKS_FILE"

  write_tasks_file <<EOF
---
- tasks:
    - name: configure
      dokku_config:
        app: docket-test-plan
        restart: false
        config:
          KEY_ONE: value-one
          KEY_TWO: value-two
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "2 key(s) to set"
  assert_output --partial "set KEY_ONE"
  assert_output --partial "set KEY_TWO"
}

@test "docket apply is idempotent on second run" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-plan
      dokku_app:
        app: docket-test-plan
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
  run dokku apps:exists docket-test-plan
  assert_success

  # Second apply must not report any error and must not destroy the app.
  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
  run dokku apps:exists docket-test-plan
  assert_success
}

@test "docket plan reports drift when a storage entry's chown changes" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-plan-data
      dokku_storage_entry:
        name: docket-test-plan-data
        chown: herokuish
EOF
  "$(docket_bin)" apply --tasks "$TASKS_FILE"
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "0 would change"

  # Changing an attribute on an entry that already exists is drift, not a
  # silent no-op, and applying it settles.
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-plan-data
      dokku_storage_entry:
        name: docket-test-plan-data
        chown: root
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "[~]"
  assert_output --partial "1 attribute(s) to set"
  assert_output --partial "set chown=root"
  assert_output --partial "1 would change"

  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success

  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "[ok]"
  assert_output --partial "0 would change"
}

@test "docket plan errors on a storage entry attribute dokku cannot change" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-plan-data
      dokku_storage_entry:
        name: docket-test-plan-data
EOF
  "$(docket_bin)" apply --tasks "$TASKS_FILE"

  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-plan-data
      dokku_storage_entry:
        name: docket-test-plan-data
        path: /mnt/docket-test-plan-elsewhere
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "records path"
  assert_output --partial "destroy and re-create the entry"
}
