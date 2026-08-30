#!/usr/bin/env bats

load test_helper

setup() {
  require_dokku
  docket_build
  dokku_clean_app docket-test-plan
  dokku_clean_storage_entry docket-test-plan-data
  dokku_clean_storage_entry docket-test-plan-hostdir
}

teardown() {
  dokku_clean_app docket-test-plan
  dokku_clean_storage_entry docket-test-plan-data
  dokku_clean_storage_entry docket-test-plan-hostdir
  rm -rf /var/lib/dokku/data/storage/docket-test-plan-hostdir
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

@test "docket plan reports drift when a storage entry's mode changes" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-plan-data
      dokku_storage_entry:
        name: docket-test-plan-data
        mode: "0750"
EOF
  "$(docket_bin)" apply --tasks "$TASKS_FILE"
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "0 would change"

  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-plan-data
      dokku_storage_entry:
        name: docket-test-plan-data
        mode: "0777"
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "[~]"
  assert_output --partial "set mode=0777"
  assert_output --partial "1 would change"

  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success

  # The converge re-runs the chmod on the directory rather than only
  # recording a new value, which is the whole point of routing it through
  # storage:set.
  run stat -c '%a' /var/lib/dokku/data/storage/docket-test-plan-data
  assert_success
  assert_output "777"

  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "[ok]"
  assert_output --partial "0 would change"
}

@test "docket plan settles on a three digit storage entry mode" {
  # dokku records 0755 whether the recipe wrote 755 or 0755, so a recipe
  # naming three digits has to read as already applied on the second run.
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-plan-data
      dokku_storage_entry:
        name: docket-test-plan-data
        mode: "755"
EOF
  "$(docket_bin)" apply --tasks "$TASKS_FILE"
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "0 would change"
}

@test "docket apply removes the host directory when a storage entry asks for it" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-plan-hostdir
      dokku_storage_entry:
        name: docket-test-plan-hostdir
EOF
  "$(docket_bin)" apply --tasks "$TASKS_FILE"
  run test -d /var/lib/dokku/data/storage/docket-test-plan-hostdir
  assert_success

  write_tasks_file <<EOF
---
- tasks:
    - name: remove docket-test-plan-hostdir
      dokku_storage_entry:
        name: docket-test-plan-hostdir
        destroy_host_dir: true
        state: absent
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "[-]"
  assert_output --partial "and its host directory /var/lib/dokku/data/storage/docket-test-plan-hostdir"

  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
  run test -e /var/lib/dokku/data/storage/docket-test-plan-hostdir
  assert_failure
}

@test "docket apply leaves the host directory when a storage entry does not ask" {
  write_tasks_file <<EOF
---
- tasks:
    - name: ensure docket-test-plan-hostdir
      dokku_storage_entry:
        name: docket-test-plan-hostdir
EOF
  "$(docket_bin)" apply --tasks "$TASKS_FILE"

  write_tasks_file <<EOF
---
- tasks:
    - name: remove docket-test-plan-hostdir
      dokku_storage_entry:
        name: docket-test-plan-hostdir
        state: absent
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  refute_output --partial "and its host directory"

  run "$(docket_bin)" apply --tasks "$TASKS_FILE"
  assert_success
  run test -d /var/lib/dokku/data/storage/docket-test-plan-hostdir
  assert_success
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
