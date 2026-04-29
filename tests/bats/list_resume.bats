#!/usr/bin/env bats

load test_helper

# list_resume.bats covers the #212 inspection / resume surface end-to-end:
# --list-tasks on apply and plan renders the resolved task plan without
# executing or probing, --start-at-task on apply skips earlier tasks and
# runs from the matched task onward (including the inside-block case),
# and validate --strict surfaces unknown_play_reference /
# unknown_start_at_task problems for typo'd CLI references.

setup() {
  docket_build
  dokku_clean_app docket-test-list-1
  dokku_clean_app docket-test-list-2
  dokku_clean_app docket-test-list-tag-api
  dokku_clean_app docket-test-list-tag-worker
  dokku_clean_app docket-test-list-loop-a
  dokku_clean_app docket-test-list-loop-b
  dokku_clean_app docket-test-list-loop-c
  dokku_clean_app docket-test-start-at-first
  dokku_clean_app docket-test-start-at-second
  dokku_clean_app docket-test-start-at-third
  dokku_clean_app docket-test-start-at-unknown
  dokku_clean_app docket-test-start-block-a
  dokku_clean_app docket-test-start-block-b
  dokku_clean_app docket-test-start-block-c
}

teardown() {
  dokku_clean_app docket-test-list-1
  dokku_clean_app docket-test-list-2
  dokku_clean_app docket-test-list-tag-api
  dokku_clean_app docket-test-list-tag-worker
  dokku_clean_app docket-test-list-loop-a
  dokku_clean_app docket-test-list-loop-b
  dokku_clean_app docket-test-list-loop-c
  dokku_clean_app docket-test-start-at-first
  dokku_clean_app docket-test-start-at-second
  dokku_clean_app docket-test-start-at-third
  dokku_clean_app docket-test-start-at-unknown
  dokku_clean_app docket-test-start-block-a
  dokku_clean_app docket-test-start-block-b
  dokku_clean_app docket-test-start-block-c
}

@test "--list-tasks prints resolved plan without running" {
  require_dokku
  write_tasks_file <<EOF
---
- tasks:
    - dokku_app: { app: docket-test-list-1 }
    - dokku_app: { app: docket-test-list-2 }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "docket-test-list-1"
  assert_output --partial "docket-test-list-2"
  run dokku apps:exists docket-test-list-1
  assert_failure   # never created
}

@test "--list-tasks honors --tags filter" {
  write_tasks_file <<EOF
---
- tasks:
    - name: api task
      tags: [api]
      dokku_app: { app: docket-test-list-tag-api }
    - name: worker task
      tags: [worker]
      dokku_app: { app: docket-test-list-tag-worker }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks --tags api
  assert_success
  assert_output --partial "api task"
  refute_output --partial "worker task"
}

@test "--list-tasks expands loops" {
  write_tasks_file <<EOF
---
- tasks:
    - loop: [a, b, c]
      dokku_app: { app: "docket-test-list-loop-{{ .item }}" }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "item=a"
  assert_output --partial "item=b"
  assert_output --partial "item=c"
}

@test "--list-tasks shows [skipped] for when:false" {
  write_tasks_file <<EOF
---
- tasks:
    - name: gated
      when: 'false'
      dokku_app: { app: docket-test-list-1 }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "[skipped] gated"
}

@test "--list-tasks works on plan as well" {
  write_tasks_file <<EOF
---
- tasks:
    - name: probe
      dokku_app: { app: docket-test-list-1 }
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "[0] probe"
}

@test "--start-at-task skips earlier tasks" {
  require_dokku
  write_tasks_file <<EOF
---
- tasks:
    - name: first
      dokku_app: { app: docket-test-start-at-first }
    - name: second
      dokku_app: { app: docket-test-start-at-second }
    - name: third
      dokku_app: { app: docket-test-start-at-third }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --start-at-task second
  assert_success
  assert_output --partial "[skipped] first"
  assert_output --partial "before --start-at-task"
  run dokku apps:exists docket-test-start-at-first
  assert_failure   # skipped
  run dokku apps:exists docket-test-start-at-second
  assert_success
  run dokku apps:exists docket-test-start-at-third
  assert_success
}

@test "--start-at-task with unknown name errors" {
  write_tasks_file <<EOF
---
- tasks:
    - name: first
      dokku_app: { app: docket-test-start-at-unknown }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --start-at-task no-such-task
  assert_failure
  assert_output --partial "no task matched name"
  assert_output --partial '"first"'
}

@test "--start-at-task matching a block child runs from that child" {
  require_dokku
  write_tasks_file <<EOF
---
- tasks:
    - name: deploy-group
      block:
        - name: block-a
          dokku_app: { app: docket-test-start-block-a }
        - name: block-b
          dokku_app: { app: docket-test-start-block-b }
        - name: block-c
          dokku_app: { app: docket-test-start-block-c }
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --start-at-task block-b
  assert_success
  assert_output --partial "[skipped] [block] block-a"
  assert_output --partial "before --start-at-task"
  run dokku apps:exists docket-test-start-block-a
  assert_failure
  run dokku apps:exists docket-test-start-block-b
  assert_success
  run dokku apps:exists docket-test-start-block-c
  assert_success
}

@test "validate --strict --play missing reports unknown_play_reference" {
  write_tasks_file <<EOF
---
- name: api
  tasks:
    - dokku_app: { app: docket-test-list-1 }
- name: worker
  tasks:
    - dokku_app: { app: docket-test-list-2 }
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --strict --play missing
  assert_failure
  assert_output --partial '--play "missing"'
  assert_output --partial '"api"'
  assert_output --partial '"worker"'
}

@test "validate --strict --start-at-task missing reports unknown_start_at_task" {
  write_tasks_file <<EOF
---
- tasks:
    - name: deploy
      dokku_app: { app: docket-test-list-1 }
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --strict --start-at-task missing-task
  assert_failure
  assert_output --partial '--start-at-task "missing-task"'
  assert_output --partial '"deploy"'
}
