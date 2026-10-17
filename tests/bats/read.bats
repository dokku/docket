#!/usr/bin/env bats

load test_helper

# These tests read over a fake `ssh` first on PATH, so they need neither a
# remote host nor a local dokku. The fake answers `dokku version` with 0.38.31
# so the version gate lets the read run, answers `config:export` for app web
# with one secret, fails `apps:exists gone` the way dokku reports a missing
# app, and answers every other command with exit 0 and no output. It touches
# the control socket on a call and removes it on `-O exit`, so a socket left
# behind is a master docket never closed.

setup() {
  docket_build
  # Under /tmp rather than $BATS_TEST_TMPDIR: a unix socket path is capped at
  # around 104 bytes on macOS, and docket puts its sockets under TMPDIR.
  SOCKET_DIR="$(mktemp -d /tmp/docket-sock.XXXXXX)"
  FAKE_BIN="$BATS_TEST_TMPDIR/fake-bin"
  mkdir -p "$FAKE_BIN"
  cat >"$FAKE_BIN/ssh" <<'EOF'
#!/usr/bin/env bash
socket=""
exit_master=0
while [ $# -gt 0 ]; do
  case "$1" in
    -o)
      case "$2" in ControlPath=*) socket="${2#ControlPath=}" ;; esac
      shift 2
      ;;
    -O)
      [ "$2" = "exit" ] && exit_master=1
      shift 2
      ;;
    -p | -i) shift 2 ;;
    --) shift; break ;;
    *) shift ;;
  esac
done
if [ "$exit_master" -eq 1 ]; then
  rm -f "$socket"
  exit 0
fi
touch "$socket"
case "$*" in
  "dokku version") printf '0.38.31\n' ;;
  "dokku --quiet config:export --format json web") printf '{"API_KEY":"s3cr3t-value"}\n' ;;
  "dokku --quiet apps:exists gone")
    echo "App gone does not exist" >&2
    exit 1
    ;;
esac
exit 0
EOF
  chmod +x "$FAKE_BIN/ssh"
}

teardown() {
  rm -rf "${SOCKET_DIR:-}"
}

# run_read runs `docket read` against the fake remote.
run_read() {
  PATH="$FAKE_BIN:$PATH" TMPDIR="$SOCKET_DIR" run "$(docket_bin)" read --host deploy@dokku.example.com "$@"
}

assert_no_sockets() {
  run find "$SOCKET_DIR" -name 'docket-*.sock'
  assert_success
  assert_output ""
}

@test "docket read prints a one-task recipe and closes its connection" {
  run_read 'dokku_app[app=web]'
  assert_success
  assert_output --partial "dokku_app:"
  assert_output --partial "app: web"
  assert_no_sockets
}

@test "docket read --json carries whether the resource exists" {
  run_read --json 'dokku_app[app=web]'
  assert_success
  echo "$output" | jq -e '.address == "dokku_app[app=web]" and .type == "dokku_app" and .exists == true and .task.app == "web"' >/dev/null ||
    fail "unexpected --json output: $output"
}

@test "docket read exits 2 for a resource that is not there" {
  run_read --json 'dokku_app[app=gone]'
  assert_equal "$status" 2
  echo "$output" | jq -e '.exists == false and .task.state == "absent"' >/dev/null ||
    fail "unexpected --json output: $output"
}

@test "docket read masks sensitive values unless --show-sensitive" {
  run_read 'dokku_config[app=web]'
  assert_success
  assert_output --partial "API_KEY"
  assert_output --partial "***"
  refute_output --partial "s3cr3t-value"

  run_read --show-sensitive 'dokku_config[app=web]'
  assert_success
  assert_output --partial "s3cr3t-value"
}

@test "docket read --format hcl prints the recipe as HCL" {
  run_read --format hcl 'dokku_app[app=web]'
  assert_success
  assert_output --partial 'dokku_app'
  assert_output --partial '"web"'
  refute_output --partial "app: web"
}

@test "docket read refuses an address that names no single resource" {
  run "$(docket_bin)" read dokku_app
  assert_failure 1
  assert_output --partial "names no keys"

  run "$(docket_bin)" read 'dokku_nope[app=a]'
  assert_failure 1
  assert_output --partial 'unknown task type "dokku_nope"'

  run "$(docket_bin)" read 'dokku_app[app=a,global=true]'
  assert_failure 1
  assert_output --partial '"global" is not an identity key of dokku_app'
}

@test "docket read takes exactly one address" {
  run "$(docket_bin)" read
  assert_failure 1
  assert_output --partial "exactly one resource address, got 0"

  run "$(docket_bin)" read 'dokku_app[app=a]' 'dokku_app[app=b]'
  assert_failure 1
  assert_output --partial "exactly one resource address, got 2"
}

@test "docket read refuses --json with --format" {
  run "$(docket_bin)" read --json --format hcl 'dokku_app[app=a]'
  assert_failure 1
  assert_output --partial "--json and --format cannot be combined"
}
