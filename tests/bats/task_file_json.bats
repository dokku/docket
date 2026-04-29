#!/usr/bin/env bats

load test_helper

setup() {
  docket_build
}

@test "docket validate accepts a JSON5 tasks.json" {
  write_tasks_file tasks.json <<'EOF'
[
  {
    inputs: [
      { name: "app", default: "docket-test-json5" },
    ],
    tasks: [
      { name: "create app", dokku_app: { app: "{{ .app }}" } },
    ],
  },
]
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "is valid"
}

@test "docket validate accepts a JSON5 tasks.json with comments and trailing commas" {
  write_tasks_file tasks.json <<'EOF'
[
  // recipe-level comment
  {
    /* play-level block comment */
    tasks: [
      { name: "create app", dokku_app: { app: "api", }, }, // inline comment
    ],
  },
]
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE"
  assert_success
  assert_output --partial "is valid"
}

@test "docket validate reports json5_parse on malformed JSON5" {
  write_tasks_file tasks.json <<'EOF'
[
  { tasks: [
EOF
  run "$(docket_bin)" validate --tasks "$TASKS_FILE" --json
  assert_failure
  assert_output --partial '"code":"json5_parse"'
}

@test "docket apply --list-tasks works against tasks.json" {
  write_tasks_file tasks.json <<'EOF'
[
  {
    tasks: [
      { name: "first task", dokku_app: { app: "api" } },
      { name: "second task", dokku_config: { app: "api", config: { K: "v" } } },
    ],
  },
]
EOF
  run "$(docket_bin)" apply --tasks "$TASKS_FILE" --list-tasks
  assert_success
  assert_output --partial "first task"
  assert_output --partial "second task"
}

@test "docket auto-detects tasks.json when no --tasks flag is given" {
  cd "$BATS_TEST_TMPDIR"
  cat > tasks.json <<'EOF'
[
  {
    tasks: [
      { name: "auto-detected", dokku_app: { app: "api" } },
    ],
  },
]
EOF
  run "$(docket_bin)" validate
  assert_success
  assert_output --partial "tasks.json"
  assert_output --partial "is valid"
}

@test "docket prefers tasks.yml over tasks.json when both exist" {
  cd "$BATS_TEST_TMPDIR"
  cat > tasks.yml <<'EOF'
---
- tasks:
    - name: yaml-task
      dokku_app:
        app: api
EOF
  cat > tasks.json <<'EOF'
[
  { tasks: [{ name: "json-task", dokku_app: { app: "api" } }] },
]
EOF
  run "$(docket_bin)" apply --list-tasks
  assert_success
  assert_output --partial "yaml-task"
  refute_output --partial "json-task"
}

@test "docket fmt canonicalises a JSON5 tasks.json with comments preserved" {
  write_tasks_file tasks.json <<'EOF'
[
  // top of recipe
  {
    tasks: [
      {
        dokku_app: { app: "api" },
        name: "create app", // out of order
      },
    ],
  },
]
EOF
  run "$(docket_bin)" fmt "$TASKS_FILE"
  assert_success

  formatted="$(cat "$TASKS_FILE")"
  echo "$formatted"
  echo "$formatted" | grep -F "// top of recipe"
  echo "$formatted" | grep -F "// out of order"

  # Re-running fmt --check should now exit 0 (idempotent).
  run "$(docket_bin)" fmt --check "$TASKS_FILE"
  assert_success
}

@test "docket fmt --diff prints unified diff for non-canonical JSON5" {
  write_tasks_file tasks.json <<'EOF'
[
  { tasks: [{ dokku_app: { app: "api" }, name: "x" }] },
]
EOF
  run "$(docket_bin)" fmt --diff "$TASKS_FILE"
  # --diff alone exits 0 even when changes are needed.
  assert_success
  assert_output --partial "--- $TASKS_FILE"
  assert_output --partial "+++ $TASKS_FILE"
}

@test "docket init --output tasks.json writes valid JSON5 that round-trips" {
  cd "$BATS_TEST_TMPDIR"
  run "$(docket_bin)" init --output tasks.json --name api --repo https://example.com/repo.git
  assert_success
  [ -f tasks.json ]

  run head -1 tasks.json
  assert_success
  assert_output "["

  run "$(docket_bin)" validate --tasks tasks.json
  assert_success
  assert_output --partial "is valid"

  run "$(docket_bin)" fmt --check tasks.json
  assert_success
}

@test "docket apply --vars-file works with a JSON5 tasks file and JSON vars file" {
  require_dokku
  dokku_clean_app docket-test-json5-mix

  write_tasks_file tasks.json <<'EOF'
[
  {
    inputs: [
      { name: "app", default: "docket-test-default" },
    ],
    tasks: [
      { name: "ensure {{ .app }}", dokku_app: { app: "{{ .app }}" } },
    ],
  },
]
EOF
  cat >"$BATS_TEST_TMPDIR/vars.json" <<'EOF'
{ "app": "docket-test-json5-mix" }
EOF
  run "$(docket_bin)" plan --tasks "$TASKS_FILE" --vars-file "$BATS_TEST_TMPDIR/vars.json"
  assert_success
  assert_output --partial "ensure docket-test-json5-mix"
}
