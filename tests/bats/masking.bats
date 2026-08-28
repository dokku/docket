#!/usr/bin/env bats

load test_helper

# Every test here drives a real apply/plan against a dokku server, so setup()
# gates the whole file on require_dokku. The offline masking cases - the
# --list-tasks listing, which contacts nothing - live in list_resume.bats so
# they still run on a box without dokku.

setup() {
  require_dokku
  docket_build
  dokku_clean_app docket-test-mask
}

teardown() {
  dokku_clean_app docket-test-mask
}

@test "docket apply --verbose masks an input declared sensitive" {
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - name: ensure docket-test-mask
      dokku_app:
        app: docket-test-mask
    - name: set the secret
      dokku_config:
        app: docket-test-mask
        config:
          MY_SECRET: "{{ .secret_value }}"
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --secret_value=topsecret123 --verbose
  assert_success
  refute_output --partial "topsecret123"
  assert_output --partial "***"
}

@test "docket apply --verbose masks dokku_config map values" {
  write_tasks_file <<'EOF'
---
- tasks:
    - name: ensure docket-test-mask
      dokku_app:
        app: docket-test-mask
    - name: set a literal config value
      dokku_config:
        app: docket-test-mask
        config:
          MY_LITERAL: literal-value-zzz
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --verbose
  assert_success
  refute_output --partial "literal-value-zzz"
  # base64 of literal-value-zzz is bGl0ZXJhbC12YWx1ZS16eno=, also masked.
  refute_output --partial "bGl0ZXJhbC12YWx1ZS16eno"
  assert_output --partial "***"
}

@test "DOKKU_TRACE masks values from inputs declared sensitive" {
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - name: ensure docket-test-mask
      dokku_app:
        app: docket-test-mask
    - name: set the secret
      dokku_config:
        app: docket-test-mask
        config:
          MY_SECRET: "{{ .secret_value }}"
EOF
  DOKKU_TRACE=1 run "$(docket_bin)" apply --tasks "$TASKS_FILE" --secret_value=tracesecretzzz
  assert_success
  refute_output --partial "tracesecretzzz"
}

@test "docket apply --verbose masks a sensitive value nested in a block" {
  # The config value lives on a task inside a block: group. Collecting it
  # requires walking the group's children (#305); otherwise it leaks.
  write_tasks_file <<'EOF'
---
- tasks:
    - name: ensure docket-test-mask
      dokku_app:
        app: docket-test-mask
    - name: group
      block:
        - name: set a literal config value in a block
          dokku_config:
            app: docket-test-mask
            config:
              MY_BLOCK_LITERAL: block-literal-zzz
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --verbose
  assert_success
  refute_output --partial "block-literal-zzz"
  assert_output --partial "***"
}

@test "docket apply masks a sensitive loop item embedded in the task name" {
  # Looping over a sensitive input names each expansion `<name> (item=<value>)`;
  # the task name must be masked in the output (#312).
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - name: ensure docket-test-mask
      dokku_app:
        app: docket-test-mask
    - name: set secret config
      loop: 'split(secret_value, ",")'
      dokku_config:
        app: docket-test-mask
        config:
          LOOP_SECRET: "{{ .item }}"
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --secret_value=loopitemzzz
  assert_success
  refute_output --partial "loopitemzzz"
  assert_output --partial "***"
}

@test "docket apply masks a whitespace-padded sensitive loop item in the task name" {
  # #473: the `(item=<value>)` suffix trims the item, so a secret padded with
  # whitespace stopped matching the registered literal and reached the run
  # stream unmasked.
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - name: ensure docket-test-mask
      dokku_app:
        app: docket-test-mask
    - name: set padded secret config
      loop: 'split(secret_value, ",")'
      dokku_config:
        app: docket-test-mask
        config:
          PADDED_LOOP_SECRET: "{{ .item }}"
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --secret_value=" paddedloopzzz "
  assert_success
  refute_output --partial "paddedloopzzz"
  assert_output --partial "set padded secret config (item=***)"
}

@test "docket apply masks a quote-bearing sensitive identity value in the task name" {
  # #475: an unnamed task is named after the resource it addresses, and the
  # address wraps a key value in Go quoting when a bare form would not parse
  # back. That escapes the quote the secret carries, so it stopped matching the
  # registered literal and reached the run stream unmasked. The option is
  # removed rather than added so the task is a no-op against the server while
  # still rendering its address.
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - name: ensure docket-test-mask
      dokku_app:
        app: docket-test-mask
    - dokku_docker_options:
        app: docket-test-mask
        phase: deploy
        option: "--label docket-test={{ .secret_value | dq }}"
        state: absent
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --secret_value='quotedaddresszzz"x'
  assert_success
  refute_output --partial "quotedaddresszzz"
  assert_output --partial 'option="--label docket-test=***"'
}

@test "docket apply masks a sensitive value interpolated into a play when:" {
  # A play predicate sigil-interpolates a sensitive input, so the recipe text
  # (and the skip line echoing it) contains the literal secret (#335).
  write_tasks_file <<'EOF'
---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  when: '"{{ .secret_value }}" == "will-not-match"'
  tasks:
    - dokku_app:
        app: docket-test-mask
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --secret_value=playwhenzzz
  assert_success
  assert_output --partial "(skipped:"
  refute_output --partial "playwhenzzz"
  assert_output --partial "***"
}

@test "docket plan masks a traefik dns-provider credential" {
  # traefik does not report its dns-provider-* family, so the property takes the
  # unprobed path and no traefik state is read or written here. The value is a
  # DNS provider credential all the same and must not reach the mutation line or
  # the --json commands array (#457).
  write_tasks_file <<'EOF'
---
- tasks:
    - name: set a traefik dns provider credential
      dokku_traefik_property:
        global: true
        property: dns-provider-CLOUDFLARE_API_TOKEN
        value: traefiktokenzzz
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  refute_output --partial "traefiktokenzzz"
  assert_output --partial "***"

  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --json
  assert_success
  refute_output --partial "traefiktokenzzz"
  assert_output --partial "***"
}

@test "docket plan output never echoes dokku_config map values" {
  # Create the app first so the dokku_config plan probe succeeds; otherwise
  # the missing-app probe error short-circuits the test before the masking
  # path is exercised.
  write_tasks_file create.yml <<'EOF'
---
- tasks:
    - name: ensure docket-test-mask
      dokku_app:
        app: docket-test-mask
EOF
  "$(docket_bin)" apply --tasks "$TASKS_FILE"

  write_tasks_file plan.yml <<'EOF'
---
- tasks:
    - name: set a literal config value
      dokku_config:
        app: docket-test-mask
        config:
          MY_LITERAL: literal-value-zzz
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE"
  assert_success
  refute_output --partial "literal-value-zzz"
}
