#!/usr/bin/env bats

load test_helper

# envelope.bats covers the #205 envelope features end-to-end against the
# docket binary: the `tags:` filter (--tags / --skip-tags), the `when:`
# conditional, `loop:` expansion, and the parse / validate diagnostics
# that fire when an envelope key is unknown or a loop variable leaks
# outside a loop body.
#
# Tests that exercise plan against a real Dokku gate on require_dokku;
# the validate-only tests run anywhere because validate is offline by
# contract.

setup() {
  docket_build
}

teardown() {
  dokku_clean_app docket-test-tag-api
  dokku_clean_app docket-test-tag-worker
  dokku_clean_app docket-test-skip-api
  dokku_clean_app docket-test-skip-worker
  dokku_clean_app docket-test-when
  dokku_clean_app docket-test-when-kept
  dokku_clean_app docket-test-loop-a
  dokku_clean_app docket-test-loop-b
  dokku_clean_app docket-test-loop-c
}

@test "envelope: --tags filter selects matching tasks" {
  require_dokku
  write_tasks_file <<EOF
---
- tasks:
    - name: app api
      tags: [api]
      dokku_app:
        app: docket-test-tag-api
    - name: app worker
      tags: [worker]
      dokku_app:
        app: docket-test-tag-worker
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --tags api
  assert_success
  assert_output --partial "app api"
  refute_output --partial "app worker"
}

@test "envelope: --skip-tags filter drops matching tasks" {
  require_dokku
  write_tasks_file <<EOF
---
- tasks:
    - name: app api
      tags: [api]
      dokku_app:
        app: docket-test-skip-api
    - name: app worker
      tags: [worker]
      dokku_app:
        app: docket-test-skip-worker
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --skip-tags api
  assert_success
  refute_output --partial "app api"
  assert_output --partial "app worker"
}

@test "envelope: when:false renders [skipped]" {
  require_dokku
  write_tasks_file <<EOF
---
- tasks:
    - name: skipped task
      when: 'false'
      dokku_app:
        app: docket-test-when
    - name: kept task
      dokku_app:
        app: docket-test-when-kept
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "[skipped]"
  assert_output --partial "skipped task"
  assert_output --partial "kept task"
  assert_output --partial "1 skipped"
}

@test "envelope: loop expands one task into N" {
  require_dokku
  write_tasks_file <<EOF
---
- tasks:
    - name: deploy
      loop: [a, b, c]
      dokku_app:
        app: "docket-test-loop-{{ .item }}"
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "deploy (item=a)"
  assert_output --partial "deploy (item=b)"
  assert_output --partial "deploy (item=c)"
}

@test "envelope: loop with when: filters items per iteration" {
  require_dokku
  write_tasks_file <<EOF
---
- tasks:
    - name: deploy
      loop: [api, worker, web]
      when: 'item != "web"'
      dokku_app:
        app: "docket-test-loop-{{ .item }}"
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "deploy (item=api)"
  assert_output --partial "deploy (item=worker)"
  assert_output --partial "[skipped]"
}

@test "envelope: unknown envelope key produces parse error with did-you-mean" {
  # Apply / plan run the loader's strict allowlist; a typo of `tags`
  # surfaces here with the closest match suggestion. Validate's path
  # catches the same shape but reports it as an unknown task type.
  write_tasks_file <<EOF
---
- tasks:
    - name: x
      tag: foo
      dokku_app:
        app: docket-test-unknown
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "unknown envelope key"
  assert_output --partial 'did you mean "tags"'
}

@test "envelope: .item outside a loop is rejected by validate" {
  write_tasks_file <<EOF
---
- tasks:
    - name: stray
      dokku_app:
        app: "{{ .item }}"
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial ".item / .index are only available inside a loop body"
}

@test "envelope: .item sub-field outside a loop is rejected by validate" {
  # A sub-field reference (`{{ .item.name }}`) used to slip past the
  # exact-token check and render to a literal; it must now be rejected
  # the same way a bare `{{ .item }}` is (#321).
  write_tasks_file <<EOF
---
- tasks:
    - name: stray
      dokku_app:
        app: "{{ .item.name }}"
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial ".item / .index are only available inside a loop body"
}
