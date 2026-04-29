#!/usr/bin/env bats

load test_helper

# These tests cover the --vars-file flag end to end. Substitution-verification
# cases run docket plan against a real dokku because plan is the cheapest
# command that exercises the full input -> sigil render -> task pipeline
# without mutating server state. Error-path cases run docket validate, which
# is fully offline and so does not need a dokku gate.

setup() {
  docket_build
}

# offline: validate is the no-dokku path for unknown-key and missing-file
# errors so these cases run on every dev box.
@test "docket validate errors on unknown key in --vars-file with did-you-mean" {
  write_tasks_file <<EOF
---
- inputs:
    - name: app
      default: default-app
  tasks:
    - dokku_app:
        app: "{{ .app }}"
EOF
  cat >"$BATS_TEST_TMPDIR/vars.yml" <<EOF
appp: typo
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --vars-file "$BATS_TEST_TMPDIR/vars.yml"
  assert_failure
  assert_output --partial 'unknown input "appp"'
  assert_output --partial 'did you mean "app"'
  assert_output --partial "vars.yml"
}

@test "docket validate errors on missing --vars-file path" {
  write_tasks_file <<EOF
---
- inputs:
    - name: app
      default: default-app
  tasks:
    - dokku_app:
        app: "{{ .app }}"
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --vars-file "$BATS_TEST_TMPDIR/does-not-exist.yml"
  assert_failure
  assert_output --partial "does-not-exist.yml"
}

@test "docket validate --strict treats --vars-file values as overrides" {
  write_tasks_file <<EOF
---
- inputs:
    - name: app
      required: true
  tasks:
    - dokku_app:
        app: {{ .app | default "" }}
EOF
  cat >"$BATS_TEST_TMPDIR/vars.yml" <<EOF
app: docket-test-vars
EOF
  # Without --vars-file, --strict flags the missing required input.
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --strict
  assert_failure
  assert_output --partial 'input "app" is required'

  # With --vars-file supplying the value, --strict accepts the recipe.
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --strict --vars-file "$BATS_TEST_TMPDIR/vars.yml"
  assert_success
}

@test "docket validate errors on unparseable --vars-file" {
  write_tasks_file <<EOF
---
- inputs:
    - name: app
      default: default-app
  tasks:
    - dokku_app:
        app: "{{ .app }}"
EOF
  cat >"$BATS_TEST_TMPDIR/vars.yml" <<EOF
- not
- a
- mapping
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --vars-file "$BATS_TEST_TMPDIR/vars.yml"
  assert_failure
  assert_output --partial "mapping"
}

# end-to-end: substitution verification through plan; needs dokku.
@test "docket plan with --vars-file overrides file inputs" {
  require_dokku
  dokku_clean_app docket-test-vars
  write_tasks_file <<EOF
---
- inputs:
    - name: app
      default: docket-test-default
  tasks:
    - name: "ensure {{ .app }}"
      dokku_app:
        app: "{{ .app }}"
EOF
  cat >"$BATS_TEST_TMPDIR/vars.yml" <<EOF
app: docket-test-vars
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --vars-file "$BATS_TEST_TMPDIR/vars.yml"
  assert_success
  assert_output --partial "ensure docket-test-vars"
  refute_output --partial "ensure docket-test-default"
}

@test "docket plan with CLI flag overrides --vars-file" {
  require_dokku
  dokku_clean_app docket-test-vars-cli
  write_tasks_file <<EOF
---
- inputs:
    - name: app
      default: docket-test-default
  tasks:
    - name: "ensure {{ .app }}"
      dokku_app:
        app: "{{ .app }}"
EOF
  cat >"$BATS_TEST_TMPDIR/vars.yml" <<EOF
app: docket-test-vars
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --vars-file "$BATS_TEST_TMPDIR/vars.yml" --app=docket-test-vars-cli
  assert_success
  assert_output --partial "ensure docket-test-vars-cli"
  refute_output --partial "ensure docket-test-vars "
}

@test "docket plan with later --vars-file overrides earlier" {
  require_dokku
  dokku_clean_app docket-test-vars-b
  write_tasks_file <<EOF
---
- inputs:
    - name: app
      default: docket-test-default
  tasks:
    - name: "ensure {{ .app }}"
      dokku_app:
        app: "{{ .app }}"
EOF
  cat >"$BATS_TEST_TMPDIR/a.yml" <<EOF
app: docket-test-vars-a
EOF
  cat >"$BATS_TEST_TMPDIR/b.yml" <<EOF
app: docket-test-vars-b
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --vars-file "$BATS_TEST_TMPDIR/a.yml" --vars-file "$BATS_TEST_TMPDIR/b.yml"
  assert_success
  assert_output --partial "ensure docket-test-vars-b"
  refute_output --partial "ensure docket-test-vars-a"
}

@test "docket plan with JSON --vars-file is parsed when extension is .json" {
  require_dokku
  dokku_clean_app docket-test-vars-json
  write_tasks_file <<EOF
---
- inputs:
    - name: app
      default: docket-test-default
  tasks:
    - name: "ensure {{ .app }}"
      dokku_app:
        app: "{{ .app }}"
EOF
  cat >"$BATS_TEST_TMPDIR/vars.json" <<EOF
{"app": "docket-test-vars-json"}
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --vars-file "$BATS_TEST_TMPDIR/vars.json"
  assert_success
  assert_output --partial "ensure docket-test-vars-json"
}

@test "docket apply with --vars-file applies layered values" {
  require_dokku
  dokku_clean_app docket-test-vars-apply
  write_tasks_file <<EOF
---
- inputs:
    - name: app
      default: docket-test-default
  tasks:
    - name: "ensure {{ .app }}"
      dokku_app:
        app: "{{ .app }}"
EOF
  cat >"$BATS_TEST_TMPDIR/vars.yml" <<EOF
app: docket-test-vars-apply
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --vars-file "$BATS_TEST_TMPDIR/vars.yml"
  assert_success
  run dokku apps:exists docket-test-vars-apply
  assert_success
  dokku_clean_app docket-test-vars-apply
}
