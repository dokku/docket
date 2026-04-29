#!/usr/bin/env bats

load test_helper

setup() {
  docket_build
}

@test "docket fmt rewrites a non-canonical file in place" {
  cd "$BATS_TEST_TMPDIR"
  cat >tasks.yml <<'EOF'
---
- tasks:
        - dokku_app:
              app: x
EOF
  run "$(docket_bin)" fmt
  assert_success
  run grep -c "^        - " tasks.yml
  assert_output "0"
  assert [ -f tasks.yml ]
}

@test "docket fmt --check exits 1 on non-canonical" {
  cd "$BATS_TEST_TMPDIR"
  cat >tasks.yml <<'EOF'
---
- tasks:
        - dokku_app:
              app: x
EOF
  run "$(docket_bin)" fmt --check
  assert_failure
  assert_output --partial "not canonically formatted"
}

@test "docket fmt --check exits 0 on canonical" {
  cd "$BATS_TEST_TMPDIR"
  "$(docket_bin)" init
  run "$(docket_bin)" fmt --check
  assert_success
}

@test "docket fmt --diff prints unified diff and does not write" {
  cd "$BATS_TEST_TMPDIR"
  cat >tasks.yml <<'EOF'
---
- tasks:
        - dokku_app:
              app: x
EOF
  before=$(cat tasks.yml)
  run "$(docket_bin)" fmt --diff --color never
  assert_success
  assert_output --partial "---"
  assert_output --partial "+++"
  assert_output --partial "@@"
  assert [ "$(cat tasks.yml)" = "$before" ]
}

@test "docket fmt --check --diff prints diff and exits 1" {
  cd "$BATS_TEST_TMPDIR"
  cat >tasks.yml <<'EOF'
---
- tasks:
        - dokku_app:
              app: x
EOF
  run "$(docket_bin)" fmt --check --diff --color never
  assert_failure
  assert_output --partial "@@"
  assert_output --partial "not canonically formatted"
}

@test "docket fmt - reads stdin and writes stdout" {
  cd "$BATS_TEST_TMPDIR"
  cat >input.yml <<'EOF'
---
- tasks:
        - dokku_app:
              app: x
EOF
  run bash -c "\"$(docket_bin)\" fmt - <input.yml"
  assert_success
  assert_output --partial "    - dokku_app:"
  assert [ ! -f tasks.yml ]
}

@test "docket fmt preserves comments" {
  cd "$BATS_TEST_TMPDIR"
  cat >tasks.yml <<'EOF'
---
- tasks:
    # this is a comment
    - dokku_app:
        app: x
EOF
  "$(docket_bin)" fmt
  run grep -c "this is a comment" tasks.yml
  assert_output "1"
}

@test "docket fmt is a no-op on already-canonical input (mtime preserved)" {
  cd "$BATS_TEST_TMPDIR"
  "$(docket_bin)" init
  before_mtime=$(stat -c "%Y" tasks.yml 2>/dev/null || stat -f "%m" tasks.yml)
  sleep 1
  run "$(docket_bin)" fmt
  assert_success
  after_mtime=$(stat -c "%Y" tasks.yml 2>/dev/null || stat -f "%m" tasks.yml)
  assert [ "$before_mtime" = "$after_mtime" ]
}

@test "docket fmt --diff --color never round-trips through patch -p0" {
  cd "$BATS_TEST_TMPDIR"
  cat >tasks.yml <<'EOF'
---
- tasks:
        - dokku_app:
              app: x
EOF
  cp tasks.yml tasks.orig

  "$(docket_bin)" fmt --diff --color never tasks.yml >tasks.diff
  assert [ -s tasks.diff ]
  patch -p0 <tasks.diff

  "$(docket_bin)" fmt - <tasks.orig >tasks.expected
  cmp tasks.yml tasks.expected
}
