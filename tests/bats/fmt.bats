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

@test "docket fmt rejects a multi-document file" {
  cd "$BATS_TEST_TMPDIR"
  cat >tasks.yml <<'EOF'
---
- tasks:
    - dokku_app:
        app: a
---
- tasks:
    - dokku_app:
        app: b
EOF
  run "$(docket_bin)" fmt
  assert_failure
  assert_output --partial "multiple YAML documents"
}

@test "docket fmt on an empty file is a no-op" {
  cd "$BATS_TEST_TMPDIR"
  : >tasks.yml
  run "$(docket_bin)" fmt
  assert_success
  run "$(docket_bin)" fmt --check
  assert_success
  assert [ ! -s tasks.yml ]
}

@test "docket fmt is idempotent with per-task head comments" {
  cd "$BATS_TEST_TMPDIR"
  cat >tasks.yml <<'EOF'
---
- tasks:
    - dokku_app:
        app: a
    # configure the second app
    - dokku_app:
        app: b
EOF
  "$(docket_bin)" fmt
  run "$(docket_bin)" fmt --check
  assert_success
  run grep -c "configure the second app" tasks.yml
  assert_output "1"
}

@test "docket fmt --format json5 - converts a piped YAML recipe" {
  cd "$BATS_TEST_TMPDIR"
  cat >input.yml <<'EOF'
---
- name: web
  tasks:
    # scale it up first
    - name: scale
      dokku_ps_scale:
        app: web
EOF
  run bash -c "\"$(docket_bin)\" fmt --format json5 - <input.yml"
  assert_success
  assert_output --partial 'name: "web",'
  assert_output --partial "// scale it up first"
  refute_output --partial "# scale it up first"
  assert [ ! -f tasks.yml ]
}

@test "docket fmt --format yaml - converts a piped JSON5 recipe" {
  cd "$BATS_TEST_TMPDIR"
  cat >input.json5 <<'EOF'
[
  {
    name: "web",
    // scale it up first
    tasks: [{ name: "scale", dokku_ps_scale: { app: "web" } }],
  },
]
EOF
  run bash -c "\"$(docket_bin)\" fmt --format yaml - <input.json5"
  assert_success
  assert_output --partial "- name: web"
  assert_output --partial "# scale it up first"
  refute_output --partial "// scale it up first"
}

@test "docket fmt --format converts a recipe in place and warns about the extension" {
  cd "$BATS_TEST_TMPDIR"
  "$(docket_bin)" init
  run "$(docket_bin)" fmt --format json5 tasks.yml
  assert_success
  assert_output --partial "does not match"
  assert_output --partial "--tasks-format json5"
  run head -n 1 tasks.yml
  assert_output "["
}

@test "docket fmt --output writes a converted recipe that validate reads" {
  cd "$BATS_TEST_TMPDIR"
  "$(docket_bin)" init
  run "$(docket_bin)" fmt --format json5 --output tasks.json5 tasks.yml
  assert_success
  assert_output --partial "Wrote tasks.json5"
  # The source is left exactly as it was.
  run "$(docket_bin)" fmt --check tasks.yml
  assert_success
  # And the converted file is canonical and loadable on its own.
  run "$(docket_bin)" fmt --check tasks.json5
  assert_success
  run "$(docket_bin)" validate --tasks tasks.json5
  assert_success
}

@test "docket fmt --output refuses to overwrite without --force" {
  cd "$BATS_TEST_TMPDIR"
  "$(docket_bin)" init
  echo "// keep me" >tasks.json5
  run "$(docket_bin)" fmt --format json5 --output tasks.json5 tasks.yml
  assert_failure
  assert_output --partial "pass --force to overwrite"
  run cat tasks.json5
  assert_output "// keep me"

  run "$(docket_bin)" fmt --format json5 --output tasks.json5 --force tasks.yml
  assert_success
  run "$(docket_bin)" validate --tasks tasks.json5
  assert_success
}

@test "docket fmt --check rejects a --format that would convert" {
  cd "$BATS_TEST_TMPDIR"
  "$(docket_bin)" init
  run "$(docket_bin)" fmt --check --format json5 tasks.yml
  assert_failure
  assert_output --partial "a conversion is never a no-op"
  # The recipe is untouched: --check never writes.
  run head -n 1 tasks.yml
  assert_output "---"
}

@test "docket fmt round-trips a recipe through JSON5 and back" {
  cd "$BATS_TEST_TMPDIR"
  "$(docket_bin)" init
  "$(docket_bin)" fmt --format json5 --output tasks.json5 tasks.yml
  run bash -c "\"$(docket_bin)\" fmt --format yaml - <tasks.json5 >back.yml"
  assert_success
  run "$(docket_bin)" validate --tasks back.yml
  assert_success
  run "$(docket_bin)" fmt --check back.yml
  assert_success

  # Equivalence is asserted by converting both to the same format rather
  # than by diffing the YAML. A round trip is deliberately not byte-exact:
  # the --- marker belongs to the bytes that were read, and quoting a value
  # that needs no quotes, or writing a sequence in flow style, are choices
  # the canonical form does not preserve.
  run bash -c "\"$(docket_bin)\" fmt --format json5 - <back.yml >back.json5"
  assert_success
  run diff tasks.json5 back.json5
  assert_success
}

@test "docket fmt keeps a dq interpolation double-quoted across a round trip" {
  cd "$BATS_TEST_TMPDIR"
  "$(docket_bin)" init
  "$(docket_bin)" fmt --format json5 --output tasks.json5 tasks.yml
  run bash -c "\"$(docket_bin)\" fmt --format yaml - <tasks.json5 >back.yml"
  assert_success
  # The quotes around an interpolation are part of what the recipe means:
  # dq escapes for a double-quoted scalar, and a single-quoted one would
  # leave the backslashes in the rendered value.
  run grep -c "\"{{ .app | dq }}\"" back.yml
  refute_output "0"
  refute_output --partial "'{{ .app | dq }}'"
}
