#!/usr/bin/env bats

load test_helper

# These tests put a fake `ssh` first on PATH, so they need neither a remote
# host nor a local dokku, and so the server can lack plugins the CI dokku has
# installed. The fake answers `dokku version`, an empty plugin list and a
# one-app apps:list; every other remote command exits 0 with no output. Every
# remote command is appended to ARGV_LOG, so a test can see which plugin
# commands export ran.

setup() {
  docket_build
  # Under /tmp rather than $BATS_TEST_TMPDIR: a unix socket path is capped at
  # around 104 bytes on macOS, and docket puts its sockets under TMPDIR.
  SOCKET_DIR="$(mktemp -d /tmp/docket-sock.XXXXXX)"
  FAKE_BIN="$BATS_TEST_TMPDIR/fake-bin"
  ARGV_LOG="$BATS_TEST_TMPDIR/argv.log"
  mkdir -p "$FAKE_BIN"
  : >"$ARGV_LOG"
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
echo "$*" >>"$ARGV_LOG"
case "$*" in
  "dokku version") printf '0.38.31\n' ;;
  "dokku --quiet plugin:list --format json") printf '[]\n' ;;
  "dokku --quiet apps:list") printf 'docket-test-export\n' ;;
esac
exit 0
EOF
  chmod +x "$FAKE_BIN/ssh"
  export ARGV_LOG
}

teardown() {
  rm -rf "${SOCKET_DIR:-}"
}

# run_fake_ssh runs docket with the fake ssh first on PATH and its sockets in
# SOCKET_DIR.
run_fake_ssh() {
  PATH="$FAKE_BIN:$PATH" TMPDIR="$SOCKET_DIR" run "$(docket_bin)" "$@"
}

@test "docket export does not read plugins the server does not have installed" {
  run_fake_ssh export --host dokku@fake.invalid --output - --verbose
  assert_success

  # The installed-plugin check ran, and none of the uninstalled plugins'
  # commands did.
  run grep -c 'plugin:list --format json' "$ARGV_LOG"
  assert_success
  run grep -E 'acl:|http-auth:|maintenance:|letsencrypt:|global-cert:' "$ARGV_LOG"
  assert_failure

  # The app is still read through the core plugins.
  run grep -c 'certs:report docket-test-export' "$ARGV_LOG"
  assert_success
}

@test "docket export --verbose reports no progress for an uninstalled plugin's task" {
  run_fake_ssh export --host dokku@fake.invalid --output - --verbose
  assert_success
  refute_output --partial "dokku_acl_app"
  refute_output --partial "dokku_http_auth"
  refute_output --partial "dokku_letsencrypt"
  assert_output --partial "dokku_config"
}
