#!/usr/bin/env bats
#
# Recipes read from stdin (#406). apply, plan, and validate accept "-"
# both as a --tasks value and as a bare positional, matching the
# `docket fmt -` that already existed. --tasks-format overrides the
# format that would otherwise come from the file extension, or from the
# content sniff when there is no filename to key off.
#
# These tests never contact a server: validate is offline by contract,
# and apply/plan are exercised through --list-tasks.

load test_helper

setup() {
  docket_build
}

@test "docket validate - reads a YAML recipe from stdin" {
  cd "$BATS_TEST_TMPDIR"
  cat >input.yml <<'EOF'
---
- tasks:
    - name: create app
      dokku_app:
        app: api
EOF
  run bash -c "\"$(docket_bin)\" validate - <input.yml"
  assert_success
  assert_output --partial "is valid"
  # stdin has no path, so it must not print a bare "-".
  assert_output --partial "<stdin>"
}

@test "docket validate --tasks - reads a YAML recipe from stdin" {
  cd "$BATS_TEST_TMPDIR"
  cat >input.yml <<'EOF'
---
- tasks:
    - name: create app
      dokku_app:
        app: api
EOF
  run bash -c "\"$(docket_bin)\" validate --tasks - <input.yml"
  assert_success
  assert_output --partial "is valid"
}

@test "docket validate - sniffs a JSON5 recipe from stdin" {
  cd "$BATS_TEST_TMPDIR"
  cat >input.json5 <<'EOF'
[
  // a comment, so this can only be JSON5
  { tasks: [{ name: "create app", dokku_app: { app: "api" } }] },
]
EOF
  run bash -c "\"$(docket_bin)\" validate - <input.json5"
  assert_success
  assert_output --partial "is valid"
}

@test "docket validate --tasks-format yaml - overrides the stdin sniff" {
  cd "$BATS_TEST_TMPDIR"
  # A top-level flow sequence is valid YAML but opens with "[", so the
  # sniff would hand it to the JSON5 parser.
  cat >flow.yml <<'EOF'
[{tasks: [{name: flow, dokku_app: {app: api}}]}]
EOF
  run bash -c "\"$(docket_bin)\" validate --tasks-format yaml - <flow.yml"
  assert_success
  assert_output --partial "is valid"
}

@test "docket validate --tasks-format json5 overrides a misleading extension" {
  cd "$BATS_TEST_TMPDIR"
  cat >recipe.txt <<'EOF'
[
  // JSON5 body behind an extension docket would otherwise read as YAML
  { tasks: [{ name: "create app", dokku_app: { app: "api" } }] },
]
EOF
  run "$(docket_bin)" validate --tasks recipe.txt --tasks-format json5
  assert_success
  assert_output --partial "is valid"
}

@test "docket init --output - pipes into docket validate -" {
  cd "$BATS_TEST_TMPDIR"
  run bash -c "\"$(docket_bin)\" init --output - --name api --repo https://example.com/repo.git | \"$(docket_bin)\" validate -"
  assert_success
  assert_output --partial "is valid"
}

@test "docket apply --list-tasks reads a recipe from stdin" {
  cd "$BATS_TEST_TMPDIR"
  cat >input.yml <<'EOF'
---
- tasks:
    - name: first task
      dokku_app:
        app: api
    - name: second task
      dokku_config:
        app: api
        config:
          K: v
EOF
  run bash -c "\"$(docket_bin)\" apply --list-tasks - <input.yml"
  assert_success
  assert_output --partial "first task"
  assert_output --partial "second task"
}

@test "docket plan --list-tasks reads a recipe from stdin" {
  cd "$BATS_TEST_TMPDIR"
  cat >input.yml <<'EOF'
---
- tasks:
    - name: planned task
      dokku_app:
        app: api
EOF
  run bash -c "\"$(docket_bin)\" plan --list-tasks - <input.yml"
  assert_success
  assert_output --partial "planned task"
}

@test "a stdin recipe still registers its inputs as flags" {
  cd "$BATS_TEST_TMPDIR"
  # The --app flag only exists because the flag set was built from the
  # piped recipe, before pflag parsed anything.
  cat >input.yml <<'EOF'
---
- inputs:
    - name: app
      default: from-default
  tasks:
    - name: create {{ .app }}
      dokku_app:
        app: "{{ .app }}"
EOF
  run bash -c "\"$(docket_bin)\" apply --list-tasks - --app from-flag <input.yml"
  assert_success
  assert_output --partial "create from-flag"
  refute_output --partial "create from-default"
}

@test "a stdin recipe ignores a tasks.yml in the working directory" {
  cd "$BATS_TEST_TMPDIR"
  cat >tasks.yml <<'EOF'
---
- tasks:
    - name: on-disk task
      dokku_app:
        app: api
EOF
  cat >piped.yml <<'EOF'
---
- tasks:
    - name: piped task
      dokku_app:
        app: api
EOF
  run bash -c "\"$(docket_bin)\" apply --list-tasks - <piped.yml"
  assert_success
  assert_output --partial "piped task"
  refute_output --partial "on-disk task"
}

@test "docket validate - exits 1 on empty stdin" {
  cd "$BATS_TEST_TMPDIR"
  run bash -c "\"$(docket_bin)\" validate - </dev/null"
  assert_failure
  assert_output --partial "stdin is empty"
}

@test "docket validate rejects an unknown --tasks-format" {
  cd "$BATS_TEST_TMPDIR"
  run bash -c "\"$(docket_bin)\" validate --tasks-format toml - </dev/null"
  assert_failure
  assert_output --partial "yaml, json5"
}

@test "docket validate rejects --tasks - together with a positional -" {
  cd "$BATS_TEST_TMPDIR"
  run bash -c "\"$(docket_bin)\" validate --tasks - - </dev/null"
  assert_failure
  assert_output --partial "both --tasks and a positional"
}

@test "docket fmt --tasks-format yaml - keeps a flow-style recipe as YAML" {
  cd "$BATS_TEST_TMPDIR"
  cat >flow.yml <<'EOF'
[{tasks: [{name: flow, dokku_app: {app: api}}]}]
EOF
  run bash -c "\"$(docket_bin)\" fmt --tasks-format yaml - <flow.yml"
  assert_success
  # The JSON5 formatter would have exploded this onto multiple lines.
  assert_output --partial "[{tasks:"
}

@test "docket fmt rejects - mixed with other paths" {
  cd "$BATS_TEST_TMPDIR"
  cat >tasks.yml <<'EOF'
---
- tasks:
    - dokku_app:
        app: api
EOF
  run "$(docket_bin)" fmt - tasks.yml
  assert_failure
  assert_output --partial "cannot mix - with other paths"
}
