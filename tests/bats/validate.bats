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

@test "docket validate exits 1 on a port mapping missing its scheme" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_ports:
        app: web
        port_mappings:
          - host: 80
            container: 5000
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "'scheme' is required"
}

@test "docket validate exits 0 on ports state clear without port_mappings (#415)" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_ports:
        app: web
        state: clear
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "is valid"
}

@test "docket validate exits 1 on ports state clear carrying port_mappings" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_ports:
        app: web
        state: clear
        port_mappings:
          - scheme: http
            host: 80
            container: 5000
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "'port_mappings' must not be set for state 'clear'"
}

@test "docket validate exits 1 on ports state set with an empty port_mappings" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_ports:
        app: web
        state: set
        port_mappings: []
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "'port_mappings' must not be empty for state 'set'"
}

@test "docket validate exits 1 on two port mappings sharing a scheme and host port (#432)" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_ports:
        app: web
        port_mappings:
          - scheme: http
            host: 80
            container: 5000
          - scheme: http
            host: 80
            container: 6000
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "port_mappings[1] reuses the scheme and host port of port_mappings[0] (http:80)"
}

@test "docket validate exits 0 on a reused scheme and host port under state absent" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_ports:
        app: web
        state: absent
        port_mappings:
          - scheme: http
            host: 80
            container: 5000
          - scheme: http
            host: 80
            container: 6000
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "is valid"
}

@test "docket validate exits 1 on git_from_image email without username" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_git_from_image:
        app: web
        image: org/app:1.0
        git_email: deploy@example.com
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "'git_username' and 'git_email' must be set together"
}

@test "docket validate exits 1 on service_backup endpoint without region" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_service_backup:
        service: postgres
        name: my-db
        aws_access_key_id: AKIA
        aws_secret_access_key: secret
        endpoint_url: https://s3.example.com
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "'aws_signature_version' is required when 'endpoint_url' is set"
}

@test "docket validate exits 1 on service_create options under state absent" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_service_create:
        service: postgres
        name: my-db
        image: postgis/postgis
        state: absent
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "'image' must not be set for state 'absent'"
}

@test "docket validate exits 1 on an unknown service_create image_drift" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_service_create:
        service: redis
        name: cache
        image_version: "7.2.5"
        image_drift: sometimes
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "'image_drift' must be one of ignore, warn, error, upgrade"
}

@test "docket validate exits 1 on service_create image_drift without an image pin" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_service_create:
        service: redis
        name: cache
        image_drift: upgrade
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "'image_drift' requires 'image' or 'image_version'"
}

@test "docket validate exits 1 on service_create image_drift upgrade without an image_version" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_service_create:
        service: redis
        name: cache
        image: redis
        image_drift: upgrade
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "'image_drift: upgrade' requires 'image_version' when 'image' is set"
}

@test "docket validate exits 1 on service_create restart_apps without an upgrade" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_service_create:
        service: redis
        name: cache
        image: redis
        image_version: "7.2.5"
        restart_apps: true
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "'restart_apps' requires 'image_drift: upgrade'"
}

@test "docket validate exits 1 on service_create image_drift under state absent" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_service_create:
        service: redis
        name: cache
        image_drift: upgrade
        state: absent
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "'image_drift' must not be set for state 'absent'"
}

@test "docket validate exits 1 on service_create restart_apps under state absent" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_service_create:
        service: redis
        name: cache
        restart_apps: true
        state: absent
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "'restart_apps' must not be set for state 'absent'"
}

@test "docket validate exits 1 on a service_create custom_env delimiter" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_service_create:
        service: postgres
        name: my-db
        custom_env:
          GREETING: "one;two"
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "'custom_env' value for \"GREETING\" must not contain ';'"
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

@test "docket validate exits 1 on an empty storage entry annotation value" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_storage_entry:
        name: docket-test-validate-data
        annotations:
          docket.io/team: ""
        state: present
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "'annotations' value for \"docket.io/team\" must not be empty"
  assert_output --partial "dokku reads an empty value as a delete"
}

@test "docket validate names the replacement task for a rejected property family (#458)" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_scheduler_k3s_property:
        global: true
        property: chart.traefik.replicas
        value: "3"
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "chart.* properties are managed by dokku_scheduler_k3s_chart"
  assert_output --partial "deprecated in dokku"
  # The whole point is that the generic list never appears here: it cannot
  # contain the name the user wrote.
  refute_output --partial "unsupported property"
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

@test "docket validate exits 1 on an input named after a built-in flag" {
  write_tasks_file <<EOF
---
- inputs:
    - name: no-color
      default: x
  tasks:
    - dokku_app:
        app: my-app
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "reserved for a built-in flag"
}

@test "docket validate exits 1 on a hyphenated input name" {
  write_tasks_file <<EOF
---
- inputs:
    - name: my-app
      default: web
  tasks:
    - dokku_app:
        app: "{{ .my-app }}"
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "not a valid template variable name"
  refute_output --partial "bad character"
}

@test "docket validate --json emits invalid_input_name for a hyphenated input name" {
  write_tasks_file <<EOF
---
- inputs:
    - name: my-app
      default: web
  tasks:
    - dokku_app:
        app: "{{ .my-app }}"
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --json
  assert_failure
  assert_output --partial '"code":"invalid_input_name"'
}

@test "docket apply rejects a hyphenated input name offline" {
  # A hyphenated input name breaks `{{ .name }}` rendering; the loader must
  # reject it up front with the same clear message validate reports, not a
  # cryptic render error (#370).
  write_tasks_file <<EOF
---
- inputs:
    - name: my-app
      default: web
  tasks:
    - dokku_app:
        app: "{{ .my-app }}"
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_failure
  assert_output --partial "not a valid template variable name"
  refute_output --partial "bad character"
}

@test "docket apply does not panic on an input named after a built-in flag" {
  # An input named after a built-in flag used to make pflag panic before
  # flag parsing began (#302). apply must now fail cleanly instead.
  write_tasks_file <<EOF
---
- inputs:
    - name: verbose
      default: x
  tasks:
    - dokku_app:
        app: my-app
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_failure
  assert_output --partial "reserved for a built-in flag"
  refute_output --partial "panic:"
  refute_output --partial "flag redefined"
}

@test "docket validate exits 1 on an input value that breaks its scalar" {
  # A value containing a double quote breaks the double-quoted body it is
  # substituted into; validate names the input instead of a cryptic YAML error
  # (#371).
  write_tasks_file <<EOF
---
- inputs:
    - name: app
      default: 'ab"cd'
  tasks:
    - dokku_app:
        app: "{{ .app }}"
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial 'input "app"'
  assert_output --partial "breaks the surrounding scalar"
  refute_output --partial "did not find expected"
}

@test "docket validate --json emits unsafe_input_value for a breaking value" {
  write_tasks_file <<EOF
---
- inputs:
    - name: app
      default: 'ab"cd'
  tasks:
    - dokku_app:
        app: "{{ .app }}"
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --json
  assert_failure
  assert_output --partial '"code":"unsafe_input_value"'
}

@test "docket validate passes when a breaking value is escaped with dq" {
  # Piping the value through dq inside the quotes keeps the rendered scalar
  # valid, and the quoted recipe still parses for validate (#371).
  write_tasks_file <<EOF
---
- inputs:
    - name: motd
      default: 'say "hi"'
  tasks:
    - dokku_config:
        app: web
        config:
          MOTD: "{{ .motd | dq }}"
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "is valid"
}

@test "docket apply rejects an unsafe input value offline" {
  write_tasks_file <<EOF
---
- inputs:
    - name: app
      default: 'ab"cd'
  tasks:
    - dokku_app:
        app: "{{ .app }}"
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_failure
  assert_output --partial "breaks the surrounding scalar"
  refute_output --partial "did not find expected"
}

@test "docket validate exits 1 on duplicate task names" {
  write_tasks_file <<EOF
---
- tasks:
    - name: same
      dokku_app:
        app: first-app
    - name: same
      dokku_app:
        app: second-app
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "duplicate task name"
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

@test "docket validate exits 1 on a non-string task name" {
  write_tasks_file <<EOF
---
- tasks:
    - name: 123
      dokku_app:
        app: x
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_failure
  assert_output --partial "name must be a string"
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

@test "docket validate checks a positional file argument" {
  mkdir -p "$BATS_TEST_TMPDIR/staging"
  cat >"$BATS_TEST_TMPDIR/staging/tasks.yml" <<EOF
---
- tasks:
    - dokku_appp:
        app: broken
EOF
  # A valid default tasks.yml in the cwd must not mask the broken
  # positional file (the bug: the positional was ignored, #331).
  cat >"$BATS_TEST_TMPDIR/tasks.yml" <<EOF
---
- tasks:
    - dokku_app:
        app: ok
EOF
  cd "$BATS_TEST_TMPDIR"
  run "$(docket_bin)" validate staging/tasks.yml
  assert_failure
  assert_output --partial "staging/tasks.yml"
  assert_output --partial "unknown task type"
}

@test "docket validate rejects both --tasks and a positional file" {
  write_tasks_file <<EOF
---
- tasks:
    - dokku_app:
        app: ok
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" "$TASKS_FILE"
  assert_failure
  assert_output --partial "cannot specify both --tasks and a positional"
}

@test "docket apply --list-tasks honors a positional file argument" {
  mkdir -p "$BATS_TEST_TMPDIR/staging"
  cat >"$BATS_TEST_TMPDIR/staging/tasks.yml" <<EOF
---
- tasks:
    - name: staging-only
      dokku_app:
        app: api
EOF
  cd "$BATS_TEST_TMPDIR"
  run "$(docket_bin)" apply --list-tasks staging/tasks.yml
  assert_success
  assert_output --partial "staging-only"
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
