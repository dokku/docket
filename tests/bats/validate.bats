#!/usr/bin/env bats

load test_helper

setup() {
  docket_build
}

@test "docket validate exits 0 on a valid tasks.yml" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_app:
        app: docket-test-validate
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "is valid"
}

@test "docket validate exits 1 on unknown task type" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_appp:
        app: docket-test-validate
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "unknown task type"
  assert_output --partial "did you mean"
}

@test "docket validate exits 1 on missing required field" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_config:
        restart: false
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial 'missing required field "app"'
}

@test "docket validate exits 1 on a conditional input error" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_acl_app:
        app: docket-test-validate
        users: []
        state: present
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "'users' must not be empty"
}

@test "docket validate exits 1 on a present-state empty pair value" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_scheduler_k3s_labels:
        app: docket-test-validate
        resource_type: deployment
        labels:
          tier: ""
        state: present
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "label values must not be empty for state 'present'"
}

@test "docket validate --json emits invalid_task_input" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_acl_app:
        app: docket-test-validate
        users: []
        state: present
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --json
  assert_failure
  assert_output --partial '"code":"invalid_task_input"'
}

@test "docket validate exits 1 on a null task body" {
  write_tasks_file <<EOF
---
- tasks:
    - name: x
      dokku_app:
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "body must not be empty"
}

@test "docket validate exits 1 on broken sigil template" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_app:
        app: {{ .broken
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "template render error"
}

@test "docket validate --json emits structured problems" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_appp:
        app: x
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --json
  assert_failure
  assert_output --partial '"type":"validate_problem"'
  assert_output --partial '"code":"unknown_task_type"'
  assert_output --partial '"version":1'
}

@test "docket validate --strict flags required input without default" {
  write_tasks_file <<EOF
---
- inputs:
    - name: app
      required: true
  tasks:
    - dokku_app:
        app: {{ .app | default "" }}
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_success

  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --strict
  assert_failure
  assert_output --partial 'input "app" is required'
}

@test "docket validate --strict passes when input has CLI override" {
  write_tasks_file <<EOF
---
- inputs:
    - name: app
      required: true
  tasks:
    - dokku_app:
        app: {{ .app | default "" }}
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --strict --app docket-test
  assert_success
  assert_output --partial "is valid"
}
