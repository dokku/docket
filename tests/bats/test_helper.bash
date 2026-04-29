#!/usr/bin/env bash
# Shared helpers for docket bats tests.
#
# Bats and the bats-support / bats-assert helper libraries are expected to be
# installed system-wide via apt; the tests load them from /usr/lib/bats/*.
# See .github/workflows/test.yml for CI install steps and
# tests/bats/README.md for local developer setup.

set -euo pipefail

# Load bats-support / bats-assert from the standard package paths.
# BATS_LIB_PATH is a colon-separated list following the bats-core
# convention (https://bats-core.readthedocs.io/) so developers without
# the apt packages can install the libraries anywhere and point at
# them locally; CI keeps using the apt-installed /usr/lib paths.
load_bats_libraries() {
  local -a search_paths=()
  if [ -n "${BATS_LIB_PATH:-}" ]; then
    local IFS=:
    for entry in $BATS_LIB_PATH; do
      search_paths+=("$entry")
    done
  fi
  search_paths+=(/usr/lib/bats /usr/lib)

  local found_support=0
  local found_assert=0
  for base in "${search_paths[@]}"; do
    if [ "$found_support" -eq 0 ] && [ -f "$base/bats-support/load.bash" ]; then
      # shellcheck disable=SC1090,SC1091
      source "$base/bats-support/load.bash"
      found_support=1
    fi
    if [ "$found_assert" -eq 0 ] && [ -f "$base/bats-assert/load.bash" ]; then
      # shellcheck disable=SC1090,SC1091
      source "$base/bats-assert/load.bash"
      found_assert=1
    fi
    if [ "$found_support" -eq 1 ] && [ "$found_assert" -eq 1 ]; then
      break
    fi
  done
}

load_bats_libraries

# Resolve the docket binary. Prefer the in-tree build at ./docket so a local
# `go build` tests the working tree; fall back to PATH otherwise.
docket_bin() {
  if [ -n "${DOCKET_BIN:-}" ]; then
    echo "$DOCKET_BIN"
    return
  fi
  local repo_root
  repo_root="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  if [ -x "$repo_root/docket" ]; then
    echo "$repo_root/docket"
    return
  fi
  command -v docket
}

# docket_build builds the binary at the repo root. Subsequent calls in the
# same bats run skip the rebuild because Go caches incremental builds.
docket_build() {
  local repo_root
  repo_root="$(cd "$BATS_TEST_DIRNAME/../.." && pwd)"
  (cd "$repo_root" && go build -o docket .)
  export DOCKET_BIN="$repo_root/docket"
}

# write_tasks_file writes its stdin to "$BATS_TEST_TMPDIR/<name>" (default:
# tasks.yml) and exports TASKS_FILE so tests can pass --tasks "$TASKS_FILE".
write_tasks_file() {
  local name="${1:-tasks.yml}"
  TASKS_FILE="$BATS_TEST_TMPDIR/$name"
  cat >"$TASKS_FILE"
  export TASKS_FILE
}

# dokku_clean_app destroys an app if it exists. Used in setup/teardown so
# tests are idempotent on shared CI hosts.
dokku_clean_app() {
  local app="$1"
  if ! command -v dokku >/dev/null 2>&1; then
    return 0
  fi
  if dokku apps:exists "$app" >/dev/null 2>&1; then
    dokku --force apps:destroy "$app" >/dev/null 2>&1 || true
  fi
}

# require_dokku skips the current test when no dokku binary is installed.
require_dokku() {
  if ! command -v dokku >/dev/null 2>&1; then
    skip "dokku not available"
  fi
}

# require_remote_dokku skips the current test when SSH transport tests
# cannot run. The localhost-ssh-to-dokku fixture is only wired up on
# Linux CI; macOS dev boxes typically lack a local dokku and the
# install/plumbing differs.
require_remote_dokku() {
  if [ "$(uname -s)" != "Linux" ]; then
    skip "ssh transport tests run on Linux only"
  fi
  if [ -z "${DOCKET_TEST_REMOTE_HOST:-}" ]; then
    skip "DOCKET_TEST_REMOTE_HOST not set"
  fi
  if ! command -v ssh >/dev/null 2>&1; then
    skip "ssh client not available"
  fi
}
